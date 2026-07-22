package rnlctl

import (
	"context"
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
	activating := operation == "start" || operation == "restart"
	if activating {
		if err := validateRuntimeConfiguration(engine.paths); err != nil {
			return Result{}, err
		}
	}
	if err := engine.host.Preflight(ctx, activating, engine.paths); err != nil {
		return Result{}, err
	}
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
	switch operation {
	case "start":
		err = engine.host.SetActive(ctx, true)
		if err == nil {
			err = engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
		}
		if err == nil {
			state.Desired.Active = true
		}
	case "stop":
		err = engine.host.SetActive(ctx, false)
		if err == nil {
			state.Desired.Active = false
		}
	case "restart":
		err = engine.host.Restart(ctx)
		if err == nil {
			err = engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
		}
	}
	if err != nil {
		return Result{}, err
	}
	if err := saveState(engine.paths, *state); err != nil {
		return Result{}, err
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("%s committed but journal cleanup failed: %w", operation, err)
	}
	return Result{Operation: operation, Changed: true, Generation: current.ID, Version: current.Version}, nil
}
