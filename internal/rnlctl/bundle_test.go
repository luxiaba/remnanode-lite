package rnlctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenBundleRejectsSymlinkedRootAndPayload(t *testing.T) {
	root := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle"), "2.8.0-rnl.1")
	rootLink := filepath.Join(t.TempDir(), "bundle-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := openBundle(BundleInput{Root: rootLink}, "amd64"); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("openBundle(root symlink) error = %v", err)
	}

	payload := filepath.Join(root, "share", "xray", "geoip.dat")
	target := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(target, []byte("geoip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := openBundle(BundleInput{Root: root}, "amd64"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("openBundle(payload symlink) error = %v", err)
	}
}

func TestValidatedBundleCannotBeChangedBeforeGenerationCopy(t *testing.T) {
	root := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle"), "2.8.0-rnl.1")
	bundle, err := openBundle(BundleInput{Root: root}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()

	payload := filepath.Join(root, "bin", "remnanode-lite")
	original, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), original...)
	tampered[0] ^= 0x01
	if err := os.WriteFile(payload, tampered, 0o755); err != nil {
		t.Fatal(err)
	}

	cache, _, cacheErr := cacheBundle(bundle, filepath.Join(t.TempDir(), "cache"))
	if cacheErr != nil {
		return
	}
	cached, err := openBundle(BundleInput{Archive: cache.Path, SHA256: cache.SHA256}, "amd64")
	if err != nil {
		t.Fatalf("cacheBundle accepted a changed source but produced an invalid snapshot: %v", err)
	}
	defer cached.Close()
	if cached.Identity != bundle.Identity {
		t.Fatalf("cached identity = %s, want validated identity %s", cached.Identity, bundle.Identity)
	}

	generations := filepath.Join(t.TempDir(), "generations")
	generationRoot, _, copyErr := copyBundleToGeneration(bundle, generations)
	if copyErr != nil {
		return
	}
	installed, err := validateBundleRoot(generationRoot, "amd64")
	if err != nil {
		t.Fatalf("generation copy accepted changed source but produced invalid payload: %v", err)
	}
	if installed.Identity != bundle.Identity {
		t.Fatalf("generation identity = %s, want %s", installed.Identity, bundle.Identity)
	}
}

func TestCachedBundleSourceCannotChangeBeforeGenerationCopy(t *testing.T) {
	root := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle"), "2.8.0-rnl.1")
	bundle, err := openBundle(BundleInput{Root: root}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()

	cache, _, err := cacheBundle(bundle, filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	cached, err := openBundle(BundleInput{Archive: cache.Path, SHA256: cache.SHA256}, "amd64")
	if err != nil {
		t.Fatalf("verify cache before source mutation: %v", err)
	}
	cached.Close()

	payload := filepath.Join(root, "bin", "rnlctl")
	original, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), original...)
	tampered[len(tampered)-2] ^= 0x01
	if err := os.WriteFile(payload, tampered, 0o755); err != nil {
		t.Fatal(err)
	}

	generationRoot, _, copyErr := copyBundleToGeneration(bundle, filepath.Join(t.TempDir(), "generations"))
	if copyErr != nil {
		return
	}
	installed, err := validateBundleRoot(generationRoot, "amd64")
	if err != nil {
		t.Fatalf("generation copy accepted a source changed after caching: %v", err)
	}
	if installed.Identity != bundle.Identity {
		t.Fatalf("generation identity = %s, want %s", installed.Identity, bundle.Identity)
	}
}

func TestOpenBundleRejectsExpectedVersionMismatch(t *testing.T) {
	root := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle"), "2.8.0-rnl.1")
	bundle, err := openBundle(BundleInput{Root: root, ExpectedVersion: "2.8.0-rnl.2"}, "amd64")
	if bundle != nil {
		bundle.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "does not match expected version") {
		t.Fatalf("openBundle() error = %v", err)
	}
}

func TestOpenBundleEnforcesStableContractPair(t *testing.T) {
	stableMismatch := writeTestBundle(t, filepath.Join(t.TempDir(), "stable-mismatch"), "2.8.1")
	if _, err := openBundle(BundleInput{Root: stableMismatch}, "amd64"); err == nil || !strings.Contains(err.Error(), "must equal contract version") {
		t.Fatalf("openBundle(stable mismatch) error = %v", err)
	}
	previewOlderContract := writeTestBundle(t, filepath.Join(t.TempDir(), "preview-older-contract"), "2.8.1-rnl.1")
	bundle, err := openBundle(BundleInput{Root: previewOlderContract}, "amd64")
	if err != nil {
		t.Fatalf("openBundle(preview with older contract) error = %v", err)
	}
	bundle.Close()
}

func TestOpenBundleRejectsNonRegularManifest(t *testing.T) {
	root := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle"), "2.8.0-rnl.1")
	manifest := filepath.Join(root, "release-manifest.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifest, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := openBundle(BundleInput{Root: root}, "amd64")
	if err == nil || !strings.Contains(err.Error(), "regular non-symlink file") {
		t.Fatalf("openBundle(non-regular manifest) error = %v", err)
	}
}
