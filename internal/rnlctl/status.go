package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func (engine *Engine) Status(ctx context.Context) (Status, error) {
	status := Status{SchemaVersion: stateSchemaVersion, Deployment: "absent", Healthy: true}
	state, err := loadState(engine.paths)
	if err != nil {
		status.Deployment = "recovery-required"
		status.Healthy = false
		status.Problems = append(status.Problems, err.Error())
		return status, nil
	}
	journal, journalErr := loadJournal(engine.paths)
	if journalErr != nil {
		status.Deployment = "recovery-required"
		status.Healthy = false
		status.Problems = append(status.Problems, journalErr.Error())
		return status, nil
	}
	status.Pending = pendingFromJournal(journal)
	if state == nil {
		if journal != nil {
			status.Deployment = "recovery-required"
			status.Healthy = false
		}
		return status, nil
	}
	current := state.Generations[state.Current]
	status.Installed = true
	status.Prepared = state.Prepared
	status.Version = current.Version
	status.Generation = current.ID
	status.Previous = state.Previous
	status.CorePolicy = state.CorePolicy
	status.Deployment = "installed"
	if state.Prepared {
		status.Deployment = "prepared"
	}
	if journal != nil {
		status.Deployment = "recovery-required"
		status.Healthy = false
	}

	if err := engine.checkSelectedLinks(*state); err != nil {
		status.Problems = append(status.Problems, err.Error())
	}
	if err := engine.checkGenerationSurface(current); err != nil {
		status.Problems = append(status.Problems, err.Error())
	}
	if state.Prepared {
		if err := validatePreparedConfiguration(engine.paths); err != nil {
			status.Problems = append(status.Problems, err.Error())
		}
	} else if err := validateRuntimeConfiguration(engine.paths); err != nil {
		status.Problems = append(status.Problems, err.Error())
	}
	service, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		status.Problems = append(status.Problems, fmt.Sprintf("query service state: %v", err))
	} else {
		status.Service = service
		if service.Enabled != state.Desired.Enabled || service.Active != state.Desired.Active {
			status.Problems = append(status.Problems, fmt.Sprintf(
				"service state enabled=%t active=%t, expected enabled=%t active=%t",
				service.Enabled, service.Active, state.Desired.Enabled, state.Desired.Active,
			))
		}
		if state.Desired.Active && service.Active {
			if err := engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 3*time.Second); err != nil {
				status.Problems = append(status.Problems, fmt.Sprintf("service healthcheck failed: %v", err))
			}
		}
	}
	if err := engine.checkManagedPermissions(*state); err != nil {
		status.Problems = append(status.Problems, err.Error())
	}
	if _, err := os.Lstat(current.CacheFile); err != nil {
		status.RepairCapability = "missing"
		status.Problems = append(status.Problems, "verified repair cache is unavailable")
	} else if current.CacheKind == "verified-archive" {
		status.RepairCapability = "verified-archive"
	} else {
		status.RepairCapability = "root-snapshot-limited"
	}
	if len(status.Problems) > 0 {
		status.Healthy = false
		if status.Deployment != "recovery-required" {
			status.Deployment = "degraded"
		}
	}
	return status, nil
}

func (engine *Engine) Doctor(ctx context.Context) (DoctorReport, error) {
	report := DoctorReport{SchemaVersion: stateSchemaVersion, Healthy: true}
	status, err := engine.Status(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	if status.Deployment == "absent" {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "installation-state", Status: "error", Detail: "remnanode-lite is not installed"})
		return report, nil
	}
	if status.Pending != nil || status.Deployment == "recovery-required" {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "transaction-journal", Status: "error", Detail: "an interrupted operation requires rnlctl repair"})
	} else {
		report.Checks = append(report.Checks, Check{Name: "transaction-journal", Status: "ok"})
	}
	state, stateErr := loadState(engine.paths)
	if stateErr != nil || state == nil {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "lifecycle-state", Status: "error", Detail: errorDetail(stateErr, "state is missing")})
		return report, nil
	}
	report.Checks = append(report.Checks, Check{Name: "lifecycle-state", Status: "ok", Detail: state.Current})
	if err := engine.checkSelectedLinks(*state); err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "generation-links", Status: "error", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, Check{Name: "generation-links", Status: "ok"})
	}
	if err := engine.checkManagedPermissions(*state); err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "managed-permissions", Status: "error", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, Check{Name: "managed-permissions", Status: "ok"})
	}
	for _, id := range orderedGenerationIDs(*state) {
		record := state.Generations[id]
		name := "generation:" + id
		if err := engine.verifyGeneration(record); err != nil {
			report.Healthy = false
			report.Checks = append(report.Checks, Check{Name: name, Status: "error", Detail: err.Error()})
		} else {
			report.Checks = append(report.Checks, Check{Name: name, Status: "ok"})
		}
		cacheName := "repair-cache:" + id
		bundle, err := openBundle(BundleInput{Archive: record.CacheFile, SHA256: record.ArchiveSHA256}, record.Architecture)
		if err != nil || bundle.Identity != record.Identity {
			report.Healthy = false
			detail := errorDetail(err, "cache identity mismatch")
			report.Checks = append(report.Checks, Check{Name: cacheName, Status: "error", Detail: detail})
			if bundle != nil {
				bundle.Close()
			}
		} else {
			bundle.Close()
			level := "ok"
			detail := record.CacheKind
			if record.CacheKind == "root-snapshot" {
				level = "warning"
				detail = "cache was synthesized from --bundle-root and lacks an external archive trust anchor"
			}
			report.Checks = append(report.Checks, Check{Name: cacheName, Status: level, Detail: detail})
		}
	}
	if state.Prepared {
		err = validatePreparedConfiguration(engine.paths)
	} else {
		err = validateRuntimeConfiguration(engine.paths)
	}
	if err != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "configuration", Status: "error", Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, Check{Name: "configuration", Status: "ok"})
	}
	service, serviceErr := engine.host.ServiceStatus(ctx)
	if serviceErr != nil {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "service", Status: "error", Detail: serviceErr.Error()})
	} else if service.Enabled != state.Desired.Enabled || service.Active != state.Desired.Active {
		report.Healthy = false
		report.Checks = append(report.Checks, Check{Name: "service", Status: "error", Detail: fmt.Sprintf("enabled=%t active=%t", service.Enabled, service.Active)})
	} else {
		report.Checks = append(report.Checks, Check{Name: "service", Status: "ok", Detail: service.Manager})
		if state.Desired.Active {
			if err := engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 3*time.Second); err != nil {
				report.Healthy = false
				report.Checks = append(report.Checks, Check{Name: "runtime-health", Status: "error", Detail: err.Error()})
			} else {
				report.Checks = append(report.Checks, Check{Name: "runtime-health", Status: "ok"})
			}
		}
	}
	return report, nil
}

func (engine *Engine) checkSelectedLinks(state persistentState) error {
	if err := requireSymlinkTarget(engine.paths.CurrentLink, filepath.Join(engine.paths.Generations, state.Current)); err != nil {
		return err
	}
	if state.Previous == "" {
		if _, err := os.Lstat(engine.paths.PreviousLink); err == nil {
			return fmt.Errorf("previous link exists without a retained generation")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := requireSymlinkTarget(engine.paths.PreviousLink, filepath.Join(engine.paths.Generations, state.Previous)); err != nil {
		return err
	}
	if err := requireSymlinkTarget(engine.paths.NodeBinaryLink, filepath.Join(engine.paths.CurrentLink, "bin", "remnanode-lite")); err != nil {
		return err
	}
	info, err := os.Lstat(engine.paths.ControlBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 {
		return fmt.Errorf("independent rnlctl binary is missing or unsafe")
	}
	if err := engine.verifyIndependentControl(state.Generations[state.Current]); err != nil {
		return err
	}
	return nil
}

func (engine *Engine) verifyIndependentControl(record generationRecord) error {
	root := filepath.Join(engine.paths.Generations, record.ID)
	raw, err := readRegularFile(filepath.Join(root, "release-manifest.json"), maxManifestBytes)
	if err != nil {
		return err
	}
	var manifest releaseManifest
	if err := decodeStrictJSON(raw, &manifest); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		if file.Path != "bin/rnlctl" {
			continue
		}
		digest, size, err := digestFile(engine.paths.ControlBinary, maxBundleFileBytes)
		if err != nil {
			return err
		}
		if digest != file.SHA256 || size != file.Size {
			return fmt.Errorf("independent rnlctl binary does not match the selected generation")
		}
		return nil
	}
	return fmt.Errorf("selected generation manifest does not describe bin/rnlctl")
}

func (engine *Engine) checkManagedPermissions(state persistentState) error {
	expectedGID, err := strconv.ParseUint(state.Account.GID, 10, 32)
	if err != nil {
		return fmt.Errorf("managed account gid is invalid")
	}
	for _, path := range []string{engine.paths.EnvironmentFile, engine.paths.SecretFile} {
		info, err := os.Lstat(path)
		if err != nil {
			if state.Prepared && path == engine.paths.SecretFile && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o640 {
			return fmt.Errorf("%s must be a regular 0640 file", path)
		}
		if engine.paths.LibraryRoot == ProductionPaths().LibraryRoot {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 || uint64(stat.Gid) != expectedGID {
				return fmt.Errorf("%s must be owned by root:remnanode", path)
			}
		}
	}
	if engine.paths.LibraryRoot == ProductionPaths().LibraryRoot {
		info, err := os.Lstat(engine.paths.ControlBinary)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || stat.Gid != 0 {
			return fmt.Errorf("%s must be owned by root:root", engine.paths.ControlBinary)
		}
	}
	return nil
}

func (engine *Engine) checkGenerationSurface(record generationRecord) error {
	root := filepath.Join(engine.paths.Generations, record.ID)
	for _, relative := range []string{"release-manifest.json", "bin/remnanode-lite", "lib/rw-core"} {
		info, err := os.Lstat(filepath.Join(root, relative))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("generation %s is missing %s", record.ID, relative)
		}
	}
	return nil
}

func requireSymlinkTarget(link, expected string) error {
	actual, err := os.Readlink(link)
	if err != nil {
		return fmt.Errorf("read managed link %s: %w", link, err)
	}
	if actual != expected {
		return fmt.Errorf("managed link %s targets %s, want %s", link, actual, expected)
	}
	return nil
}

func orderedGenerationIDs(state persistentState) []string {
	result := []string{state.Current}
	if state.Previous != "" {
		result = append(result, state.Previous)
	}
	return result
}

func errorDetail(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
