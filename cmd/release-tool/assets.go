package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxXrayArchiveEntries = 64
	maxXrayEntryNameBytes = 256
)

type assetFetcher struct {
	cacheDir string
	offline  bool
	client   *http.Client
}

func newAssetFetcher(cacheDir string, offline bool) *assetFetcher {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if request.URL.Scheme != "https" {
				return fmt.Errorf("redirect target must use HTTPS")
			}
			return nil
		},
	}
	return &assetFetcher{cacheDir: cacheDir, offline: offline, client: client}
}

func (fetcher *assetFetcher) fetch(ctx context.Context, name string, artifact artifactLock) (string, error) {
	if fetcher.cacheDir == "" {
		return "", fmt.Errorf("asset cache directory is required")
	}
	if err := os.MkdirAll(fetcher.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create asset cache: %w", err)
	}
	target := filepath.Join(fetcher.cacheDir, artifact.SHA256)
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("cached %s is not a regular file", name)
		}
		if err := verifyFileIdentity(target, artifact.SHA256, artifact.Size); err != nil {
			return "", fmt.Errorf("cached %s failed verification: %w", name, err)
		}
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect cached %s: %w", name, err)
	}
	if fetcher.offline {
		return "", fmt.Errorf("cached %s is unavailable in offline mode", name)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create %s request: %w", name, err)
	}
	request.Header.Set("User-Agent", "remnanode-lite-release-tool/1")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: unexpected HTTP status %s", name, response.Status)
	}
	if response.ContentLength > artifact.Size {
		return "", fmt.Errorf("download %s: content length %d exceeds locked size %d", name, response.ContentLength, artifact.Size)
	}

	temporary, err := os.CreateTemp(fetcher.cacheDir, ".asset-*")
	if err != nil {
		return "", fmt.Errorf("create temporary cache file for %s: %w", name, err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure temporary cache file for %s: %w", name, err)
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, artifact.Size+1))
	if err != nil {
		return "", fmt.Errorf("download %s body: %w", name, err)
	}
	if written != artifact.Size {
		return "", fmt.Errorf("download %s: size %d does not match locked size %d", name, written, artifact.Size)
	}
	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != artifact.SHA256 {
		return "", fmt.Errorf("download %s: SHA-256 %s does not match locked digest %s", name, actualDigest, artifact.SHA256)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync cached %s: %w", name, err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close cached %s: %w", name, err)
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return "", fmt.Errorf("set cached %s permissions: %w", name, err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		if info, statErr := os.Lstat(target); statErr == nil && info.Mode().IsRegular() {
			if verifyErr := verifyFileIdentity(target, artifact.SHA256, artifact.Size); verifyErr == nil {
				return target, nil
			}
		}
		return "", fmt.Errorf("publish cached %s: %w", name, err)
	}
	keep = true
	return target, nil
}

func verifyFileIdentity(filePath, wantDigest string, wantSize int64) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if info.Size() != wantSize {
		return fmt.Errorf("size %d does not match %d", info.Size(), wantSize)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != wantDigest {
		return fmt.Errorf("SHA-256 %s does not match %s", actual, wantDigest)
	}
	return nil
}

func extractXrayAssets(archivePath string, architecture xrayArchitecture) (map[string][]byte, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open Xray archive: %w", err)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > maxXrayArchiveEntries {
		return nil, fmt.Errorf("Xray archive contains %d entries; limit is %d", len(archive.File), maxXrayArchiveEntries)
	}

	wanted := map[string]archiveEntry{
		architecture.Core.ArchivePath: architecture.Core,
	}
	result := make(map[string][]byte, len(wanted))
	seen := make(map[string]struct{}, len(archive.File))
	for _, file := range archive.File {
		if err := validateArchiveRelativePath(file.Name, maxXrayEntryNameBytes); err != nil {
			return nil, fmt.Errorf("unsafe Xray archive entry %q: %w", file.Name, err)
		}
		if _, exists := seen[file.Name]; exists {
			return nil, fmt.Errorf("duplicate Xray archive entry %q", file.Name)
		}
		seen[file.Name] = struct{}{}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("Xray archive entry %q is not a regular file", file.Name)
		}
		expected, required := wanted[file.Name]
		if !required {
			continue
		}
		if int64(file.UncompressedSize64) != expected.Size {
			return nil, fmt.Errorf("Xray entry %q declares size %d, want %d", file.Name, file.UncompressedSize64, expected.Size)
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open Xray entry %q: %w", file.Name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, expected.Size+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Xray entry %q: %w", file.Name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close Xray entry %q: %w", file.Name, closeErr)
		}
		if int64(len(data)) != expected.Size {
			return nil, fmt.Errorf("Xray entry %q has size %d, want %d", file.Name, len(data), expected.Size)
		}
		actualDigest := digestBytes(data)
		if actualDigest != expected.SHA256 {
			return nil, fmt.Errorf("Xray entry %q SHA-256 %s does not match %s", file.Name, actualDigest, expected.SHA256)
		}
		result[file.Name] = data
	}
	for name := range wanted {
		if _, exists := result[name]; !exists {
			return nil, fmt.Errorf("Xray archive is missing required root entry %q", name)
		}
	}
	return result, nil
}

func validateArchiveRelativePath(name string, maxBytes int) error {
	if name == "" || len(name) > maxBytes {
		return fmt.Errorf("path length must be between 1 and %d bytes", maxBytes)
	}
	if strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("path must be a portable relative slash-separated path")
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned != strings.TrimSuffix(name, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path is not normalized")
	}
	return nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
