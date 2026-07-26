package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (engine *Engine) Install(ctx context.Context, request InstallRequest) (Result, error) {
	emitProgressPhase(ctx, phaseVerifyBundle)
	bundle, err := openBundle(request.Bundle, engine.architecture)
	if err != nil {
		return Result{}, err
	}
	defer bundle.Close()
	completeProgressPhase(ctx, phaseVerifyBundle, true)

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
	pendingJournal, err := loadJournal(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if pendingJournal != nil {
		return Result{}, fmt.Errorf("an interrupted %s operation requires rnlctl repair", pendingJournal.Operation)
	}
	if state != nil {
		current := state.Generations[state.Current]
		if current.Identity == bundle.Identity {
			return Result{Operation: "install", Generation: current.ID, Version: current.Version}, nil
		}
		return Result{}, ErrAlreadyInstalled
	}
	secretData, err := effectiveInstallSecret(request, engine.paths)
	if err != nil {
		return Result{}, err
	}
	if !request.PrepareOnly && len(secretData) == 0 {
		return Result{}, fmt.Errorf("a valid Secret Key is required")
	}
	emitProgressPhase(ctx, phaseValidateHost)
	if err := engine.host.Preflight(ctx, !request.PrepareOnly, engine.paths); err != nil {
		return Result{}, err
	}
	completeProgressPhase(ctx, phaseValidateHost, true)
	retained, err := loadRetained(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if err := engine.requireFreshInstallLayout(); err != nil {
		return Result{}, err
	}

	environmentSnapshot, err := snapshotFile(engine.paths.EnvironmentFile, maxEnvironmentBytes)
	if err != nil {
		return Result{}, err
	}
	secretSnapshot, err := snapshotFile(engine.paths.SecretFile, maxEnvironmentBytes)
	if err != nil {
		return Result{}, err
	}
	environment, _, err := prepareEnvironment(bundle.Root, engine.paths, request.Port)
	if err != nil {
		return Result{}, err
	}
	emitProgressPhase(ctx, phasePrepareGeneration)
	cache, cacheCreated, err := cacheBundle(bundle, engine.paths.BundleCache)
	if err != nil {
		return Result{}, err
	}
	record := generationFromBundle(bundle, cache)
	desired := desiredServiceState{Enabled: !request.PrepareOnly, Active: !request.PrepareOnly}
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "install", Phase: "planned",
		Target: record, Desired: desired, StartedAt: time.Now().UTC().Format(time.RFC3339),
		Prepared: request.PrepareOnly,
	}
	if err := saveJournal(engine.paths, journal); err != nil {
		if cacheCreated {
			_ = os.Remove(cache.Path)
		}
		return Result{}, err
	}
	if err := engine.checkpoint("install-after-journal"); err != nil {
		_ = clearJournal(engine.paths)
		return Result{}, err
	}

	generationCreated := false
	account := ManagedAccount{}
	transactionAccount := ManagedAccount{}
	servicePrepared := false
	rollback := func(cause error) (Result, error) {
		rollbackErr := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
			if err := engine.rollbackFailedInstall(recoveryCtx, record, cacheCreated, generationCreated, transactionAccount, servicePrepared, environmentSnapshot, secretSnapshot); err != nil {
				return err
			}
			return clearJournal(engine.paths)
		})
		return Result{}, errors.Join(cause, rollbackErr)
	}

	generationRoot, created, err := copyBundleToGeneration(bundle, engine.paths.Generations)
	generationCreated = created
	if err != nil {
		return rollback(err)
	}
	journal.Phase = "payload-ready"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-generation"); err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phasePrepareGeneration, true)
	emitProgressPhase(ctx, phaseWriteConfiguration)
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollback(err)
	}
	if err := atomicWriteFile(engine.paths.EnvironmentFile, environment, 0o640); err != nil {
		return rollback(fmt.Errorf("write node.env: %w", err))
	}
	if len(secretData) > 0 {
		if err := atomicWriteFile(engine.paths.SecretFile, secretData, 0o640); err != nil {
			return rollback(fmt.Errorf("write Secret Key: %w", err))
		}
	}
	completeProgressPhase(ctx, phaseWriteConfiguration, true)
	emitProgressPhase(ctx, phaseSwitchGeneration)
	if err := engine.selectGeneration(record.ID, ""); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-current-link"); err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phaseSwitchGeneration, true)
	emitProgressPhase(ctx, phasePrepareService)
	account, err = engine.host.Prepare(ctx, generationRoot, engine.paths)
	// Prepare may create the managed account before a later host step fails.
	// Preserve the returned ownership metadata even when Prepare reports that
	// failure so install rollback can remove only resources created by this
	// transaction.
	transactionAccount = account
	if err != nil {
		return rollback(err)
	}
	servicePrepared = true
	if retained != nil {
		account = mergeAccountOwnership(retained.Account, account)
	}
	journal.Account = account
	journal.TransactionAccount = &transactionAccount
	journal.Phase = "service-prepared"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phasePrepareService, true)
	if err := engine.applyServiceState(ctx, desired); err != nil {
		return rollback(err)
	}
	if err := engine.verifyTransitionOutcome(ctx, record, desired); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-service"); err != nil {
		return rollback(err)
	}
	emitProgressPhase(ctx, phaseCommitState)
	state = &persistentState{
		SchemaVersion: stateSchemaVersion, Current: record.ID,
		CorePolicy: managedCorePolicy, Prepared: request.PrepareOnly, Desired: desired, Account: account,
		Generations: map[string]generationRecord{record.ID: record},
	}
	if err := saveState(engine.paths, *state); err != nil {
		return rollback(err)
	}
	journal.Phase = "state-committed"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-state"); err != nil {
		return rollback(err)
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("installation committed but journal cleanup failed: %w; run rnlctl repair", err)
	}
	completeProgressPhase(ctx, phaseCommitState, true)
	emitProgressPhase(ctx, phaseCleanUp)
	if err := removeAndSync(engine.paths.RetainedFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("installation committed but retained metadata cleanup failed: %w", err)
	}
	completeProgressPhase(ctx, phaseCleanUp, true)
	return Result{
		Operation: "install", Changed: true, Generation: record.ID,
		Version: record.Version, PreparedOnly: request.PrepareOnly,
	}, nil
}

func (engine *Engine) Activate(ctx context.Context, request ActivateRequest) (Result, error) {
	secretData, err := effectiveActivationSecret(request, engine.paths)
	if err != nil {
		return Result{}, err
	}
	emitProgressPhase(ctx, phaseValidateHost)
	if err := engine.host.Preflight(ctx, true, engine.paths); err != nil {
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
	current := state.Generations[state.Current]
	emitProgressPhase(ctx, phaseVerifyBundle)
	if err := engine.verifyGeneration(current); err != nil {
		return Result{}, err
	}
	completeProgressPhase(ctx, phaseVerifyBundle, true)
	secretSnapshot, err := snapshotFile(engine.paths.SecretFile, maxEnvironmentBytes)
	if err != nil {
		return Result{}, err
	}
	secretChanged := request.SecretFile != "" && (!secretSnapshot.exists || !bytes.Equal(secretSnapshot.data, secretData))
	serviceBefore, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if state.Desired.Enabled && state.Desired.Active && serviceBefore.Enabled && serviceBefore.Active && request.SecretFile == "" {
		return Result{Operation: "activate", Generation: current.ID, Version: current.Version}, nil
	}
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "activate", Phase: "planned",
		From: current.ID, Target: current, Desired: desiredServiceState{Enabled: true, Active: true},
		RestartRequired: secretChanged && serviceBefore.Active,
		Account:         state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(engine.paths, journal); err != nil {
		return Result{}, err
	}
	rollback := func(cause error) (Result, error) {
		rollbackErr := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
			restoreErr := secretSnapshot.restore()
			if journal.RestartRequired {
				restoreErr = errors.Join(restoreErr, engine.reassertServiceState(recoveryCtx, desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}))
				if restoreErr == nil {
					restoreErr = engine.host.Restart(recoveryCtx)
				}
				if restoreErr == nil {
					restoreErr = engine.host.WaitHealthy(recoveryCtx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
				}
			} else {
				restoreErr = errors.Join(restoreErr, engine.reassertServiceState(recoveryCtx, desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}))
			}
			if restoreErr != nil {
				return restoreErr
			}
			return clearJournal(engine.paths)
		})
		return Result{}, errors.Join(cause, rollbackErr)
	}
	if request.SecretFile != "" {
		emitProgressPhase(ctx, phaseWriteConfiguration)
		if err := atomicWriteFile(engine.paths.SecretFile, secretData, 0o640); err != nil {
			return rollback(err)
		}
		completeProgressPhase(ctx, phaseWriteConfiguration, true)
	}
	if err := validateRuntimeConfiguration(engine.paths); err != nil {
		return rollback(err)
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollback(err)
	}
	generationRoot := filepath.Join(engine.paths.Generations, current.ID)
	emitProgressPhase(ctx, phasePrepareService)
	account, err := engine.host.Prepare(ctx, generationRoot, engine.paths)
	if err == nil {
		completeProgressPhase(ctx, phasePrepareService, true)
	}
	if err == nil {
		err = engine.applyServiceState(ctx, journal.Desired)
	}
	if err == nil && journal.RestartRequired {
		emitProgressPhase(ctx, phaseRestartService)
		err = engine.host.Restart(ctx)
		if err == nil {
			completeProgressPhase(ctx, phaseRestartService, true)
		}
	}
	if err == nil {
		err = engine.verifyTransitionOutcome(ctx, current, journal.Desired)
	}
	if err != nil {
		return rollback(err)
	}
	state.Desired = journal.Desired
	state.Prepared = false
	state.Account = mergeAccountOwnership(state.Account, account)
	emitProgressPhase(ctx, phaseCommitState)
	if err := saveState(engine.paths, *state); err != nil {
		return rollback(err)
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("activation committed but journal cleanup failed: %w", err)
	}
	completeProgressPhase(ctx, phaseCommitState, true)
	return Result{Operation: "activate", Changed: true, Generation: current.ID, Version: current.Version}, nil
}

func (engine *Engine) Upgrade(ctx context.Context, request UpgradeRequest) (Result, error) {
	bundle, cleanup, err := engine.prepareUpgradeCandidate(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	emitProgressPhase(ctx, phaseValidateHost)
	inspection, err := engine.inspectUpgrade(ctx, bundle)
	if err != nil {
		return Result{}, err
	}
	completeProgressPhase(ctx, phaseValidateHost, true)
	state := inspection.state
	oldRecord := inspection.current
	superseded, hasSuperseded := inspection.superseded, inspection.hasPrevious
	if !inspection.plan.ChangeRequired {
		return Result{Operation: "upgrade", Generation: oldRecord.ID, Version: oldRecord.Version}, nil
	}
	serviceBefore := inspection.plan.Service
	desired := desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}
	emitProgressPhase(ctx, phasePrepareGeneration)
	cache, cacheCreated, err := cacheBundle(bundle, engine.paths.BundleCache)
	if err != nil {
		return Result{}, err
	}
	record := generationFromBundle(bundle, cache)
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "upgrade", Phase: "planned",
		From: oldRecord.ID, Previous: state.Previous, Target: record, Desired: desired,
		Account: state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(engine.paths, journal); err != nil {
		cleanupErr := engine.discardStagedUpgrade(record, cacheCreated, false)
		return Result{}, errors.Join(err, cleanupErr)
	}
	generationCreated := false
	transitionStarted := false
	rollback := func(cause error) (Result, error) {
		rollbackErr := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
			var err error
			if transitionStarted {
				err = engine.rollbackTransition(recoveryCtx, *state, desired, record, cacheCreated, generationCreated)
			} else {
				err = engine.discardStagedUpgrade(record, cacheCreated, generationCreated)
			}
			if err != nil {
				return err
			}
			return clearJournal(engine.paths)
		})
		return Result{}, errors.Join(cause, rollbackErr)
	}
	targetRoot, created, err := copyBundleToGeneration(bundle, engine.paths.Generations)
	generationCreated = created
	if err != nil {
		return rollback(err)
	}
	journal.Phase = "payload-ready"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	if err := engine.host.ValidateBinary(
		ctx,
		filepath.Join(targetRoot, "bin", "remnanode-lite"),
		record.Version,
		record.ContractVersion,
	); err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phasePrepareGeneration, true)
	if serviceBefore.Active {
		transitionStarted = true
		emitProgressPhase(ctx, phaseStopService)
		if err := engine.host.SetActive(ctx, false); err != nil {
			return rollback(err)
		}
		completeProgressPhase(ctx, phaseStopService, true)
	}
	if err := engine.checkpoint("upgrade-after-stop"); err != nil {
		return rollback(err)
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollback(err)
	}
	transitionStarted = true
	emitProgressPhase(ctx, phaseSwitchGeneration)
	if err := engine.selectGeneration(record.ID, oldRecord.ID); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("upgrade-after-current-link"); err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phaseSwitchGeneration, true)
	emitProgressPhase(ctx, phasePrepareService)
	account, err := engine.host.Prepare(ctx, targetRoot, engine.paths)
	if err != nil {
		return rollback(err)
	}
	completeProgressPhase(ctx, phasePrepareService, true)
	if err := engine.applyServiceState(ctx, desired); err != nil {
		return rollback(err)
	}
	if err := engine.verifyTransitionOutcome(ctx, record, desired); err != nil {
		return rollback(err)
	}
	journal.Phase = "service-restored"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("upgrade-after-service"); err != nil {
		return rollback(err)
	}
	emitProgressPhase(ctx, phaseCommitState)
	newState := persistentState{
		SchemaVersion: stateSchemaVersion, Current: record.ID, Previous: oldRecord.ID,
		CorePolicy: managedCorePolicy, Prepared: state.Prepared, Desired: desired,
		Account:     mergeAccountOwnership(state.Account, account),
		Generations: map[string]generationRecord{record.ID: record, oldRecord.ID: oldRecord},
	}
	if err := saveState(engine.paths, newState); err != nil {
		return rollback(err)
	}
	journal.Phase = "state-committed"
	if err := saveJournal(engine.paths, journal); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("upgrade-after-state"); err != nil {
		return rollback(err)
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("upgrade committed but journal cleanup failed: %w; run rnlctl repair", err)
	}
	completeProgressPhase(ctx, phaseCommitState, true)
	if hasSuperseded {
		emitProgressPhase(ctx, phaseCleanUp)
		if err := engine.removeSuperseded(superseded, newState); err != nil {
			return Result{}, fmt.Errorf("upgrade committed but superseded payload cleanup failed: %w", err)
		}
		completeProgressPhase(ctx, phaseCleanUp, true)
	}
	return Result{Operation: "upgrade", Changed: true, Generation: record.ID, Version: record.Version}, nil
}

func (engine *Engine) Rollback(ctx context.Context, request RollbackRequest) (Result, error) {
	preState, err := loadState(engine.paths)
	if err != nil {
		return Result{}, err
	}
	if preState == nil {
		return Result{}, ErrNotInstalled
	}
	targetID := request.GenerationID
	if targetID == "" {
		targetID = preState.Previous
	}
	if targetID == "" {
		return Result{}, fmt.Errorf("no previous generation is available")
	}
	if targetID != preState.Previous && targetID != preState.Current {
		return Result{}, fmt.Errorf("generation %q is not the retained previous generation", targetID)
	}
	emitProgressPhase(ctx, phaseValidateHost)
	serviceBefore, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := engine.host.Preflight(ctx, serviceBefore.Active, engine.paths); err != nil {
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
	targetID = request.GenerationID
	if targetID == "" {
		targetID = state.Previous
	}
	if targetID == state.Current {
		current := state.Generations[state.Current]
		return Result{Operation: "rollback", Generation: current.ID, Version: current.Version}, nil
	}
	if targetID == "" || targetID != state.Previous {
		return Result{}, fmt.Errorf("generation %q is not the retained previous generation", targetID)
	}
	target := state.Generations[targetID]
	emitProgressPhase(ctx, phaseVerifyBundle)
	if err := engine.verifyGeneration(target); err != nil {
		return Result{}, fmt.Errorf("target generation is invalid; run rnlctl repair: %w", err)
	}
	completeProgressPhase(ctx, phaseVerifyBundle, true)
	serviceBefore, err = engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	desired := desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}
	oldState := *state
	current := state.Generations[state.Current]
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "rollback", Phase: "planned",
		From: current.ID, Previous: state.Previous, Target: target, Desired: desired,
		Account: state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(engine.paths, journal); err != nil {
		return Result{}, err
	}
	rollbackFailure := func(cause error) (Result, error) {
		restoreErr := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
			if err := engine.rollbackTransition(recoveryCtx, oldState, desired, target, false, false); err != nil {
				return err
			}
			return clearJournal(engine.paths)
		})
		return Result{}, errors.Join(cause, restoreErr)
	}
	if serviceBefore.Active {
		emitProgressPhase(ctx, phaseStopService)
		if err := engine.host.SetActive(ctx, false); err != nil {
			return rollbackFailure(err)
		}
		completeProgressPhase(ctx, phaseStopService, true)
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollbackFailure(err)
	}
	emitProgressPhase(ctx, phaseSwitchGeneration)
	if err := engine.selectGeneration(target.ID, current.ID); err != nil {
		return rollbackFailure(err)
	}
	completeProgressPhase(ctx, phaseSwitchGeneration, true)
	targetRoot := filepath.Join(engine.paths.Generations, target.ID)
	emitProgressPhase(ctx, phasePrepareService)
	account, err := engine.host.Prepare(ctx, targetRoot, engine.paths)
	if err != nil {
		return rollbackFailure(err)
	}
	completeProgressPhase(ctx, phasePrepareService, true)
	if err := engine.applyServiceState(ctx, desired); err != nil {
		return rollbackFailure(err)
	}
	if err := engine.verifyTransitionOutcome(ctx, target, desired); err != nil {
		return rollbackFailure(err)
	}
	emitProgressPhase(ctx, phaseCommitState)
	newState := persistentState{
		SchemaVersion: stateSchemaVersion, Current: target.ID, Previous: current.ID,
		CorePolicy: managedCorePolicy, Prepared: state.Prepared, Desired: desired,
		Account:     mergeAccountOwnership(state.Account, account),
		Generations: map[string]generationRecord{target.ID: target, current.ID: current},
	}
	if err := saveState(engine.paths, newState); err != nil {
		return rollbackFailure(err)
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("rollback committed but journal cleanup failed: %w", err)
	}
	completeProgressPhase(ctx, phaseCommitState, true)
	return Result{Operation: "rollback", Changed: true, Generation: target.ID, Version: target.Version}, nil
}

func (engine *Engine) resolveBundleInput(ctx context.Context, input BundleInput, version string) (BundleInput, func(), error) {
	if version == "" {
		if (input.Root == "") == (input.Archive == "") {
			return BundleInput{}, func() {}, fmt.Errorf("upgrade requires one of --bundle-root, --bundle, or --to")
		}
		return input, func() {}, nil
	}
	if input.Root != "" || input.Archive != "" || input.SHA256 != "" || input.ExpectedVersion != "" {
		return BundleInput{}, func() {}, fmt.Errorf("--to cannot be combined with local bundle options")
	}
	if !projectVersionRE.MatchString(version) {
		return BundleInput{}, func() {}, fmt.Errorf("--to requires an exact version such as 2.8.0 or 2.8.0-rnl.1")
	}
	temporary, err := createNativeTemporaryDirectory("rnlctl-release-*")
	if err != nil {
		return BundleInput{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	archive, err := engine.resolver.Resolve(ctx, version, engine.architecture, temporary)
	if err != nil {
		cleanup()
		return BundleInput{}, func() {}, err
	}
	digest, _, err := digestFile(archive, maxBundleArchive)
	if err != nil {
		cleanup()
		return BundleInput{}, func() {}, err
	}
	return BundleInput{Archive: archive, SHA256: digest, ExpectedVersion: version}, cleanup, nil
}

func (engine *Engine) requireCleanState() (*persistentState, error) {
	state, err := loadState(engine.paths)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, ErrNotInstalled
	}
	journal, err := loadJournal(engine.paths)
	if err != nil {
		return nil, err
	}
	if journal != nil {
		return nil, fmt.Errorf("an interrupted %s operation requires rnlctl repair", journal.Operation)
	}
	return state, nil
}

func (engine *Engine) requireFreshInstallLayout() error {
	for _, target := range []string{engine.paths.CurrentLink, engine.paths.PreviousLink, engine.paths.NodeBinaryLink, engine.paths.ControlBinary} {
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("managed path %s exists without lifecycle state; remove it or recover the prior installation", target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (engine *Engine) ensureRuntimeDirectories() error {
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{
		{engine.paths.LibraryRoot, 0o755}, {engine.paths.Generations, 0o755},
		{engine.paths.ConfigDirectory, 0o750}, {engine.paths.ApplicationState, 0o750},
		{engine.paths.LogDirectory, 0o750}, {engine.paths.RuntimeDirectory, 0o750},
		{engine.paths.BundleCache, 0o700},
	} {
		if err := ensureDirectory(entry.path, entry.mode); err != nil {
			return err
		}
	}
	return nil
}

func (engine *Engine) internalSocketPath() string {
	return filepath.Join(engine.paths.RuntimeDirectory, "internal.sock")
}

func (engine *Engine) selectGeneration(current, previous string) error {
	currentRoot := filepath.Join(engine.paths.Generations, current)
	if err := atomicSymlink(currentRoot, engine.paths.CurrentLink); err != nil {
		return fmt.Errorf("select current generation: %w", err)
	}
	if previous == "" {
		if err := removeAndSync(engine.paths.PreviousLink); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := atomicSymlink(filepath.Join(engine.paths.Generations, previous), engine.paths.PreviousLink); err != nil {
		return fmt.Errorf("select previous generation: %w", err)
	}
	if err := atomicSymlink(filepath.Join(engine.paths.CurrentLink, "bin", "remnanode-lite"), engine.paths.NodeBinaryLink); err != nil {
		return fmt.Errorf("install node binary link: %w", err)
	}
	if err := atomicCopyFile(filepath.Join(currentRoot, "bin", "rnlctl"), engine.paths.ControlBinary, 0o755); err != nil {
		return fmt.Errorf("install independent rnlctl binary: %w", err)
	}
	return nil
}

func (engine *Engine) applyServiceState(ctx context.Context, desired desiredServiceState) error {
	actual, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return err
	}
	if actual.Active && !desired.Active {
		emitProgressPhase(ctx, phaseStopService)
		if err := engine.host.SetActive(ctx, false); err != nil {
			return err
		}
		completeProgressPhase(ctx, phaseStopService, true)
		actual.Active = false
	}
	if actual.Enabled != desired.Enabled {
		if err := engine.host.SetEnabled(ctx, desired.Enabled); err != nil {
			return err
		}
	}
	if !actual.Active && desired.Active {
		emitProgressPhase(ctx, phaseStartService)
		if err := engine.host.SetActive(ctx, true); err != nil {
			return err
		}
		completeProgressPhase(ctx, phaseStartService, true)
	}
	return nil
}

// reassertServiceState is used only during recovery. A canceled service-manager
// client does not prove that the manager canceled the job it already accepted,
// so recovery must issue the previous intent again instead of trusting a
// transient status snapshot.
func (engine *Engine) reassertServiceState(ctx context.Context, desired desiredServiceState) error {
	var errs []error
	if desired.Enabled {
		errs = appendIf(errs, engine.host.SetEnabled(ctx, true))
	}
	errs = appendIf(errs, engine.host.SetActive(ctx, desired.Active))
	if !desired.Enabled {
		errs = appendIf(errs, engine.host.SetEnabled(ctx, false))
	}
	actual, err := engine.host.ServiceStatus(ctx)
	errs = appendIf(errs, err)
	if err == nil && (actual.Enabled != desired.Enabled || actual.Active != desired.Active) {
		errs = append(errs, fmt.Errorf(
			"service recovery state is enabled=%t active=%t; want enabled=%t active=%t",
			actual.Enabled, actual.Active, desired.Enabled, desired.Active,
		))
	}
	return errors.Join(errs...)
}

func (engine *Engine) verifyGeneration(record generationRecord) error {
	root := filepath.Join(engine.paths.Generations, record.ID)
	bundle, err := validateBundleRoot(root, record.Architecture)
	if err != nil {
		return err
	}
	if bundle.Identity != record.Identity || bundle.GenerationID != record.ID {
		return fmt.Errorf("generation identity does not match lifecycle state")
	}
	return nil
}

func (engine *Engine) verifyTransitionOutcome(ctx context.Context, record generationRecord, desired desiredServiceState) error {
	binary := filepath.Join(engine.paths.Generations, record.ID, "bin", "remnanode-lite")
	if err := engine.host.ValidateBinary(ctx, binary, record.Version, record.ContractVersion); err != nil {
		return err
	}
	if desired.Active {
		emitProgressPhase(ctx, phaseWaitHealthy)
		if err := engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second); err != nil {
			return err
		}
		completeProgressPhase(ctx, phaseWaitHealthy, true)
	}
	return nil
}

func (engine *Engine) rollbackFailedInstall(ctx context.Context, record generationRecord, cacheCreated, generationCreated bool, account ManagedAccount, servicePrepared bool, environment, secret fileSnapshot) error {
	var errs []error
	if servicePrepared {
		if err := engine.host.SetActive(ctx, false); err != nil {
			errs = append(errs, err)
		}
		if err := engine.host.SetEnabled(ctx, false); err != nil {
			errs = append(errs, err)
		}
	}
	if err := engine.host.RemoveService(ctx, engine.paths); err != nil {
		errs = append(errs, err)
	}
	for _, target := range []string{engine.paths.CurrentLink, engine.paths.PreviousLink, engine.paths.NodeBinaryLink, engine.paths.ControlBinary, engine.paths.StateFile} {
		if err := removeAndSync(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if generationCreated {
		errs = appendIf(errs, os.RemoveAll(filepath.Join(engine.paths.Generations, record.ID)))
	}
	if cacheCreated {
		if err := removeAndSync(record.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	errs = appendIf(errs, environment.restore())
	errs = appendIf(errs, secret.restore())
	if account.UserCreated || account.GroupCreated {
		errs = appendIf(errs, engine.host.RemoveAccount(ctx, account))
	}
	return errors.Join(errs...)
}

func (engine *Engine) rollbackTransition(ctx context.Context, old persistentState, desired desiredServiceState, target generationRecord, cacheCreated, generationCreated bool) error {
	var errs []error
	if err := engine.host.SetActive(ctx, false); err != nil {
		errs = append(errs, err)
	}
	if err := engine.selectGeneration(old.Current, old.Previous); err != nil {
		errs = append(errs, err)
	} else {
		if err := engine.ensureRuntimeDirectories(); err != nil {
			errs = append(errs, err)
		} else {
			root := filepath.Join(engine.paths.Generations, old.Current)
			if _, err := engine.host.Prepare(ctx, root, engine.paths); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := engine.applyServiceState(ctx, desired); err != nil {
		errs = append(errs, err)
	} else if record, exists := old.Generations[old.Current]; exists {
		if err := engine.verifyTransitionOutcome(ctx, record, desired); err != nil {
			errs = append(errs, err)
		}
	}
	if err := saveState(engine.paths, old); err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	if generationCreated {
		errs = appendIf(errs, os.RemoveAll(filepath.Join(engine.paths.Generations, target.ID)))
	}
	if cacheCreated {
		if err := removeAndSync(target.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (engine *Engine) removeSuperseded(record generationRecord, keep persistentState) error {
	if record.ID == "" {
		return nil
	}
	// Re-selecting a retained generation keeps its payload but may replace its
	// repair cache with a separately staged verified archive.
	if _, retained := keep.Generations[record.ID]; !retained {
		if err := os.RemoveAll(filepath.Join(engine.paths.Generations, record.ID)); err != nil {
			return err
		}
	}
	for _, retained := range keep.Generations {
		if retained.CacheFile == record.CacheFile {
			return nil
		}
	}
	if err := removeAndSync(record.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func mergeAccountOwnership(old, current ManagedAccount) ManagedAccount {
	if old.UID == current.UID && old.GID == current.GID && old.Home == current.Home && old.Shell == current.Shell {
		current.UserCreated = current.UserCreated || old.UserCreated
		current.GroupCreated = current.GroupCreated || old.GroupCreated
	}
	return current
}

func appendIf(values []error, err error) []error {
	if err != nil {
		return append(values, err)
	}
	return values
}
