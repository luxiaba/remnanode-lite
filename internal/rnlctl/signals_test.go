package rnlctl

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNotifySignalsRecordsAProcessSignal(t *testing.T) {
	ctx, stop := NotifySignals(context.Background(), syscall.SIGUSR1)
	defer stop()

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("signal context was not canceled")
	}
	if got := SignalExitCode(ctx, 1); got != 128+int(syscall.SIGUSR1) {
		t.Fatalf("SignalExitCode() = %d, want %d", got, 128+int(syscall.SIGUSR1))
	}
}

func TestNotifySignalsStopPreservesOrdinaryResult(t *testing.T) {
	ctx, stop := NotifySignals(context.Background(), syscall.SIGUSR1)
	stop()

	if got := SignalExitCode(ctx, 7); got != 7 {
		t.Fatalf("SignalExitCode() after Stop = %d, want 7", got)
	}
}

func TestSignalExitCodeUsesCancellationSignal(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&processSignalCause{signal: syscall.SIGINT})

	if got := SignalExitCode(ctx, 1); got != 130 {
		t.Fatalf("SignalExitCode() = %d, want 130", got)
	}
}

func TestSignalExitCodePreservesOrdinaryResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := SignalExitCode(ctx, 7); got != 7 {
		t.Fatalf("SignalExitCode() = %d, want 7", got)
	}
}
