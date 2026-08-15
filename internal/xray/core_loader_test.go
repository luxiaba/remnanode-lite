package xray

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseCustomCoreModesAndValidation(t *testing.T) {
	digest := strings.Repeat("A", 64)
	mode, core, err := parseCustomCore(map[string]any{"core": map[string]any{
		"url":    "https://example.com/rw-core",
		"sha256": digest,
	}}, true)
	if err != nil || mode != coreUseCustom || core.sha256 != strings.ToLower(digest) {
		t.Fatalf("valid core = %v %#v %v", mode, core, err)
	}
	if mode, _, err := parseCustomCore(map[string]any{}, true); mode != coreUseBundled || err != nil {
		t.Fatalf("missing core = %v %v", mode, err)
	}
	for _, geodata := range []any{
		"invalid",
		map[string]any{"core": nil},
		map[string]any{"core": map[string]any{"url": "http://example.com/core", "sha256": digest}},
		map[string]any{"core": map[string]any{"url": "https://example.com/core", "sha256": "short"}},
	} {
		if mode, _, err := parseCustomCore(geodata, true); mode != coreKeepCurrent || err == nil {
			t.Errorf("invalid core %#v = %v, %v", geodata, mode, err)
		}
	}
}

func TestPrepareCoreUsesContentAddressedVerifiedCache(t *testing.T) {
	script := []byte("#!/bin/sh\nif env | grep -q '^SECRET_KEY='; then exit 9; fi\necho 'Xray 26.7.28-rc.1+rw.2'\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(script))
	manager := &Manager{xrayBin: "/bundled/rw-core", panelRuntimeDir: t.TempDir()}
	var downloads atomic.Int32
	manager.downloadPanelFile = func(_ context.Context, _ string, destination, expected string, mode os.FileMode) (downloadResult, error) {
		downloads.Add(1)
		if expected != digest {
			t.Fatalf("expected digest = %q", expected)
		}
		if err := os.WriteFile(destination, script, mode); err != nil {
			return downloadResult{}, err
		}
		return downloadResult{sha256: digest, size: int64(len(script))}, nil
	}
	t.Setenv("SECRET_KEY", "must-not-reach-core")
	geodata := map[string]any{"core": map[string]any{
		"url":    "https://example.com/rw-core",
		"sha256": strings.ToUpper(digest),
	}}

	selected, err := manager.prepareCore(context.Background(), geodata, true, "/current/rw-core")
	if err != nil {
		t.Fatalf("prepareCore: %v", err)
	}
	want := filepath.Join(manager.panelRuntimeDir, "cores", digest)
	if selected != want || downloads.Load() != 1 {
		t.Fatalf("selected/downloads = %q/%d, want %q/1", selected, downloads.Load(), want)
	}
	selected, err = manager.prepareCore(context.Background(), geodata, true, "/current/rw-core")
	if err != nil || selected != want || downloads.Load() != 1 {
		t.Fatalf("cached selected/downloads = %q/%d, %v", selected, downloads.Load(), err)
	}
}

func TestPrepareCoreFallsBackWithoutSelectingInvalidDownload(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
		err  error
	}{
		{name: "download failure", err: errors.New("offline")},
		{name: "invalid version", body: []byte("#!/bin/sh\necho no-version\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest := fmt.Sprintf("%x", sha256.Sum256(test.body))
			if test.err != nil {
				digest = strings.Repeat("a", 64)
			}
			manager := &Manager{xrayBin: "/bundled/rw-core", panelRuntimeDir: t.TempDir()}
			manager.downloadPanelFile = func(_ context.Context, _ string, destination, _ string, mode os.FileMode) (downloadResult, error) {
				if test.err != nil {
					return downloadResult{}, test.err
				}
				if err := os.WriteFile(destination, test.body, mode); err != nil {
					return downloadResult{}, err
				}
				return downloadResult{sha256: digest}, nil
			}
			selected, err := manager.prepareCore(context.Background(), map[string]any{"core": map[string]any{
				"url": "https://example.com/core", "sha256": digest,
			}}, true, "/current/rw-core")
			if err != nil || selected != "/current/rw-core" {
				t.Fatalf("selected = %q, %v", selected, err)
			}
			candidate := filepath.Join(manager.panelRuntimeDir, "cores", digest)
			if test.err == nil {
				if _, statErr := os.Stat(candidate); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("invalid candidate remains: %v", statErr)
				}
			}
		})
	}
}

func TestCleanupCoreCacheKeepsOnlyCommittedCustomCore(t *testing.T) {
	manager := &Manager{panelRuntimeDir: t.TempDir()}
	coreDir := filepath.Join(manager.panelRuntimeDir, "cores")
	if err := os.MkdirAll(filepath.Join(coreDir, "directory"), 0o750); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(coreDir, strings.Repeat("a", 64))
	stale := filepath.Join(coreDir, strings.Repeat("b", 64))
	for _, path := range []string{selected, stale} {
		if err := os.WriteFile(path, []byte("core"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	manager.cleanupCoreCache(selected)
	if _, err := os.Stat(selected); err != nil {
		t.Fatalf("selected Core removed: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale Core remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(coreDir, "directory")); err != nil {
		t.Fatalf("unexpected cache directory removed: %v", err)
	}

	manager.cleanupCoreCache("/bundled/rw-core")
	if _, err := os.Stat(selected); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("custom Core remains after bundled commit: %v", err)
	}
}

func TestCustomBinaryVersionProbeDoesNotUseBundledOverride(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "custom-core")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'Xray 27.1.2-custom.1'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{xrayBin: "/bundled/rw-core"}
	manager.version.coreOverride = "26.7.28"
	version := manager.probeVersionForBinary(context.Background(), binary)
	if version == nil || *version != "27.1.2-custom.1" {
		t.Fatalf("custom version = %#v", version)
	}
}
