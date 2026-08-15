package xray

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const geodataDownloadConcurrency = 5

var geodataFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type geodataAsset struct {
	url  string
	file string
}

func (m *Manager) prepareGeoData(ctx context.Context, geodata any, hasGeodata bool) (string, error) {
	if !hasGeodata || geodata == nil {
		return m.geoDir, nil
	}
	object, ok := geodata.(map[string]any)
	if !ok {
		logInvalidPanelSection("assets", errors.New("geodata must be an object"))
		return m.geoDir, nil
	}

	assetDir := filepath.Join(m.panelRuntimeDir, "assets")
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		return "", fmt.Errorf("create Panel GeoData directory: %w", err)
	}
	if err := seedBundledGeoData(assetDir, m.geoDir); err != nil {
		return "", err
	}

	assets, err := parseGeoDataAssets(object)
	if err != nil {
		logInvalidPanelSection("assets", err)
		return assetDir, nil
	}
	if len(assets) == 0 {
		return assetDir, nil
	}
	if err := m.prepareGeoDataAssets(ctx, assetDir, assets); err != nil {
		return "", err
	}
	log.Printf("Panel GeoData preparation processed %d asset(s)", len(assets))
	return assetDir, nil
}

func parseGeoDataAssets(geodata map[string]any) ([]geodataAsset, error) {
	raw, exists := geodata["assets"]
	if !exists {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("assets must be an array")
	}
	assets := make([]geodataAsset, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("assets[%d] must be an object", index)
		}
		address, ok := object["url"].(string)
		address = strings.TrimSpace(address)
		if !ok || !validHTTPSURL(address) {
			return nil, fmt.Errorf("assets[%d].url must use HTTPS", index)
		}
		name, ok := object["file"].(string)
		if !ok || name == "." || name == ".." || !geodataFileNamePattern.MatchString(name) {
			return nil, fmt.Errorf("assets[%d].file must be an ASCII file name without a path", index)
		}
		assets = append(assets, geodataAsset{url: address, file: name})
	}
	return assets, nil
}

func validHTTPSURL(address string) bool {
	parsed, err := url.ParseRequestURI(address)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func seedBundledGeoData(assetDir, bundledDir string) error {
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		destination := filepath.Join(assetDir, name)
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect GeoData overlay %s: %w", name, err)
		}
		if err := os.Symlink(filepath.Join(bundledDir, name), destination); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("seed GeoData overlay %s: %w", name, err)
		}
	}
	return nil
}

func (m *Manager) prepareGeoDataAssets(ctx context.Context, assetDir string, assets []geodataAsset) error {
	workerCount := min(geodataDownloadConcurrency, len(assets))
	jobs := make(chan geodataAsset)
	var workers sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for asset := range jobs {
				if err := m.prepareGeoDataAsset(ctx, assetDir, asset); err != nil {
					errOnce.Do(func() { firstErr = err })
				}
			}
		}()
	}
	for _, asset := range assets {
		select {
		case jobs <- asset:
		case <-ctx.Done():
			errOnce.Do(func() { firstErr = ctx.Err() })
			close(jobs)
			workers.Wait()
			return firstErr
		}
	}
	close(jobs)
	workers.Wait()
	return firstErr
}

func (m *Manager) prepareGeoDataAsset(ctx context.Context, assetDir string, asset geodataAsset) error {
	destination := filepath.Join(assetDir, asset.file)
	if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return nil
	}
	if _, err := m.downloadPanelFile(ctx, asset.url, destination, "", 0o644); err == nil {
		log.Printf("downloaded Panel GeoData asset %s", asset.file)
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	} else {
		log.Printf("warning: failed to download Panel GeoData asset %s: %v", asset.url, err)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_ = file.Close()
		log.Printf("warning: created empty GeoData stub %s", destination)
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		log.Printf("warning: failed to create GeoData stub %s: %v", destination, err)
	}
	return nil
}
