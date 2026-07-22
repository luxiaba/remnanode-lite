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
	bundle, err := openBundle(request.Bundle, engine.architecture)
	if err != nil {
		return Result{}, err
	}
	defer bundle.Close()

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
	if err := engine.host.Preflight(ctx, !request.PrepareOnly, engine.paths); err != nil {
		return Result{}, err
	}
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
		rollbackErr := engine.rollbackFailedInstall(ctx, record, cacheCreated, generationCreated, transactionAccount, servicePrepared, environmentSnapshot, secretSnapshot)
		if rollbackErr == nil {
			_ = clearJournal(engine.paths)
		}
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
	if err := engine.selectGeneration(record.ID, ""); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-current-link"); err != nil {
		return rollback(err)
	}
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
	if err := engine.applyServiceState(ctx, desired); err != nil {
		return rollback(err)
	}
	if err := engine.verifyTransitionOutcome(ctx, record, desired); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("install-after-service"); err != nil {
		return rollback(err)
	}
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
	if err := removeAndSync(engine.paths.RetainedFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("installation committed but retained metadata cleanup failed: %w", err)
	}
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
	if err := engine.host.Preflight(ctx, true, engine.paths); err != nil {
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
	current := state.Generations[state.Current]
	if err := engine.verifyGeneration(current); err != nil {
		return Result{}, err
	}
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
		rollbackErr := secretSnapshot.restore()
		if journal.RestartRequired {
			rollbackErr = errors.Join(rollbackErr, engine.applyServiceState(ctx, desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}))
			if rollbackErr == nil {
				rollbackErr = engine.host.Restart(ctx)
			}
			if rollbackErr == nil {
				rollbackErr = engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
			}
		} else {
			rollbackErr = errors.Join(rollbackErr, engine.applyServiceState(ctx, desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}))
		}
		if rollbackErr == nil {
			rollbackErr = clearJournal(engine.paths)
		}
		return Result{}, errors.Join(cause, rollbackErr)
	}
	if request.SecretFile != "" {
		if err := atomicWriteFile(engine.paths.SecretFile, secretData, 0o640); err != nil {
			return rollback(err)
		}
	}
	if err := validateRuntimeConfiguration(engine.paths); err != nil {
		return rollback(err)
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollback(err)
	}
	generationRoot := filepath.Join(engine.paths.Generations, current.ID)
	account, err := engine.host.Prepare(ctx, generationRoot, engine.paths)
	if err == nil {
		err = engine.applyServiceState(ctx, journal.Desired)
	}
	if err == nil && journal.RestartRequired {
		err = engine.host.Restart(ctx)
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
	if err := saveState(engine.paths, *state); err != nil {
		return rollback(err)
	}
	if err := clearJournal(engine.paths); err != nil {
		return Result{}, fmt.Errorf("activation committed but journal cleanup failed: %w", err)
	}
	return Result{Operation: "activate", Changed: true, Generation: current.ID, Version: current.Version}, nil
}

func (engine *Engine) Upgrade(ctx context.Context, request UpgradeRequest) (Result, error) {
	input, cleanup, err := engine.resolveBundleInput(ctx, request.Bundle, request.To)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	bundle, err := openBundle(input, engine.architecture)
	if err != nil {
		return Result{}, err
	}
	defer bundle.Close()
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
	oldRecord := state.Generations[state.Current]
	superseded, hasSuperseded := state.Generations[state.Previous]
	if oldRecord.Identity == bundle.Identity {
		return Result{Operation: "upgrade", Generation: oldRecord.ID, Version: oldRecord.Version}, nil
	}
	serviceBefore, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := engine.host.Preflight(ctx, serviceBefore.Active, engine.paths); err != nil {
		return Result{}, err
	}
	desired := desiredServiceState{Enabled: serviceBefore.Enabled, Active: serviceBefore.Active}
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
		if cacheCreated {
			_ = os.Remove(cache.Path)
		}
		return Result{}, err
	}
	generationCreated := false
	rollback := func(cause error) (Result, error) {
		rollbackErr := engine.rollbackTransition(ctx, *state, desired, record, cacheCreated, generationCreated)
		if rollbackErr == nil {
			_ = clearJournal(engine.paths)
		}
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
	if serviceBefore.Active {
		if err := engine.host.SetActive(ctx, false); err != nil {
			return rollback(err)
		}
	}
	if err := engine.checkpoint("upgrade-after-stop"); err != nil {
		return rollback(err)
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollback(err)
	}
	if err := engine.selectGeneration(record.ID, oldRecord.ID); err != nil {
		return rollback(err)
	}
	if err := engine.checkpoint("upgrade-after-current-link"); err != nil {
		return rollback(err)
	}
	account, err := engine.host.Prepare(ctx, targetRoot, engine.paths)
	if err != nil {
		return rollback(err)
	}
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
	if hasSuperseded {
		if err := engine.removeSuperseded(superseded, newState); err != nil {
			return Result{}, fmt.Errorf("upgrade committed but superseded payload cleanup failed: %w", err)
		}
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
	serviceBefore, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := engine.host.Preflight(ctx, serviceBefore.Active, engine.paths); err != nil {
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
	if err := engine.verifyGeneration(target); err != nil {
		return Result{}, fmt.Errorf("target generation is invalid; run rnlctl repair: %w", err)
	}
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
		restoreErr := engine.rollbackTransition(ctx, oldState, desired, target, false, false)
		if restoreErr == nil {
			_ = clearJournal(engine.paths)
		}
		return Result{}, errors.Join(cause, restoreErr)
	}
	if serviceBefore.Active {
		if err := engine.host.SetActive(ctx, false); err != nil {
			return rollbackFailure(err)
		}
	}
	if err := engine.ensureRuntimeDirectories(); err != nil {
		return rollbackFailure(err)
	}
	if err := engine.selectGeneration(target.ID, current.ID); err != nil {
		return rollbackFailure(err)
	}
	targetRoot := filepath.Join(engine.paths.Generations, target.ID)
	account, err := engine.host.Prepare(ctx, targetRoot, engine.paths)
	if err != nil {
		return rollbackFailure(err)
	}
	if err := engine.applyServiceState(ctx, desired); err != nil {
		return rollbackFailure(err)
	}
	if err := engine.verifyTransitionOutcome(ctx, target, desired); err != nil {
		return rollbackFailure(err)
	}
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
	return Result{Operation: "rollback", Changed: true, Generation: target.ID, Version: target.Version}, nil
}

func (engine *Engine) resolveBundleInput(ctx context.Context, input BundleInput, version string) (BundleInput, func(), error) {
	if version == "" {
		if (input.Root == "") == (input.Archive == "") {
			return BundleInput{}, func() {}, fmt.Errorf("upgrade requires one of --bundle-root, --bundle, or --to")
		}
		return input, func() {}, nil
	}
	if input.Root != "" || input.Archive != "" {
		return BundleInput{}, func() {}, fmt.Errorf("--to cannot be combined with --bundle-root or --bundle")
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
		if err := engine.host.SetActive(ctx, false); err != nil {
			return err
		}
		actual.Active = false
	}
	if actual.Enabled != desired.Enabled {
		if err := engine.host.SetEnabled(ctx, desired.Enabled); err != nil {
			return err
		}
	}
	if !actual.Active && desired.Active {
		if err := engine.host.SetActive(ctx, true); err != nil {
			return err
		}
	}
	return nil
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
		return engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second)
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
	if _, retained := keep.Generations[record.ID]; retained {
		return nil
	}
	if err := os.RemoveAll(filepath.Join(engine.paths.Generations, record.ID)); err != nil {
		return err
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
