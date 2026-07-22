package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (engine *Engine) Uninstall(ctx context.Context, request UninstallRequest) (Result, error) {
	if request.Purge && !request.Yes {
		return Result{}, fmt.Errorf("uninstall --purge requires --yes")
	}
	state, err := loadState(engine.paths)
	if err != nil {
		return Result{}, err
	}
	retained, err := loadRetained(engine.paths)
	if err != nil {
		return Result{}, err
	}
	journal, err := loadJournal(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if journal != nil {
		return Result{}, fmt.Errorf("an interrupted %s operation requires rnlctl repair", journal.Operation)
	}
	if state == nil {
		if !request.Purge || retained == nil {
			return Result{Operation: "uninstall"}, nil
		}
		if err := engine.host.PreflightRemoveAccount(ctx, retained.Account); err != nil {
			return Result{}, err
		}
	} else {
		if err := engine.host.Preflight(ctx, false, engine.paths); err != nil {
			return Result{}, err
		}
		if request.Purge {
			if err := engine.host.PreflightRemoveAccount(ctx, state.Account); err != nil {
				return Result{}, err
			}
		}
	}
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	state, err = loadState(engine.paths)
	if err != nil {
		return Result{}, err
	}
	retained, err = loadRetained(engine.paths)
	if err != nil {
		return Result{}, err
	}
	journal, err = loadJournal(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if journal != nil {
		return Result{}, fmt.Errorf("an interrupted %s operation requires rnlctl repair", journal.Operation)
	}
	account := ManagedAccount{}
	if state != nil {
		account = state.Account
		current := state.Generations[state.Current]
		journal = &transactionJournal{
			SchemaVersion: journalSchemaVersion, Operation: "uninstall", Phase: "planned",
			From: state.Current, Previous: state.Previous, Target: current,
			Desired: desiredServiceState{}, Prepared: state.Prepared, Purge: request.Purge,
			Account:   account,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := saveJournal(engine.paths, *journal); err != nil {
			return Result{}, err
		}
	} else if retained != nil {
		account = retained.Account
	}
	if state == nil && retained == nil {
		return Result{Operation: "uninstall"}, nil
	}
	if err := engine.finishUninstall(ctx, account, request.Purge, state != nil); err != nil {
		return Result{}, err
	}
	return Result{Operation: "uninstall", Changed: true}, nil
}

// finishUninstall is deliberately idempotent. The uninstall journal remains
// durable until every destructive step succeeds, so repair can resume the same
// intent after a process or machine interruption.
func (engine *Engine) finishUninstall(ctx context.Context, account ManagedAccount, purge, removeService bool) error {
	if removeService {
		if err := engine.applyServiceState(ctx, desiredServiceState{}); err != nil {
			return err
		}
		if err := engine.host.RemoveService(ctx, engine.paths); err != nil {
			return err
		}
	}
	if !purge {
		if err := saveRetained(engine.paths, account); err != nil {
			return err
		}
	}
	if err := engine.removeRuntimeFiles(purge); err != nil {
		return err
	}
	if purge && (account.UserCreated || account.GroupCreated) {
		if err := engine.host.RemoveAccount(ctx, account); err != nil {
			return err
		}
	}
	if err := removeAndSync(engine.paths.StateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !engine.safeRemovalPath(engine.paths.BundleCache) {
		return fmt.Errorf("refusing unsafe bundle cache removal")
	}
	if err := os.RemoveAll(engine.paths.BundleCache); err != nil {
		return err
	}
	if purge {
		if err := removeAndSync(engine.paths.RetainedFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := removeAndSync(engine.paths.ControlBinary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if purge {
		if !engine.safeRemovalPath(engine.paths.InstallerState) {
			return fmt.Errorf("refusing unsafe installer state removal")
		}
		if err := os.RemoveAll(engine.paths.InstallerState); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(engine.paths.InstallerState))
	}
	return clearJournal(engine.paths)
}

func (engine *Engine) removeRuntimeFiles(purge bool) error {
	for _, target := range []string{
		engine.paths.NodeBinaryLink,
		engine.paths.CurrentLink,
		engine.paths.PreviousLink,
	} {
		if err := removeAndSync(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, directory := range []string{
		engine.paths.LibraryRoot,
		engine.paths.ApplicationState,
		engine.paths.LogDirectory,
		engine.paths.RuntimeDirectory,
	} {
		if !engine.safeRemovalPath(directory) {
			return fmt.Errorf("refusing unsafe managed directory removal: %s", directory)
		}
		if err := os.RemoveAll(directory); err != nil {
			return err
		}
		if parent := filepath.Dir(directory); parent != directory {
			if err := syncDirectory(parent); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	if purge {
		if !engine.safeRemovalPath(engine.paths.ConfigDirectory) {
			return fmt.Errorf("refusing unsafe configuration removal")
		}
		if err := os.RemoveAll(engine.paths.ConfigDirectory); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(engine.paths.ConfigDirectory)); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) safeRemovalPath(path string) bool {
	if path == "" || path == "/" || path == "." {
		return false
	}
	for _, allowed := range []string{
		engine.paths.LibraryRoot, engine.paths.BundleCache, engine.paths.ApplicationState,
		engine.paths.LogDirectory, engine.paths.RuntimeDirectory, engine.paths.ConfigDirectory,
		engine.paths.InstallerState,
	} {
		if path == allowed {
			return true
		}
	}
	return false
}
