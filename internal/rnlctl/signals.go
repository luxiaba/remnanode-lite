package rnlctl

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type processSignalCause struct {
	signal os.Signal
}

func (cause *processSignalCause) Error() string {
	return "received " + cause.signal.String()
}

// NotifySignals returns a context canceled by the first process signal. Signal
// handling is restored immediately afterward so a repeated signal can force
// termination if graceful cancellation becomes stuck.
func NotifySignals(parent context.Context, watched ...os.Signal) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	signals := make(chan os.Signal, 1)
	stopRequested := make(chan struct{})
	signal.Notify(signals, watched...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case received := <-signals:
			signal.Stop(signals)
			if received != nil {
				cancel(&processSignalCause{signal: received})
			}
		case <-stopRequested:
			signal.Stop(signals)
			// Stop guarantees no more deliveries after it returns. Preserve a
			// signal already queued while the command was still running.
			select {
			case received := <-signals:
				if received != nil {
					cancel(&processSignalCause{signal: received})
				}
			default:
				cancel(context.Canceled)
			}
		case <-ctx.Done():
			signal.Stop(signals)
		}
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(stopRequested)
			<-done
		})
	}
	return ctx, stop
}

// SignalExitCode converts a signal-canceled context to the conventional shell
// exit status while preserving fallback for ordinary command completion.
func SignalExitCode(ctx context.Context, fallback int) int {
	received := signalFromContext(ctx)
	unixSignal, ok := received.(syscall.Signal)
	if !ok {
		return fallback
	}
	return 128 + int(unixSignal)
}

func signalFromContext(ctx context.Context) os.Signal {
	if ctx == nil {
		return nil
	}
	cause, ok := context.Cause(ctx).(*processSignalCause)
	if !ok {
		return nil
	}
	return cause.signal
}
