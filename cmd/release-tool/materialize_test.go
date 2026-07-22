package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeLockedRuntimeAssets(t *testing.T) {
	fixture := newBuildFixture(t)
	output := filepath.Join(t.TempDir(), "assets")
	options := materializeOptions{
		lockPath: fixture.options.lockPath, architecture: "amd64",
		asnBuilderPath:  fixture.options.asnBuilderPath,
		cacheDirectory:  fixture.options.cacheDirectory,
		outputDirectory: output, offline: true,
	}
	if err := materializeRuntimeAssets(context.Background(), options); err != nil {
		t.Fatalf("materialize runtime assets: %v", err)
	}
	document, err := loadRuntimeLockDocument(options.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]struct {
		digest string
		size   int64
		mode   os.FileMode
	}{
		"lib/rw-core":                {document.Lock.Xray.Architectures.AMD64.Core.SHA256, document.Lock.Xray.Architectures.AMD64.Core.Size, 0o755},
		"runtime-assets.lock.json":   {document.SHA256, int64(len(document.Data)), 0o644},
		"share/asn/asn-prefixes.bin": {document.Lock.ASN.Output.SHA256, document.Lock.ASN.Output.Size, 0o644},
		"share/xray/geoip.dat":       {document.Lock.GeoIP.Artifact.SHA256, document.Lock.GeoIP.Artifact.Size, 0o644},
		"share/xray/geosite.dat":     {document.Lock.GeoSite.Artifact.SHA256, document.Lock.GeoSite.Artifact.Size, 0o644},
	}
	for identifier, artifact := range document.Lock.licenseArtifacts() {
		wanted["licenses/"+identifier+".txt"] = struct {
			digest string
			size   int64
			mode   os.FileMode
		}{artifact.SHA256, artifact.Size, 0o644}
	}
	for relative, expected := range wanted {
		path := filepath.Join(output, filepath.FromSlash(relative))
		if err := verifyFileIdentity(path, expected.digest, expected.size); err != nil {
			t.Errorf("verify %s: %v", relative, err)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != expected.mode {
			t.Errorf("%s mode = %04o, want %04o", relative, info.Mode().Perm(), expected.mode)
		}
	}
	if err := materializeRuntimeAssets(context.Background(), options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second materialize error = %v, want existing output rejection", err)
	}
}

func TestBuildBundleRejectsWrongProjectBinaryArchitecture(t *testing.T) {
	fixture := newBuildFixture(t)
	wrong := filepath.Join(t.TempDir(), "arm64-node")
	writeFixtureFile(t, wrong, fixtureGoBinaries(t)["remnanode-lite-arm64"], 0o755)
	options := fixture.options
	options.nodePath = wrong
	options.outputPath = filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := buildBundle(context.Background(), options); err == nil || !strings.Contains(err.Error(), "want linux/amd64") {
		t.Fatalf("build error = %v, want ELF architecture rejection", err)
	}
}
