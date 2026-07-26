package rnlctl

import (
	"context"
	"time"
)

const compensationTimeout = time.Minute

func newCompensationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), compensationTimeout)
}

func runRecoveryPhase(ctx context.Context, recover func(context.Context) error) error {
	completeActiveProgressPhase(ctx, false)
	emitProgressPhase(ctx, phaseRestorePrevious)
	recoveryCtx, cancelRecovery := newCompensationContext(ctx)
	defer cancelRecovery()

	err := recover(withProgressSuppressed(recoveryCtx))
	completeProgressPhase(recoveryCtx, phaseRestorePrevious, err == nil)
	return err
}
