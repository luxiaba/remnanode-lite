package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryRuntimeLockIsValid(t *testing.T) {
	_, err := loadRuntimeLock(filepath.Join("..", "..", "release", "runtime-assets.lock.json"))
	if err != nil {
		t.Fatalf("load repository runtime lock: %v", err)
	}
}

func TestRuntimeLockRejectsUnknownAndDuplicateFields(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "release", "runtime-assets.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"unknown field":         strings.Replace(string(original), "\"schemaVersion\": 2,", "\"schemaVersion\": 2, \"unexpected\": true,", 1),
		"duplicate key":         strings.Replace(string(original), "\"schemaVersion\": 2,", "\"schemaVersion\": 2, \"schemaVersion\": 2,", 1),
		"uppercase digest":      strings.Replace(string(original), "b3e5902d", "B3e5902d", 1),
		"escaped URL separator": strings.Replace(string(original), "/releases/download/", "%2Freleases/download/", 1),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRuntimeLock(path); err == nil {
				t.Fatal("loadRuntimeLock accepted malformed lock")
			}
		})
	}
}

func TestRepositoryNoticesMatchRuntimeLock(t *testing.T) {
	lock, err := loadRuntimeLock(filepath.Join("..", "..", "release", "runtime-assets.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile(filepath.Join("..", "..", "release", "bundle", "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateThirdPartyNotices(lock, notices); err != nil {
		t.Fatalf("validate repository notices: %v", err)
	}
	lock.GeoSite.Commit = strings.Repeat("f", 40)
	lock.GeoSite.SourceURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/tree/" + lock.GeoSite.Commit
	lock.GeoSite.SourceArtifact.URL = "https://github.com/Loyalsoldier/v2ray-rules-dat/archive/" + lock.GeoSite.Commit + ".tar.gz"
	if err := validateThirdPartyNotices(lock, notices); err == nil {
		t.Fatal("notices validation accepted a lock with different GeoSite provenance")
	}
}

func TestRepositorySourceOfferMatchesRuntimeLock(t *testing.T) {
	lock, err := loadRuntimeLock(filepath.Join("..", "..", "release", "runtime-assets.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	sourceOffer, err := os.ReadFile(filepath.Join("..", "..", "release", "bundle", "SOURCE-OFFER.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSourceOffer(lock, sourceOffer); err != nil {
		t.Fatalf("validate repository source offer: %v", err)
	}
	lock.GeoIP.SourceArtifact.SHA256 = strings.Repeat("f", 64)
	if err := validateSourceOffer(lock, sourceOffer); err == nil {
		t.Fatal("source offer validation accepted a different source archive digest")
	}
}

func TestAssetFetcherAlwaysVerifiesCache(t *testing.T) {
	cache := t.TempDir()
	contents := []byte("expected")
	artifact := artifactLock{SHA256: digestBytes(contents), Size: int64(len(contents))}
	cachePath := filepath.Join(cache, artifact.SHA256)
	if err := os.WriteFile(cachePath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	fetcher := newAssetFetcher(cache, true)
	if _, err := fetcher.fetch(context.Background(), "fixture", artifact); err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("fetch error = %v, want cached verification failure", err)
	}
}

func TestAssetFetcherRejectsCacheSymlink(t *testing.T) {
	cache := t.TempDir()
	contents := []byte("expected")
	artifact := artifactLock{SHA256: digestBytes(contents), Size: int64(len(contents))}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(cache, artifact.SHA256)); err != nil {
		t.Fatal(err)
	}
	fetcher := newAssetFetcher(cache, true)
	if _, err := fetcher.fetch(context.Background(), "fixture", artifact); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("fetch error = %v, want symlink rejection", err)
	}
}
