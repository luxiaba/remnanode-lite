package xray

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luxiaba/remnanode-lite/internal/executil"
)

var coreSHA256Pattern = regexp.MustCompile(`^[0-9A-Fa-f]{64}$`)

type coreSelectionMode uint8

const (
	coreUseBundled coreSelectionMode = iota
	coreKeepCurrent
	coreUseCustom
)

type customCore struct {
	url    string
	sha256 string
}

func (m *Manager) prepareCore(ctx context.Context, geodata any, hasGeodata bool, fallbackBinary string) (string, error) {
	mode, configured, parseErr := parseCustomCore(geodata, hasGeodata)
	if parseErr != nil {
		logInvalidPanelSection("core", parseErr)
	}
	switch mode {
	case coreUseBundled:
		return m.xrayBin, nil
	case coreKeepCurrent:
		return fallbackBinary, nil
	}

	coreDir := filepath.Join(m.panelRuntimeDir, "cores")
	if err := os.MkdirAll(coreDir, 0o750); err != nil {
		log.Printf("warning: failed to prepare custom Core directory: %v", err)
		return fallbackBinary, nil
	}
	destination := filepath.Join(coreDir, configured.sha256)
	if version, err := validateCoreCandidate(ctx, destination, configured.sha256); err == nil {
		log.Printf("using cached custom Core %s from %s", version, destination)
		return destination, nil
	} else if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if _, err := m.downloadPanelFile(ctx, configured.url, destination, configured.sha256, 0o755); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.Printf("warning: failed to download custom Core; continuing with current Core: %v", err)
		return fallbackBinary, nil
	}
	version, err := validateCoreCandidate(ctx, destination, configured.sha256)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		_ = os.Remove(destination)
		log.Printf("warning: downloaded custom Core is unusable; continuing with current Core: %v", err)
		return fallbackBinary, nil
	}
	log.Printf("prepared custom Core %s at %s", version, destination)
	return destination, nil
}

func parseCustomCore(geodata any, hasGeodata bool) (coreSelectionMode, customCore, error) {
	if !hasGeodata || geodata == nil {
		return coreUseBundled, customCore{}, nil
	}
	object, ok := geodata.(map[string]any)
	if !ok {
		return coreKeepCurrent, customCore{}, errors.New("geodata must be an object")
	}
	raw, exists := object["core"]
	if !exists {
		return coreUseBundled, customCore{}, nil
	}
	coreObject, ok := raw.(map[string]any)
	if !ok {
		return coreKeepCurrent, customCore{}, errors.New("core must be an object")
	}
	address, ok := coreObject["url"].(string)
	address = strings.TrimSpace(address)
	if !ok || !validHTTPSURL(address) {
		return coreKeepCurrent, customCore{}, errors.New("core.url must use HTTPS")
	}
	digest, ok := coreObject["sha256"].(string)
	if !ok || !coreSHA256Pattern.MatchString(digest) {
		return coreKeepCurrent, customCore{}, errors.New("core.sha256 must contain 64 hexadecimal characters")
	}
	return coreUseCustom, customCore{url: address, sha256: strings.ToLower(digest)}, nil
}

func validateCoreCandidate(ctx context.Context, path, expectedHash string) (string, error) {
	digest, err := hashRegularFile(ctx, path, panelDownloadMaxSize)
	if err != nil {
		return "", err
	}
	if digest != expectedHash {
		return "", fmt.Errorf("sha256 mismatch, got %s, expected %s", digest, expectedHash)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", fmt.Errorf("make custom Core executable: %w", err)
	}
	version, err := probeCoreBinary(ctx, path)
	if err != nil {
		return "", err
	}
	return version, nil
}

func hashRegularFile(ctx context.Context, path string, maxSize int64) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxSize {
		return "", errors.New("custom Core must be a non-empty bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = hasher.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func probeCoreBinary(parent context.Context, binary string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, versionProbeTimeout)
	defer cancel()
	result, err := executil.RunWithEnv(
		ctx,
		nil,
		versionOutputMaxSize,
		sanitizedChildEnvironment(os.Environ()),
		binary,
		"version",
	)
	if err != nil {
		return "", fmt.Errorf("read Core version: %w", err)
	}
	version := parseVersionLine(string(result.Stdout))
	if version == "" {
		return "", errors.New("Core version output does not contain semver")
	}
	return version, nil
}

func (m *Manager) cleanupCoreCache(selectedBinary string) {
	coreDir := filepath.Join(m.panelRuntimeDir, "cores")
	entries, err := os.ReadDir(coreDir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("warning: failed to inspect custom Core cache: %v", err)
		return
	}
	keep := ""
	if filepath.Clean(filepath.Dir(selectedBinary)) == filepath.Clean(coreDir) {
		keep = filepath.Base(selectedBinary)
	}
	for _, entry := range entries {
		if entry.Name() == keep || entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(coreDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("warning: failed to remove stale custom Core %s: %v", entry.Name(), err)
		}
	}
}
