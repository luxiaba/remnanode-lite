package xray

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseGeoDataAssetsValidatesWholeSection(t *testing.T) {
	valid := map[string]any{"assets": []any{
		map[string]any{"url": "https://example.com/geo.dat", "file": "geo-custom_1.dat"},
	}}
	assets, err := parseGeoDataAssets(valid)
	if err != nil || len(assets) != 1 || assets[0].file != "geo-custom_1.dat" {
		t.Fatalf("parse valid assets = %#v, %v", assets, err)
	}

	for _, name := range []string{".", "..", "../geo.dat", "nested/geo.dat", "geo 数据.dat"} {
		_, err := parseGeoDataAssets(map[string]any{"assets": []any{
			map[string]any{"url": "https://example.com/geo.dat", "file": name},
		}})
		if err == nil {
			t.Errorf("file name %q unexpectedly accepted", name)
		}
	}
	_, err = parseGeoDataAssets(map[string]any{"assets": []any{
		map[string]any{"url": "http://example.com/geo.dat", "file": "geo.dat"},
	}})
	if err == nil {
		t.Fatal("HTTP asset URL unexpectedly accepted")
	}
}

func TestPrepareGeoDataUsesOverlayReuseAndFailureStub(t *testing.T) {
	bundled := t.TempDir()
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if err := os.WriteFile(filepath.Join(bundled, name), []byte("bundled "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runtimeDir := t.TempDir()
	assetDir := filepath.Join(runtimeDir, "assets")
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "reuse.dat"), []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := &Manager{geoDir: bundled, panelRuntimeDir: runtimeDir}
	var downloads atomic.Int32
	manager.downloadPanelFile = func(_ context.Context, address, destination, _ string, mode os.FileMode) (downloadResult, error) {
		downloads.Add(1)
		if strings.Contains(address, "failure") {
			return downloadResult{}, errors.New("network unavailable")
		}
		body := []byte("downloaded")
		if err := os.WriteFile(destination, body, mode); err != nil {
			return downloadResult{}, err
		}
		return downloadResult{size: int64(len(body))}, nil
	}

	gotDir, err := manager.prepareGeoData(context.Background(), map[string]any{"assets": []any{
		map[string]any{"url": "https://example.com/reuse", "file": "reuse.dat"},
		map[string]any{"url": "https://example.com/new", "file": "new.dat"},
		map[string]any{"url": "https://example.com/failure", "file": "failed.dat"},
	}}, true)
	if err != nil {
		t.Fatalf("prepareGeoData: %v", err)
	}
	if gotDir != assetDir || downloads.Load() != 2 {
		t.Fatalf("asset dir/downloads = %q/%d", gotDir, downloads.Load())
	}
	if got, err := os.ReadFile(filepath.Join(assetDir, "reuse.dat")); err != nil || string(got) != "cached" {
		t.Fatalf("cached asset = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(assetDir, "new.dat")); err != nil || string(got) != "downloaded" {
		t.Fatalf("downloaded asset = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(assetDir, "failed.dat")); err != nil || info.Size() != 0 || !info.Mode().IsRegular() {
		t.Fatalf("failed asset stub = %#v, %v", info, err)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		target, err := os.Readlink(filepath.Join(assetDir, name))
		if err != nil || target != filepath.Join(bundled, name) {
			t.Fatalf("overlay link %s = %q, %v", name, target, err)
		}
	}
}

func TestInvalidAssetsDoNotAffectCoreParsing(t *testing.T) {
	digest := strings.Repeat("A", 64)
	mode, core, err := parseCustomCore(map[string]any{
		"core":   map[string]any{"url": "https://example.com/core", "sha256": digest},
		"assets": "invalid",
	}, true)
	if err != nil || mode != coreUseCustom || core.sha256 != strings.ToLower(digest) {
		t.Fatalf("core parse = %v %#v %v", mode, core, err)
	}
}
