package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const upgradePlanSchemaVersion = 1

type upgradeInspection struct {
	plan        UpgradePlan
	state       *persistentState
	current     generationRecord
	superseded  generationRecord
	hasPrevious bool
}

// PreflightUpgrade performs the same static candidate and installed-state
// checks used by Upgrade without creating a generation or changing service
// state. Online preflights still download the exact release into a private,
// short-lived workspace so its complete contents can be verified.
func (engine *Engine) PreflightUpgrade(ctx context.Context, request UpgradeRequest) (UpgradePlan, error) {
	bundle, cleanup, err := engine.prepareUpgradeCandidate(ctx, request)
	if err != nil {
		return UpgradePlan{}, err
	}
	defer cleanup()

	lock, err := acquireOperationLock(engine.paths)
	if err != nil {
		return UpgradePlan{}, err
	}
	defer lock.Close()

	inspection, err := engine.inspectUpgrade(ctx, bundle)
	if err != nil {
		return UpgradePlan{}, err
	}
	return inspection.plan, nil
}

// prepareUpgradeCandidate contains all work which may be done outside the
// lifecycle lock. The clean-state check is repeated authoritatively under the
// lock after a potentially slow online download.
func (engine *Engine) prepareUpgradeCandidate(ctx context.Context, request UpgradeRequest) (*validatedBundle, func(), error) {
	if err := validateUpgradeRequest(request); err != nil {
		return nil, func() {}, err
	}
	if err := engine.requirePrivileges(); err != nil {
		return nil, func() {}, err
	}
	if _, err := engine.requireCleanState(); err != nil {
		return nil, func() {}, err
	}

	input, resolveCleanup, err := engine.resolveBundleInput(ctx, request.Bundle, request.To)
	if err != nil {
		return nil, func() {}, err
	}
	bundle, err := openBundle(input, engine.architecture)
	if err != nil {
		resolveCleanup()
		return nil, func() {}, err
	}
	cleanup := func() {
		bundle.Close()
		resolveCleanup()
	}
	return bundle, cleanup, nil
}

func validateUpgradeRequest(request UpgradeRequest) error {
	input := request.Bundle
	hasRoot := input.Root != ""
	hasArchive := input.Archive != ""
	hasBundleOption := hasRoot || hasArchive || input.SHA256 != "" || input.ExpectedVersion != ""

	if request.To != "" {
		if hasBundleOption {
			return fmt.Errorf("--to cannot be combined with local bundle options")
		}
		if !projectVersionRE.MatchString(request.To) {
			return fmt.Errorf("--to requires an exact version such as 2.8.0 or 2.8.0-rnl.1")
		}
		return nil
	}
	if hasRoot == hasArchive {
		return fmt.Errorf("upgrade requires one of --bundle-root, --bundle, or --to")
	}
	if input.ExpectedVersion != "" && !projectVersionRE.MatchString(input.ExpectedVersion) {
		return fmt.Errorf("invalid expected version %q", input.ExpectedVersion)
	}
	if hasRoot {
		if input.SHA256 != "" {
			return fmt.Errorf("--sha256 is valid only with --bundle")
		}
		return nil
	}
	if !hexDigestRE.MatchString(input.SHA256) {
		return fmt.Errorf("--bundle requires a lowercase 64-character --sha256")
	}
	return nil
}

func (engine *Engine) inspectUpgrade(ctx context.Context, bundle *validatedBundle) (upgradeInspection, error) {
	state, err := engine.requireCleanState()
	if err != nil {
		return upgradeInspection{}, err
	}
	current := state.Generations[state.Current]
	if err := engine.verifyGeneration(current); err != nil {
		return upgradeInspection{}, fmt.Errorf("verify current generation: %w; run rnlctl repair", err)
	}
	if err := engine.checkSelectedLinks(*state); err != nil {
		return upgradeInspection{}, fmt.Errorf("verify generation selection: %w; run rnlctl repair", err)
	}
	if state.Prepared {
		err = validatePreparedConfiguration(engine.paths)
	} else {
		err = validateRuntimeConfiguration(engine.paths)
	}
	if err != nil {
		return upgradeInspection{}, fmt.Errorf("validate current configuration: %w", err)
	}
	if err := engine.checkManagedPermissions(*state); err != nil {
		return upgradeInspection{}, fmt.Errorf("validate current managed permissions: %w", err)
	}

	service, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return upgradeInspection{}, err
	}
	if state.Prepared && (service.Enabled || service.Active) {
		return upgradeInspection{}, fmt.Errorf("prepared installation is unexpectedly enabled or active; run rnlctl repair before upgrading")
	}
	changeRequired := current.Identity != bundle.Identity
	if changeRequired {
		if err := engine.host.Preflight(ctx, service.Active, engine.paths); err != nil {
			return upgradeInspection{}, err
		}
	}

	inspection := upgradeInspection{
		plan: UpgradePlan{
			SchemaVersion:     upgradePlanSchemaVersion,
			ChangeRequired:    changeRequired,
			CurrentVersion:    current.Version,
			CurrentGeneration: current.ID,
			TargetVersion:     bundle.Manifest.Version,
			TargetGeneration:  bundle.GenerationID,
			Prepared:          state.Prepared,
			Service:           service,
		},
		state:   state,
		current: current,
	}
	inspection.superseded, inspection.hasPrevious = state.Generations[state.Previous]
	return inspection, nil
}

// discardStagedUpgrade removes only payloads created before an upgrade begins
// changing service state or selected generation links.
func (engine *Engine) discardStagedUpgrade(record generationRecord, cacheCreated, generationCreated bool) error {
	var cleanupErrors []error
	if generationCreated {
		if err := os.RemoveAll(filepath.Join(engine.paths.Generations, record.ID)); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		} else if err := syncDirectory(engine.paths.Generations); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if cacheCreated {
		if err := removeAndSync(record.CacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}
