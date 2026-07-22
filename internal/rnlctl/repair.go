package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (engine *Engine) Repair(ctx context.Context, request RepairRequest) (Result, error) {
	preState, stateErr := loadState(engine.paths)
	preJournal, journalErr := loadJournal(engine.paths)
	if stateErr != nil {
		return Result{}, stateErr
	}
	if journalErr != nil {
		return Result{}, journalErr
	}
	if preState == nil && preJournal == nil {
		return Result{}, ErrNotInstalled
	}
	if preJournal != nil && preJournal.Operation == "uninstall" {
		return engine.repairInterruptedUninstall(ctx, *preJournal)
	}
	desired := desiredServiceState{}
	prepared := false
	if preState != nil {
		desired = preState.Desired
		prepared = preState.Prepared
	} else {
		desired = preJournal.Desired
		prepared = preJournal.Prepared
	}
	if preJournal != nil && preJournal.Operation == "activate" {
		desired = preJournal.Desired
		prepared = false
	}
	if err := engine.host.Preflight(ctx, desired.Active, engine.paths); err != nil {
		return Result{}, err
	}
	var configurationErr error
	if desired.Active {
		configurationErr = validateRuntimeConfiguration(engine.paths)
	} else {
		configurationErr = validatePreparedConfiguration(engine.paths)
	}
	if configurationErr != nil && preState != nil {
		return Result{}, configurationErr
	}
	var supplied *validatedBundle
	if request.Bundle.Root != "" || request.Bundle.Archive != "" || request.Bundle.SHA256 != "" {
		bundle, err := openBundle(request.Bundle, engine.architecture)
		if err != nil {
			return Result{}, err
		}
		defer bundle.Close()
		supplied = bundle
	}
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	state, err := loadState(engine.paths)
	if err != nil {
		return Result{}, err
	}
	interrupted, err := loadJournal(engine.paths)
	if err != nil {
		return Result{}, err
	}
	cleanupUpgrade := false
	if interrupted != nil && interrupted.Operation == "upgrade" {
		if state == nil {
			if err := engine.removeUncommittedUpgrade(*interrupted, nil); err != nil {
				return Result{}, err
			}
			return Result{}, fmt.Errorf("upgrade journal has no lifecycle state; reinstall the Native release")
		}
		if _, committed := state.Generations[interrupted.Target.ID]; !committed {
			cleanupUpgrade = true
		}
	}
	reconstructedInstall := state == nil
	if reconstructedInstall {
		if interrupted == nil || interrupted.Operation != "install" {
			return Result{}, fmt.Errorf("lifecycle state is missing and the journal cannot reconstruct an installation")
		}
		if configurationErr != nil {
			if err := engine.cleanupInterruptedInstall(ctx, *interrupted); err != nil {
				return Result{}, errors.Join(configurationErr, err)
			}
			return Result{Operation: "repair", Changed: true}, nil
		}
		state = &persistentState{
			SchemaVersion: stateSchemaVersion, Current: interrupted.Target.ID,
			CorePolicy: managedCorePolicy, Prepared: interrupted.Prepared,
			Desired: interrupted.Desired, Account: interrupted.Account,
			Generations: map[string]generationRecord{interrupted.Target.ID: interrupted.Target},
		}
	}
	if supplied != nil {
		matched := false
		for _, generation := range state.Generations {
			if generation.Identity == supplied.Identity {
				matched = true
				break
			}
		}
		if !matched {
			return Result{}, fmt.Errorf("repair bundle identity does not match any installed generation; repair never upgrades")
		}
	}
	if interrupted != nil && interrupted.Operation == "activate" {
		state.Desired = interrupted.Desired
		state.Prepared = false
	}
	current := state.Generations[state.Current]
	if interrupted == nil {
		repairJournal := transactionJournal{
			SchemaVersion: journalSchemaVersion, Operation: "repair", Phase: "planned",
			From: state.Current, Previous: state.Previous, Target: current,
			Desired: state.Desired, Prepared: state.Prepared, Account: state.Account,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := saveJournal(engine.paths, repairJournal); err != nil {
			return Result{}, err
		}
	}
	actual, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if actual.Active {
		if err := engine.host.SetActive(ctx, false); err != nil {
			return Result{}, err
		}
	}
	changed := interrupted != nil
	for id, record := range state.Generations {
		repaired, recordChanged, repairErr := engine.repairGeneration(record, supplied)
		if repairErr != nil {
			return Result{}, fmt.Errorf("repair generation %s: %w", id, repairErr)
		}
		state.Generations[id] = repaired
		changed = changed || recordChanged
	}
	current = state.Generations[state.Current]
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return Result{}, err
	}
	if err := engine.selectGeneration(state.Current, state.Previous); err != nil {
		return Result{}, err
	}
	// An interrupted upgrade may have switched the service to its target before
	// state.json was committed. Stop the service and restore the committed links
	// before removing that uncommitted payload.
	if cleanupUpgrade {
		if err := engine.removeUncommittedUpgrade(*interrupted, state); err != nil {
			return Result{}, err
		}
	}
	if err := engine.checkpoint("repair-after-current-link"); err != nil {
		return Result{}, err
	}
	root := filepath.Join(engine.paths.Generations, state.Current)
	account, err := engine.host.Prepare(ctx, root, engine.paths)
	if err != nil {
		return Result{}, err
	}
	state.Account = mergeAccountOwnership(state.Account, account)
	if reconstructedInstall {
		interrupted.Account = state.Account
		interrupted.Phase = "service-prepared"
		if err := saveJournal(engine.paths, *interrupted); err != nil {
			return Result{}, err
		}
	}
	if err := engine.applyServiceState(ctx, state.Desired); err != nil {
		return Result{}, err
	}
	if interrupted != nil && interrupted.RestartRequired {
		if err := engine.host.Restart(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := engine.verifyTransitionOutcome(ctx, current, state.Desired); err != nil {
		return Result{}, err
	}
	if err := saveState(engine.paths, *state); err != nil {
		return Result{}, err
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("repair committed but journal cleanup failed: %w", err)
	}
	if err := engine.garbageCollectCaches(*state); err != nil {
		return Result{}, fmt.Errorf("repair completed but cache cleanup failed: %w", err)
	}
	return Result{Operation: "repair", Changed: changed, Generation: current.ID, Version: current.Version, PreparedOnly: prepared}, nil
}

func (engine *Engine) repairInterruptedUninstall(ctx context.Context, journal transactionJournal) (Result, error) {
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	if journal.Purge {
		if err := engine.host.PreflightRemoveAccount(ctx, journal.Account); err != nil {
			return Result{}, err
		}
	}
	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()

	currentJournal, err := loadJournal(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if currentJournal == nil || currentJournal.Operation != "uninstall" {
		return Result{}, fmt.Errorf("interrupted uninstall journal changed while acquiring the lifecycle lock")
	}
	if currentJournal.Purge != journal.Purge || currentJournal.Account != journal.Account {
		return Result{}, fmt.Errorf("interrupted uninstall intent changed while acquiring the lifecycle lock")
	}
	if err := engine.finishUninstall(ctx, currentJournal.Account, currentJournal.Purge, true); err != nil {
		return Result{}, err
	}
	return Result{Operation: "repair", Changed: true}, nil
}

func (engine *Engine) cleanupInterruptedInstall(ctx context.Context, journal transactionJournal) error {
	var errs []error
	if status, err := engine.host.ServiceStatus(ctx); err == nil {
		if status.Active {
			errs = appendIf(errs, engine.host.SetActive(ctx, false))
		}
		if status.Enabled {
			errs = appendIf(errs, engine.host.SetEnabled(ctx, false))
		}
	}
	errs = appendIf(errs, engine.host.RemoveService(ctx, engine.paths))
	for _, target := range []string{
		engine.paths.CurrentLink, engine.paths.PreviousLink,
		engine.paths.NodeBinaryLink, engine.paths.ControlBinary, engine.paths.StateFile,
	} {
		if err := removeAndSync(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if pathWithin(engine.paths.Generations, filepath.Join(engine.paths.Generations, journal.Target.ID)) {
		errs = appendIf(errs, os.RemoveAll(filepath.Join(engine.paths.Generations, journal.Target.ID)))
	}
	if pathWithin(engine.paths.BundleCache, journal.Target.CacheFile) {
		if err := removeAndSync(journal.Target.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	createdAccount := transactionAccount(journal)
	if createdAccount.UserCreated || createdAccount.GroupCreated {
		errs = appendIf(errs, engine.host.RemoveAccount(ctx, createdAccount))
	}
	if len(errs) == 0 {
		errs = appendIf(errs, clearJournal(engine.paths))
	}
	return errors.Join(errs...)
}

func (engine *Engine) garbageCollectCaches(state persistentState) error {
	retained := make(map[string]struct{}, len(state.Generations))
	for _, generation := range state.Generations {
		retained[generation.CacheFile] = struct{}{}
	}
	entries, err := os.ReadDir(engine.paths.BundleCache)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(engine.paths.BundleCache, entry.Name())
		if _, keep := retained[path]; keep {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !regexpCacheName(entry.Name()) {
			continue
		}
		if err := removeAndSync(path); err != nil {
			return err
		}
	}
	return nil
}

// removeUncommittedUpgrade discards the payload created by an upgrade whose
// state commit was never durable. Repair must do this explicitly: the target
// generation is not present in persistentState, so normal generation and cache
// garbage collection cannot discover it. Keeping the old generation intact
// leaves the installation usable while reclaiming the failed upgrade's disk.
func (engine *Engine) removeUncommittedUpgrade(journal transactionJournal, state *persistentState) error {
	if journal.Target.ID == "" || !pathWithin(engine.paths.Generations, filepath.Join(engine.paths.Generations, journal.Target.ID)) {
		return fmt.Errorf("unsafe interrupted upgrade generation path")
	}
	if state != nil {
		if _, committed := state.Generations[journal.Target.ID]; committed {
			return nil
		}
	}
	generationRoot := filepath.Join(engine.paths.Generations, journal.Target.ID)
	if info, err := os.Lstat(generationRoot); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(generationRoot); err != nil {
				return fmt.Errorf("remove uncommitted upgrade generation: %w", err)
			}
		} else if err := os.RemoveAll(generationRoot); err != nil {
			return fmt.Errorf("remove uncommitted upgrade generation: %w", err)
		}
		if err := syncDirectory(engine.paths.Generations); err != nil {
			return fmt.Errorf("sync generation directory after cleanup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect uncommitted upgrade generation: %w", err)
	}

	if journal.Target.CacheFile == "" ||
		!pathWithin(engine.paths.BundleCache, journal.Target.CacheFile) ||
		filepath.Dir(journal.Target.CacheFile) != engine.paths.BundleCache {
		return fmt.Errorf("unsafe interrupted upgrade cache path")
	}
	keepCache := false
	if state != nil {
		for _, generation := range state.Generations {
			if generation.CacheFile == journal.Target.CacheFile {
				keepCache = true
				break
			}
		}
	}
	if !keepCache {
		if err := removeAndSync(journal.Target.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove uncommitted upgrade cache: %w", err)
		}
	}
	return nil
}

func regexpCacheName(name string) bool {
	return len(name) == 71 && name[64:] == ".tar.gz" && hexDigestRE.MatchString(name[:64])
}

func (engine *Engine) repairGeneration(record generationRecord, supplied *validatedBundle) (generationRecord, bool, error) {
	generationRoot := filepath.Join(engine.paths.Generations, record.ID)
	generation, generationErr := validateBundleRoot(generationRoot, record.Architecture)
	if generationErr == nil && (generation.Identity != record.Identity || generation.GenerationID != record.ID) {
		generationErr = fmt.Errorf("generation identity mismatch")
	}
	var cached *validatedBundle
	if record.CacheFile != "" && record.ArchiveSHA256 != "" {
		cached, _ = openBundle(BundleInput{Archive: record.CacheFile, SHA256: record.ArchiveSHA256}, record.Architecture)
		if cached != nil && cached.Identity != record.Identity {
			cached.Close()
			cached = nil
		}
	}
	if cached != nil {
		defer cached.Close()
	}
	var matchingSupplied *validatedBundle
	if supplied != nil && supplied.Identity == record.Identity {
		matchingSupplied = supplied
	}
	changed := false
	if generationErr != nil {
		source := matchingSupplied
		if source == nil {
			source = cached
		}
		if source == nil {
			return record, false, fmt.Errorf("generation is damaged and no matching verified cache or repair bundle is available: %w", generationErr)
		}
		if !pathWithin(engine.paths.Generations, generationRoot) {
			return record, false, fmt.Errorf("unsafe generation path")
		}
		if err := os.RemoveAll(generationRoot); err != nil {
			return record, false, err
		}
		_, created, err := copyBundleToGeneration(source, engine.paths.Generations)
		if err != nil || !created {
			return record, false, errors.Join(err, fmt.Errorf("generation was not recreated"))
		}
		generation = source
		changed = true
	}
	if cached == nil {
		source := matchingSupplied
		if source == nil {
			source = generation
		}
		if source == nil {
			return record, false, fmt.Errorf("no source is available to rebuild bundle cache")
		}
		if err := removeAndSync(record.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return record, false, err
		}
		cache, _, err := cacheBundle(source, engine.paths.BundleCache)
		if err != nil {
			return record, false, err
		}
		record.CacheFile = cache.Path
		record.ArchiveSHA256 = cache.SHA256
		record.CacheKind = cache.Kind
		changed = true
	}
	return record, changed, nil
}
