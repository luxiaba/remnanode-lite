package rnlctl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const processRunnerHelperEnv = "GO_WANT_RNLCTL_PROCESS_HELPER"

func TestProcessRunnerHelper(t *testing.T) {
	mode := os.Getenv(processRunnerHelperEnv)
	if mode == "" {
		return
	}

	switch mode {
	case "exit-seven":
		os.Exit(7)
	case "stdio":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		_, _ = fmt.Fprint(os.Stderr, "helper stderr")
	case "arguments":
		separator := -1
		for index, argument := range os.Args {
			if argument == "--" {
				separator = index
				break
			}
		}
		if separator < 0 {
			os.Exit(8)
		}
		_, _ = fmt.Fprint(os.Stdout, strings.Join(os.Args[separator+1:], "\n"))
	case "wait-for-signal":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(30 * time.Second)
	case "ignore-termination":
		signal.Ignore(syscall.SIGTERM)
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(30 * time.Second)
	default:
		os.Exit(9)
	}
	os.Exit(0)
}

func TestProcessRunnerPreservesChildExitCode(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "exit-seven")
	runner := &ProcessRunner{signals: make(chan os.Signal)}
	var stderr bytes.Buffer

	code := runner.Run(context.Background(), Command{
		Name:   os.Args[0],
		Args:   []string{"-test.run=^TestProcessRunnerHelper$"},
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if code != 7 {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestProcessRunnerStreamsStandardIO(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "stdio")
	runner := &ProcessRunner{signals: make(chan os.Signal)}
	var stdout, stderr bytes.Buffer

	code := runner.Run(context.Background(), Command{
		Name:   os.Args[0],
		Args:   []string{"-test.run=^TestProcessRunnerHelper$"},
		Stdin:  strings.NewReader("helper stdin"),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != 0 {
		t.Fatalf("Run() = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if stdout.String() != "helper stdin" || stderr.String() != "helper stderr" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestProcessRunnerPassesLiteralArgumentArray(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "arguments")
	runner := &ProcessRunner{signals: make(chan os.Signal)}
	var stdout, stderr bytes.Buffer
	literalArgs := []string{"literal; echo unsafe", "$(touch /tmp/never-run)", "space separated"}

	code := runner.Run(context.Background(), Command{
		Name: os.Args[0],
		Args: append([]string{
			"-test.run=^TestProcessRunnerHelper$",
			"--",
		}, literalArgs...),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != 0 {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.Split(stdout.String(), "\n"); !equalStrings(got, literalArgs) {
		t.Fatalf("arguments = %q, want %q", got, literalArgs)
	}
}

func TestProcessRunnerForwardsSignalAndReturnsShellExitCode(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "wait-for-signal")
	signalChannel := make(chan os.Signal, 1)
	runner := &ProcessRunner{signals: signalChannel}
	ready := newReadyWriter()
	var stderr bytes.Buffer
	result := make(chan int, 1)

	go func() {
		result <- runner.Run(context.Background(), Command{
			Name:   os.Args[0],
			Args:   []string{"-test.run=^TestProcessRunnerHelper$"},
			Stdout: ready,
			Stderr: &stderr,
		})
	}()

	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not become ready")
	}
	signalChannel <- syscall.SIGTERM

	select {
	case code := <-result:
		if code != 128+int(syscall.SIGTERM) {
			t.Fatalf("Run() = %d, want %d; stderr = %q", code, 128+int(syscall.SIGTERM), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("signal was not forwarded to the child")
	}
}

func TestProcessRunnerContextCancellationTerminatesChild(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "wait-for-signal")
	runner := &ProcessRunner{signals: make(chan os.Signal)}
	ready := newReadyWriter()
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)

	go func() {
		result <- runner.Run(ctx, Command{
			Name:   os.Args[0],
			Args:   []string{"-test.run=^TestProcessRunnerHelper$"},
			Stdout: ready,
			Stderr: &stderr,
		})
	}()

	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("child did not become ready")
	}
	cancel()

	select {
	case code := <-result:
		if code != 128+int(syscall.SIGTERM) {
			t.Fatalf("Run() = %d, want %d; stderr = %q", code, 128+int(syscall.SIGTERM), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("context cancellation did not terminate the child")
	}
}

func TestProcessRunnerContextCancellationKillsUnresponsiveChild(t *testing.T) {
	t.Setenv(processRunnerHelperEnv, "ignore-termination")
	runner := &ProcessRunner{
		signals:           make(chan os.Signal),
		cancelGracePeriod: 100 * time.Millisecond,
	}
	ready := newReadyWriter()
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)

	go func() {
		result <- runner.Run(ctx, Command{
			Name:   os.Args[0],
			Args:   []string{"-test.run=^TestProcessRunnerHelper$"},
			Stdout: ready,
			Stderr: &stderr,
		})
	}()

	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("child did not become ready")
	}
	cancel()

	select {
	case code := <-result:
		if code != 128+int(syscall.SIGKILL) {
			t.Fatalf("Run() = %d, want %d; stderr = %q", code, 128+int(syscall.SIGKILL), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unresponsive child was not killed after the cancellation grace period")
	}
}

func TestProcessRunnerReportsStartFailure(t *testing.T) {
	runner := &ProcessRunner{signals: make(chan os.Signal)}
	var stderr bytes.Buffer

	code := runner.Run(context.Background(), Command{
		Name:   "/path/that/does/not/exist/rnlctl-test",
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if code != 1 || !strings.Contains(stderr.String(), "start") {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
}

func TestProcessRunnerRejectsEmptyCommand(t *testing.T) {
	var stderr bytes.Buffer
	code := NewProcessRunner().Run(context.Background(), Command{Stderr: &stderr})
	if code != 1 || !strings.Contains(stderr.String(), "empty command") {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
}

type readyWriter struct {
	mu    sync.Mutex
	once  sync.Once
	ready chan struct{}
	data  bytes.Buffer
}

func newReadyWriter() *readyWriter {
	return &readyWriter{ready: make(chan struct{})}
}

func (w *readyWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written, err := w.data.Write(payload)
	if strings.Contains(w.data.String(), "ready") {
		w.once.Do(func() { close(w.ready) })
	}
	return written, err
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
