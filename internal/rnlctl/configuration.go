package rnlctl

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const configurationSchemaVersion = 1

var editableConfigurationKeys = []string{
	"NODE_PORT",
	"NODE_BIND_ADDR",
	"LOW_MEMORY",
	"BODY_LIMIT_MB",
	"GOMEMLIMIT",
	"DISABLE_HASHED_SET_CHECK",
}

var editableConfigurationKeySet = func() map[string]struct{} {
	keys := make(map[string]struct{}, len(editableConfigurationKeys))
	for _, key := range editableConfigurationKeys {
		keys[key] = struct{}{}
	}
	return keys
}()

func (engine *Engine) ReadConfiguration(_ context.Context) (Configuration, error) {
	raw, err := readRegularFile(engine.paths.EnvironmentFile, maxEnvironmentBytes)
	if err != nil {
		return Configuration{}, fmt.Errorf("read node.env: %w", err)
	}
	assignments, err := parseEnvironmentAssignments(raw)
	if err != nil {
		return Configuration{}, err
	}
	values := make(map[string]string, len(editableConfigurationKeys))
	for _, key := range editableConfigurationKeys {
		values[key] = assignments[key]
	}
	return Configuration{
		SchemaVersion: configurationSchemaVersion,
		Path:          engine.paths.EnvironmentFile,
		Values:        values,
	}, nil
}

func (engine *Engine) CheckConfiguration(_ context.Context) error {
	state, err := loadState(engine.paths)
	if err != nil {
		return err
	}
	if state == nil {
		return ErrNotInstalled
	}
	if err := engine.checkConfigurationPermissions(*state); err != nil {
		return err
	}
	return engine.validateConfigurationForState(state)
}

func (engine *Engine) UpdateConfiguration(ctx context.Context, request ConfigurationUpdateRequest) (Result, error) {
	operation := "config-set"
	if len(request.Set) == 0 {
		operation = "config-unset"
	}
	if err := validateConfigurationUpdate(request); err != nil {
		return Result{}, err
	}
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	if request.Apply {
		if err := engine.host.Preflight(ctx, true, engine.paths); err != nil {
			return Result{}, err
		}
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
	if request.Apply {
		if err := engine.requireActiveConfigurationTarget(ctx, state); err != nil {
			return Result{}, err
		}
	}
	snapshot, err := snapshotFile(engine.paths.EnvironmentFile, maxEnvironmentBytes)
	if err != nil {
		return Result{}, fmt.Errorf("snapshot node.env: %w", err)
	}
	candidate, err := rewriteEditableConfiguration(snapshot.data, request.Set, request.Unset)
	if err != nil {
		return Result{}, err
	}
	if err := validateManagedConfigurationData(candidate, engine.paths, !state.Prepared); err != nil {
		return Result{}, err
	}

	fileChanged := !bytes.Equal(snapshot.data, candidate)
	if fileChanged {
		if err := atomicWriteFile(engine.paths.EnvironmentFile, candidate, 0o640); err != nil {
			return Result{}, fmt.Errorf("write node.env: %w", err)
		}
		if err := engine.host.ApplyOwnership(ctx, engine.paths); err != nil {
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, false)
			return Result{}, errors.Join(fmt.Errorf("set managed ownership after writing node.env: %w", err), rollbackErr)
		}
	}
	if request.Apply {
		if err := engine.checkConfigurationPermissions(*state); err != nil {
			if !fileChanged {
				return Result{}, err
			}
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, false)
			return Result{}, errors.Join(err, rollbackErr)
		}
		if err := engine.restartManagedConfiguration(ctx); err != nil {
			if !fileChanged {
				return Result{}, err
			}
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, true)
			return Result{}, errors.Join(err, rollbackErr)
		}
	}
	current := state.Generations[state.Current]
	return Result{
		Operation:  operation,
		Changed:    fileChanged || request.Apply,
		Generation: current.ID,
		Version:    current.Version,
	}, nil
}

func (engine *Engine) ApplyConfiguration(ctx context.Context) (Result, error) {
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	if err := engine.host.Preflight(ctx, true, engine.paths); err != nil {
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
	if err := engine.requireActiveConfigurationTarget(ctx, state); err != nil {
		return Result{}, err
	}
	if err := engine.checkConfigurationPermissions(*state); err != nil {
		return Result{}, err
	}
	if err := engine.validateConfigurationForState(state); err != nil {
		return Result{}, err
	}
	if err := engine.restartManagedConfiguration(ctx); err != nil {
		return Result{}, err
	}
	current := state.Generations[state.Current]
	return Result{Operation: "config-apply", Changed: true, Generation: current.ID, Version: current.Version}, nil
}

func (engine *Engine) SetSecret(ctx context.Context, request SecretUpdateRequest) (Result, error) {
	if strings.TrimSpace(request.File) == "" {
		return Result{}, fmt.Errorf("--file is required")
	}
	if err := engine.requirePrivileges(); err != nil {
		return Result{}, err
	}
	if request.Apply {
		if err := engine.host.Preflight(ctx, true, engine.paths); err != nil {
			return Result{}, err
		}
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
	if request.Apply {
		if err := engine.requireActiveConfigurationTarget(ctx, state); err != nil {
			return Result{}, err
		}
	}
	secretData, err := readSecretSource(request.File)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := snapshotFile(engine.paths.SecretFile, maxEnvironmentBytes)
	if err != nil {
		return Result{}, fmt.Errorf("snapshot Secret Key: %w", err)
	}
	fileChanged := !snapshot.exists || !bytes.Equal(snapshot.data, secretData)
	if fileChanged {
		if err := atomicWriteFile(engine.paths.SecretFile, secretData, 0o640); err != nil {
			return Result{}, fmt.Errorf("write Secret Key: %w", err)
		}
		if err := engine.host.ApplyOwnership(ctx, engine.paths); err != nil {
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, false)
			return Result{}, errors.Join(fmt.Errorf("set managed ownership after writing Secret Key: %w", err), rollbackErr)
		}
	}
	if fileChanged || request.Apply {
		if err := engine.validateConfigurationForState(state); err != nil {
			if !fileChanged {
				return Result{}, err
			}
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, false)
			return Result{}, errors.Join(err, rollbackErr)
		}
	}
	if request.Apply {
		if err := engine.checkConfigurationPermissions(*state); err != nil {
			if !fileChanged {
				return Result{}, err
			}
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, false)
			return Result{}, errors.Join(err, rollbackErr)
		}
		if err := engine.restartManagedConfiguration(ctx); err != nil {
			if !fileChanged {
				return Result{}, err
			}
			rollbackErr := engine.restoreManagedFile(ctx, snapshot, true)
			return Result{}, errors.Join(err, rollbackErr)
		}
	}
	current := state.Generations[state.Current]
	return Result{
		Operation:  "secret-set",
		Changed:    fileChanged || request.Apply,
		Generation: current.ID,
		Version:    current.Version,
	}, nil
}

func (engine *Engine) validateConfigurationForState(state *persistentState) error {
	if state.Prepared {
		return validatePreparedConfiguration(engine.paths)
	}
	return validateRuntimeConfiguration(engine.paths)
}

func (engine *Engine) requireActiveConfigurationTarget(ctx context.Context, state *persistentState) error {
	if state.Prepared {
		return fmt.Errorf("prepared installations must be enabled with rnlctl activate")
	}
	service, err := engine.host.ServiceStatus(ctx)
	if err != nil {
		return err
	}
	if !state.Desired.Active || !service.Active {
		return fmt.Errorf("service is stopped; change the configuration without --apply, then use rnlctl start")
	}
	return nil
}

func (engine *Engine) restartManagedConfiguration(ctx context.Context) error {
	if err := engine.host.Restart(ctx); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	if err := engine.host.WaitHealthy(ctx, engine.paths.NodeBinaryLink, engine.internalSocketPath(), 25*time.Second); err != nil {
		return fmt.Errorf("verify restarted service: %w", err)
	}
	return nil
}

func (engine *Engine) restoreManagedFile(ctx context.Context, snapshot fileSnapshot, restart bool) error {
	if err := snapshot.restore(); err != nil {
		return fmt.Errorf("restore previous %s: %w", snapshot.path, err)
	}
	if err := engine.host.ApplyOwnership(ctx, engine.paths); err != nil {
		return fmt.Errorf("restore managed ownership: %w", err)
	}
	if restart {
		if err := engine.restartManagedConfiguration(ctx); err != nil {
			return fmt.Errorf("restore service with previous configuration: %w", err)
		}
	}
	return nil
}

func validateConfigurationUpdate(request ConfigurationUpdateRequest) error {
	if len(request.Set) == 0 && len(request.Unset) == 0 {
		return fmt.Errorf("at least one configuration assignment or key is required")
	}
	seen := make(map[string]struct{}, len(request.Set)+len(request.Unset))
	for key, value := range request.Set {
		if err := validateEditableConfigurationKey(key); err != nil {
			return err
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("configuration key %s is repeated", key)
		}
		seen[key] = struct{}{}
		if err := validateConfigurationValue(key, value); err != nil {
			return err
		}
	}
	for _, key := range request.Unset {
		if err := validateEditableConfigurationKey(key); err != nil {
			return err
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("configuration key %s is repeated", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateConfigurationValue(key, value string) error {
	if !utf8.ValidString(value) || strings.Contains(value, `\n`) || strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return fmt.Errorf("%s contains unsupported whitespace or control data", key)
	}
	return nil
}

func validateEditableConfigurationKey(key string) error {
	if _, ok := editableConfigurationKeySet[key]; !ok {
		return fmt.Errorf("configuration key %q is not administrator-editable", key)
	}
	return nil
}

func rewriteEditableConfiguration(input []byte, set map[string]string, unset []string) ([]byte, error) {
	if _, err := parseEnvironmentAssignments(input); err != nil {
		return nil, err
	}
	remove := make(map[string]struct{}, len(unset))
	for _, key := range unset {
		remove[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(set))
	var output bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(input))
	scanner.Buffer(make([]byte, 64<<10), maxEnvironmentBytes)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			assignment := strings.TrimPrefix(trimmed, "export ")
			if index := strings.IndexByte(assignment, '='); index > 0 {
				key := strings.TrimSpace(assignment[:index])
				if _, ok := remove[key]; ok {
					continue
				}
				if value, ok := set[key]; ok {
					line = key + "=" + value
					seen[key] = struct{}{}
				}
			}
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, key := range editableConfigurationKeys {
		value, requested := set[key]
		if !requested {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		output.WriteString(key + "=" + value + "\n")
	}
	return output.Bytes(), nil
}

func editableConfigurationOrder() []string {
	return append([]string(nil), editableConfigurationKeys...)
}

func isEditableConfigurationKey(key string) bool {
	_, ok := editableConfigurationKeySet[key]
	return ok
}
