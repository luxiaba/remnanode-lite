package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInternalHealthcheckRequiresReadyUnixHTTPServer(t *testing.T) {
	path, stop := startHealthcheckServer(t, http.StatusOK)
	defer stop()
	t.Setenv("INTERNAL_SOCKET_PATH", filepath.Join(t.TempDir(), "wrong.sock"))

	var stderr bytes.Buffer
	if code := internalHealthcheck(path, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("healthcheck = %d, stderr = %q", code, stderr.String())
	}
}

func TestInternalHealthcheckFailsForUnavailableSocket(t *testing.T) {
	var stderr bytes.Buffer
	if code := internalHealthcheck(filepath.Join(t.TempDir(), "missing.sock"), &stderr); code != 1 || !strings.Contains(stderr.String(), "internal healthcheck failed") {
		t.Fatalf("healthcheck = %d, stderr = %q", code, stderr.String())
	}
}

func TestInternalHealthcheckRejectsNotReadyResponse(t *testing.T) {
	path, stop := startHealthcheckServer(t, http.StatusServiceUnavailable)
	defer stop()
	var stderr bytes.Buffer
	if code := internalHealthcheck(path, &stderr); code != 1 || !strings.Contains(stderr.String(), "HTTP 503") {
		t.Fatalf("healthcheck = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunCLIHealthcheckSocketOverridesInheritedEnvironment(t *testing.T) {
	path, stop := startHealthcheckServer(t, http.StatusOK)
	defer stop()
	t.Setenv("INTERNAL_SOCKET_PATH", filepath.Join(t.TempDir(), "wrong.sock"))

	for _, args := range [][]string{
		{"healthcheck", "--socket", path},
		{"healthcheck", "--socket=" + path},
	} {
		var stderr bytes.Buffer
		code := runCLI(args, strings.NewReader(""), &bytes.Buffer{}, &stderr,
			func() error { t.Fatal("healthcheck must not start the daemon"); return nil },
			func([]string) int { t.Fatal("healthcheck must not invoke doctor"); return 1 },
			socketKillerNotCalled(t))
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("runCLI(%q) = %d, stderr = %q", args, code, stderr.String())
		}
	}
}

func startHealthcheckServer(t *testing.T, status int) (string, func()) {
	t.Helper()
	// macOS and Linux both impose a short limit on Unix socket paths. Keep the
	// test path compact even when the repository's temporary directory is deep.
	path := filepath.Join("/tmp", fmt.Sprintf("rnl-health-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/health" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
	})}
	done := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	return path, func() {
		_ = server.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("healthcheck server did not stop")
		}
		_ = os.Remove(path)
	}
}
