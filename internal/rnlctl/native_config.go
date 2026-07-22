package rnlctl

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luxiaba/remnanode-lite/internal/secret"
)

const (
	defaultNativePort   = 2222
	maxEnvironmentBytes = 1 << 20
)

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

func snapshotFile(path string, limit int64) (fileSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileSnapshot{path: path}, nil
		}
		return fileSnapshot{}, err
	}
	data, err := readRegularFile(path, limit)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{path: path, exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func (snapshot fileSnapshot) restore() error {
	if snapshot.exists {
		return atomicWriteFile(snapshot.path, snapshot.data, snapshot.mode)
	}
	err := removeAndSync(snapshot.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readSecretSource(source string) ([]byte, error) {
	if source == "" {
		return nil, nil
	}
	data, err := readRegularFile(source, secret.MaxEncodedBytes+1)
	if err != nil {
		return nil, fmt.Errorf("read --secret-file: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if _, err := secret.Parse(value); err != nil {
		return nil, fmt.Errorf("validate --secret-file: %w", err)
	}
	return append([]byte(value), '\n'), nil
}

func readExistingSecret(path string) ([]byte, error) {
	data, err := readRegularFile(path, secret.MaxEncodedBytes+1)
	if err != nil {
		return nil, err
	}
	value := strings.TrimSpace(string(data))
	if _, err := secret.Parse(value); err != nil {
		return nil, fmt.Errorf("validate existing secret: %w", err)
	}
	return append([]byte(value), '\n'), nil
}

func effectiveInstallSecret(request InstallRequest, paths Paths) ([]byte, error) {
	if request.SecretFile != "" {
		return readSecretSource(request.SecretFile)
	}
	data, err := readExistingSecret(paths.SecretFile)
	if err == nil {
		return data, nil
	}
	if request.PrepareOnly && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("a valid Secret Key is required; pass it with --secret-file")
	}
	return nil, err
}

func effectiveActivationSecret(request ActivateRequest, paths Paths) ([]byte, error) {
	if request.SecretFile != "" {
		return readSecretSource(request.SecretFile)
	}
	data, err := readExistingSecret(paths.SecretFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("a valid Secret Key is required; pass it with --secret-file")
	}
	return data, err
}

func prepareEnvironment(bundleRoot string, paths Paths, requestedPort int) ([]byte, int, error) {
	existing, err := readRegularFile(paths.EnvironmentFile, maxEnvironmentBytes)
	if err == nil {
		assignments, parseErr := parseEnvironmentAssignments(existing)
		if parseErr != nil {
			return nil, 0, parseErr
		}
		port, parseErr := parseNativePort(assignments["NODE_PORT"])
		if parseErr != nil {
			return nil, 0, parseErr
		}
		if requestedPort != 0 {
			if requestedPort < 1 || requestedPort > 65535 {
				return nil, 0, fmt.Errorf("--port must be between 1 and 65535")
			}
			port = requestedPort
		}
		updated, renderErr := rewriteManagedEnvironment(existing, managedEnvironmentValues(paths, port))
		return updated, port, renderErr
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("read existing node.env: %w", err)
	}
	port := requestedPort
	if port == 0 {
		port = defaultNativePort
	}
	if port < 1 || port > 65535 {
		return nil, 0, fmt.Errorf("--port must be between 1 and 65535")
	}
	template, err := readRegularFile(filepath.Join(bundleRoot, "support", "deploy", "node.env.example"), maxEnvironmentBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("read node.env template: %w", err)
	}
	rendered, err := rewriteManagedEnvironment(template, managedEnvironmentValues(paths, port))
	return rendered, port, err
}

func managedEnvironmentValues(paths Paths, port int) map[string]string {
	return map[string]string{
		"NODE_PORT":            strconv.Itoa(port),
		"SECRET_KEY":           "",
		"SECRET_KEY_FILE":      paths.SecretFile,
		"XRAY_BIN":             filepath.Join(paths.CurrentLink, "lib", "rw-core"),
		"GEO_DIR":              filepath.Join(paths.CurrentLink, "share", "xray"),
		"LOG_DIR":              paths.LogDirectory,
		"ASN_DB_PATH":          filepath.Join(paths.CurrentLink, "share", "asn", "asn-prefixes.bin"),
		"INTERNAL_SOCKET_PATH": filepath.Join(paths.RuntimeDirectory, "internal.sock"),
	}
}

func rewriteManagedEnvironment(input []byte, values map[string]string) ([]byte, error) {
	seen := make(map[string]bool, len(values))
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
				if value, managed := values[key]; managed {
					if seen[key] {
						return nil, fmt.Errorf("node.env repeats managed key %s", key)
					}
					seen[key] = true
					line = key + "=" + value
				}
			}
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	keys := []string{"NODE_PORT", "SECRET_KEY", "SECRET_KEY_FILE", "XRAY_BIN", "GEO_DIR", "LOG_DIR", "ASN_DB_PATH", "INTERNAL_SOCKET_PATH"}
	for _, key := range keys {
		if !seen[key] {
			output.WriteString(key + "=" + values[key] + "\n")
		}
	}
	return output.Bytes(), nil
}

func parseEnvironmentAssignments(data []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxEnvironmentBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("node.env:%d is not an assignment", lineNumber)
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, fmt.Errorf("node.env:%d has an empty key", lineNumber)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("node.env repeats key %s", key)
		}
		values[key] = unquoteEnvironment(strings.TrimSpace(parts[1]))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func unquoteEnvironment(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

func parseNativePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("node.env NODE_PORT must be between 1 and 65535")
	}
	return port, nil
}

func validateRuntimeConfiguration(paths Paths) error {
	return validateManagedConfiguration(paths, true)
}

func validatePreparedConfiguration(paths Paths) error {
	return validateManagedConfiguration(paths, false)
}

func validateManagedConfiguration(paths Paths, requireSecret bool) error {
	environment, err := readRegularFile(paths.EnvironmentFile, maxEnvironmentBytes)
	if err != nil {
		return fmt.Errorf("read node.env: %w", err)
	}
	values, err := parseEnvironmentAssignments(environment)
	if err != nil {
		return err
	}
	if _, err := parseNativePort(values["NODE_PORT"]); err != nil {
		return err
	}
	want := managedEnvironmentValues(paths, defaultNativePort)
	for _, key := range []string{"SECRET_KEY_FILE", "XRAY_BIN", "GEO_DIR", "LOG_DIR", "ASN_DB_PATH", "INTERNAL_SOCKET_PATH"} {
		if values[key] != want[key] {
			return fmt.Errorf("node.env %s=%q, want managed path %q", key, values[key], want[key])
		}
	}
	if strings.TrimSpace(values["SECRET_KEY"]) != "" {
		return fmt.Errorf("node.env must not contain an inline SECRET_KEY")
	}
	if _, err := readExistingSecret(paths.SecretFile); err != nil {
		if !requireSecret && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}
