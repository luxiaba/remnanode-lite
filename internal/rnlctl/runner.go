package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const defaultCancelGracePeriod = 5 * time.Second

// Command describes one direct process invocation. Args are passed to exec
// without shell interpolation.
type Command struct {
	Name   string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Runner executes one external command and returns its process exit code.
type Runner interface {
	Run(context.Context, Command) int
}

// ProcessRunner streams a child process and forwards interactive termination
// signals to it.
type ProcessRunner struct {
	signals           <-chan os.Signal
	cancelGracePeriod time.Duration
}

// NewProcessRunner creates the production external-command runner.
func NewProcessRunner() *ProcessRunner {
	return &ProcessRunner{}
}

// Run invokes one command directly and preserves child exit-code semantics.
func (r *ProcessRunner) Run(ctx context.Context, command Command) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if command.Stderr == nil {
		command.Stderr = io.Discard
	}
	if command.Stdout == nil {
		command.Stdout = io.Discard
	}
	if command.Name == "" {
		fmt.Fprintln(command.Stderr, "rnlctl: refusing to run an empty command")
		return 1
	}

	child := exec.CommandContext(ctx, command.Name, command.Args...)
	child.Cancel = func() error {
		err := child.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return err
	}
	child.WaitDelay = r.gracePeriod()
	child.Stdin = command.Stdin
	child.Stdout = command.Stdout
	child.Stderr = command.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintf(command.Stderr, "rnlctl: start %s: %v\n", command.Name, err)
		return 1
	}

	signals, stopSignals := r.signalSource()
	done := make(chan struct{})
	forwarderDone := make(chan struct{})
	go func() {
		defer close(forwarderDone)
		forwardSignals(child.Process, signals, done)
	}()

	err := child.Wait()
	close(done)
	stopSignals()
	<-forwarderDone
	return processExitCode(err, command.Stderr)
}

func (r *ProcessRunner) gracePeriod() time.Duration {
	if r.cancelGracePeriod > 0 {
		return r.cancelGracePeriod
	}
	return defaultCancelGracePeriod
}

func (r *ProcessRunner) signalSource() (<-chan os.Signal, func()) {
	if r.signals != nil {
		return r.signals, func() {}
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(
		signals,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	return signals, func() { signal.Stop(signals) }
}

func forwardSignals(
	process *os.Process,
	signals <-chan os.Signal,
	done <-chan struct{},
) {
	for {
		select {
		case <-done:
			return
		case processSignal, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if processSignal != nil {
				_ = process.Signal(processSignal)
			}
		}
	}
}

func processExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		if code := exitError.ExitCode(); code >= 0 {
			return code
		}
	}
	fmt.Fprintf(stderr, "rnlctl: wait for command: %v\n", err)
	return 1
}
