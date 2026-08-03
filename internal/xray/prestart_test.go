package xray

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

type staticPreStartConfig struct {
	enabled  bool
	patterns []string
}

func (c staticPreStartConfig) PreStartCleanupSockets() (bool, []string) {
	return c.enabled, append([]string(nil), c.patterns...)
}

func createStaleUnixSocket(t *testing.T, path string) {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("close Unix socket: %v", err)
	}
}

func shortPreStartTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "rnl-prestart-")
	if err != nil {
		t.Fatalf("create short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func TestCleanupPreStartSocketsRemovesOnlyUnixSockets(t *testing.T) {
	t.Parallel()

	directory := shortPreStartTempDir(t)
	socketPath := filepath.Join(directory, "stale.sock")
	regularPath := filepath.Join(directory, "regular.sock")
	subdirectoryPath := filepath.Join(directory, "directory.sock")
	symlinkPath := filepath.Join(directory, "symlink.sock")
	createStaleUnixSocket(t, socketPath)
	if err := os.WriteFile(regularPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subdirectoryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(socketPath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	if removed := cleanupPreStartSockets(context.Background(), []string{filepath.Join(directory, "*.sock")}); removed != 1 {
		t.Fatalf("removed %d sockets, want 1", removed)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket still exists: %v", err)
	}
	for _, path := range []string{regularPath, subdirectoryPath, symlinkPath} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("non-socket %q was removed: %v", path, err)
		}
	}
}

func TestResolvePreStartPatternCapsMatches(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	for index := 0; index <= maxPreStartMatchesPerPattern; index++ {
		path := filepath.Join(directory, "match-"+strconv.Itoa(index))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if matches := resolvePreStartPattern(filepath.Join(directory, "match-*")); len(matches) != maxPreStartMatchesPerPattern {
		t.Fatalf("resolved %d matches, want %d", len(matches), maxPreStartMatchesPerPattern)
	}
}

func TestCleanupPreStartSocketsHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(shortPreStartTempDir(t), "stale.sock")
	createStaleUnixSocket(t, socketPath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if removed := cleanupPreStartSockets(ctx, []string{socketPath}); removed != 0 {
		t.Fatalf("removed %d sockets after cancellation", removed)
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("socket was removed after cancellation: %v", err)
	}
}

func TestPreStartRunsAfterStopAndBeforeReplacementSpawn(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	request := lifecycleStartRequest("client-a")
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("initial start failed: %#v", response)
	}

	socketPath := filepath.Join(shortPreStartTempDir(t), "stale.sock")
	createStaleUnixSocket(t, socketPath)
	manager.preStart = staticPreStartConfig{enabled: true, patterns: []string{socketPath}}
	originalCommand := manager.processCommand
	var socketAbsentAtSpawn atomic.Bool
	manager.processCommand = func() *exec.Cmd {
		if _, err := os.Lstat(socketPath); errors.Is(err, os.ErrNotExist) {
			socketAbsentAtSpawn.Store(true)
		}
		return originalCommand()
	}

	request.Internals.ForceRestart = true
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("replacement start failed: %#v", response)
	}
	if !socketAbsentAtSpawn.Load() {
		t.Fatal("replacement process spawned before stale socket cleanup")
	}
}

func TestUnchangedStartDoesNotRunPreStart(t *testing.T) {
	manager, process := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	request := lifecycleStartRequest("client-a")
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("initial start failed: %#v", response)
	}

	socketPath := filepath.Join(shortPreStartTempDir(t), "stale.sock")
	createStaleUnixSocket(t, socketPath)
	manager.preStart = staticPreStartConfig{enabled: true, patterns: []string{socketPath}}
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("unchanged start failed: %#v", response)
	}
	if process.starts.Load() != 1 {
		t.Fatalf("unchanged start spawned %d processes, want 1", process.starts.Load())
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("unchanged start removed socket: %v", err)
	}
}

func TestPreStartExpansionFailureDoesNotBlockSpawn(t *testing.T) {
	manager, process := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	manager.preStart = staticPreStartConfig{enabled: true, patterns: []string{"["}}
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("start failed after pre-start error: %#v", response)
	}
	if process.starts.Load() != 1 {
		t.Fatalf("pre-start error spawned %d processes, want 1", process.starts.Load())
	}
}
