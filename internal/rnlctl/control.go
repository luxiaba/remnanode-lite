package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (engine *Engine) Start(ctx context.Context) (Result, error) {
	return engine.controlService(ctx, "start")
}

func (engine *Engine) Stop(ctx context.Context) (Result, error) {
	return engine.controlService(ctx, "stop")
}

func (engine *Engine) Restart(ctx context.Context) (Result, error) {
	return engine.controlService(ctx, "restart")
}

func (engine *Engine) controlService(ctx context.Context, operation string) (Result, error) {
	preState, err := loadState(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if preState == nil {
		return Result{}, ErrNotInstalled
	}
	if (operation == "start" || operation == "restart") && preState.Prepared {
		return Result{}, fmt.Errorf("prepared installations must be enabled with rnlctl activate")
	}
	emitProgressPhase(ctx, phaseValidateHost)
	activating := operation == "start" || operation == "restart"
	if activating {
		if err := validateRuntimeConfiguration(engine.paths); err != nil {
			return Result{}, err
		}
	}
	if err := engine.host.Preflight(ctx, activating, engine.paths); err != nil {
		return Result{}, err
	}
	completeProgressPhase(ctx, phaseValidateHost, true)
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	state, err := engine.requireCleanState()
	if err != nil {
		return Result{}, err
	}
	if (operation == "start" || operation == "restart") && state.Prepared {
		return Result{}, fmt.Errorf("prepared installations must be enabled with rnlctl activate")
	}
	current := state.Generations[state.Current]
	actual, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if operation == "start" && actual.Active && state.Desired.Active {
		return Result{Operation: operation, Generation: current.ID, Version: current.Version}, nil
	}
	if operation == "stop" && !actual.Active && !state.Desired.Active {
		return Result{Operation: operation, Generation: current.ID, Version: current.Version}, nil
	}
	if operation == "restart" && !actual.Active {
		return Result{}, fmt.Errorf("service is stopped; use rnlctl start")
	}
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: operation, Phase: "planned",
		From: state.Current, Previous: state.Previous, Target: current,
		Desired: state.Desired, Prepared: state.Prepared, Account: state.Account,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(engine.paths, journal); err != nil {
		return Result{}, err
	}
	restoreAfterCancellation := func(cause error) (Result, error) {
		restoreErr := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
			previous := desiredServiceState{Enabled: actual.Enabled, Active: actual.Active}
			if err := engine.reassertServiceState(recoveryCtx, previous); err != nil {
				return err
			}
			if previous.Active {
				if err := engine.verifyTransitionOutcome(recoveryCtx, current, previous); err != nil {
					return err
				}
			}
			return clearJournal(engine.paths)
		})
		return Result{}, errors.Join(cause, restoreErr)
	}
	switch operation {
	case "start":
		emitProgressPhase(ctx, phaseStartService)
		err = engine.host.SetActive(ctx, true)
		if err == nil {
			completeProgressPhase(ctx, phaseStartService, true)
			emitProgressPhase(ctx, phaseWaitHealthy)
			err = engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
			if err == nil {
				completeProgressPhase(ctx, phaseWaitHealthy, true)
			}
		}
		if err == nil {
			state.Desired.Active = true
		}
	case "stop":
		emitProgressPhase(ctx, phaseStopService)
		err = engine.host.SetActive(ctx, false)
		if err == nil {
			completeProgressPhase(ctx, phaseStopService, true)
			state.Desired.Active = false
		}
	case "restart":
		emitProgressPhase(ctx, phaseRestartService)
		err = engine.host.Restart(ctx)
		if err == nil {
			completeProgressPhase(ctx, phaseRestartService, true)
			emitProgressPhase(ctx, phaseWaitHealthy)
			err = engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
			if err == nil {
				completeProgressPhase(ctx, phaseWaitHealthy, true)
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return restoreAfterCancellation(ctx.Err())
		}
		return Result{}, err
	}
	if ctx.Err() != nil {
		return restoreAfterCancellation(ctx.Err())
	}
	emitProgressPhase(ctx, phaseCommitState)
	if err := saveState(engine.paths, *state); err != nil {
		return Result{}, err
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("%s committed but journal cleanup failed: %w", operation, err)
	}
	completeProgressPhase(ctx, phaseCommitState, true)
	return Result{Operation: operation, Changed: true, Generation: current.ID, Version: current.Version}, nil
}
