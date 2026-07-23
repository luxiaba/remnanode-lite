package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssembleReleasePackage(t *testing.T) {
	root, native := releasePackageFixture(t)
	output := filepath.Join(root, "dist", "release")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: native, outputDirectory: output, version: "2.8.0",
	}); err != nil {
		t.Fatalf("assembleReleasePackage(): %v", err)
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, "\n") != strings.Join(baseReleaseAssetNames("2.8.0"), "\n") {
		t.Fatalf("assembled names = %v, want %v", names, baseReleaseAssetNames("2.8.0"))
	}
	singleFile, err := os.ReadFile(filepath.Join(output, "docker-compose.single-file.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(singleFile), ":latest") ||
		!strings.Contains(string(singleFile), "ghcr.io/luxiaba/remnanode-lite:2.8.0") {
		t.Fatalf("single-file Compose was not pinned: %s", singleFile)
	}
	wantChecksums, err := buildChecksumFile(output, baseReleasePayloadNames("2.8.0"))
	if err != nil {
		t.Fatal(err)
	}
	gotChecksums, err := os.ReadFile(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotChecksums) != string(wantChecksums) {
		t.Fatalf("SHA256SUMS = %q, want %q", gotChecksums, wantChecksums)
	}
}

func TestFinalizeReleasePackageBindsVerifiedIndex(t *testing.T) {
	options := completeReleasePackageFixture(t)
	if err := finalizeReleasePackage(options); err != nil {
		t.Fatalf("finalizeReleasePackage(): %v", err)
	}
	index, err := readReleaseIndex(filepath.Join(options.directory, releaseIndexName))
	if err != nil {
		t.Fatal(err)
	}
	if index.SourceRevision != options.sourceRevision || index.Image != options.image ||
		index.IndexDigest != options.indexDigest {
		t.Fatalf("release index = %#v, does not match finalizer input", index)
	}
	if err := verifyReleasePackage(verifyPackageOptions{
		lockPath: options.lockPath, directory: options.directory, version: options.version,
		contractVersion: options.contractVersion, sourceRevision: options.sourceRevision,
		requireReleaseIndex: true,
	}); err != nil {
		t.Fatalf("verify finalized package: %v", err)
	}
}

func TestFinalizeReleasePackageRejectsExistingIndex(t *testing.T) {
	root, native := releasePackageFixture(t)
	directory := filepath.Join(root, "release-output")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: native, outputDirectory: directory, version: "2.8.0",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, releaseIndexName), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := finalizeReleasePackage(finalizeReleaseOptions{
		lockPath: "unused", directory: directory, version: "2.8.0", contractVersion: "2.8.0",
		sourceRevision: strings.Repeat("a", 40), image: "ghcr.io/luxiaba/remnanode-lite",
		indexDigest: "sha256:" + strings.Repeat("b", 64),
	})
	if err == nil || !strings.Contains(err.Error(), "release index already exists") {
		t.Fatalf("finalizeReleasePackage() error = %v, want existing index rejection", err)
	}
}

func TestRunVerifyReleaseIndexBindsExpectedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), releaseIndexName)
	index := releaseIndex{
		SchemaVersion: 1, Version: "2.8.0", SourceRevision: strings.Repeat("a", 40),
		Image: "ghcr.io/luxiaba/remnanode-lite", IndexDigest: "sha256:" + strings.Repeat("b", 64),
	}
	if err := writeReleaseIndex(path, index); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if err := runVerifyReleaseIndex([]string{
		"--file", path, "--tag", "2.8.0", "--image", index.Image,
		"--source-revision", index.SourceRevision, "--index-digest", index.IndexDigest,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runVerifyReleaseIndex(): %v", err)
	}
	if got, want := stdout.String(), "source_revision="+index.SourceRevision+"\nindex_digest="+index.IndexDigest+"\n"; got != want {
		t.Fatalf("verify-release-index output = %q, want %q", got, want)
	}
	if err := runVerifyReleaseIndex([]string{
		"--file", path, "--tag", "2.8.0", "--image", index.Image,
		"--index-digest", "sha256:" + strings.Repeat("c", 64),
	}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("runVerifyReleaseIndex() error = %v, want digest mismatch", err)
	}
}

func TestVerifyReleaseSnapshot(t *testing.T) {
	root, native := releasePackageFixture(t)
	output := filepath.Join(root, "release-output")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: native, outputDirectory: output, version: "2.8.0",
	}); err != nil {
		t.Fatal(err)
	}
	addReleaseIndexFixture(t, output)
	snapshot := githubReleaseSnapshot{
		TagName: "2.8.0", TargetCommitish: strings.Repeat("a", 40), Draft: true,
	}
	for _, name := range releaseAssetNames("2.8.0") {
		digest, size, err := fileDigestAndSize(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Assets = append(snapshot.Assets, struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
			State  string `json:"state"`
		}{Name: name, Digest: "sha256:" + digest, Size: size, State: "uploaded"})
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(root, "release.json")
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseSnapshot(snapshotPath, output, "2.8.0", strings.Repeat("a", 40), true, false, releaseImmutabilityFalse); err != nil {
		t.Fatalf("verifyReleaseSnapshot(): %v", err)
	}

	snapshot.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseSnapshot(snapshotPath, output, "2.8.0", strings.Repeat("a", 40), true, false, releaseImmutabilityFalse); err == nil {
		t.Fatal("verifyReleaseSnapshot() accepted a mismatched asset digest")
	}
}

func TestVerifyReleaseSnapshotAllowsPendingImmutability(t *testing.T) {
	root, native := releasePackageFixture(t)
	output := filepath.Join(root, "release-output")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: native, outputDirectory: output, version: "2.8.0",
	}); err != nil {
		t.Fatal(err)
	}
	addReleaseIndexFixture(t, output)
	snapshot := githubReleaseSnapshot{
		TagName: "2.8.0", TargetCommitish: strings.Repeat("a", 40), Immutable: false,
	}
	for _, name := range releaseAssetNames("2.8.0") {
		digest, size, err := fileDigestAndSize(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Assets = append(snapshot.Assets, struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
			State  string `json:"state"`
		}{Name: name, Digest: "sha256:" + digest, Size: size, State: "uploaded"})
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(root, "release.json")
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseSnapshot(snapshotPath, output, "2.8.0", strings.Repeat("a", 40), false, false, releaseImmutabilityAny); err != nil {
		t.Fatalf("pending immutable state was rejected during identity verification: %v", err)
	}
	if err := verifyReleaseSnapshot(snapshotPath, output, "2.8.0", strings.Repeat("a", 40), false, false, releaseImmutabilityTrue); err == nil {
		t.Fatal("pending immutable state satisfied the final immutable check")
	}
}

func releasePackageFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"release/native", "deploy", "native"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"release/native/install.sh":       "#!/bin/sh\nexit 0\n",
		"compose.yaml":                    "image: ghcr.io/luxiaba/remnanode-lite:2.8.0\n",
		".env.example":                    "REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:2.8.0\n",
		"deploy/compose.single-file.yaml": "image: ghcr.io/luxiaba/remnanode-lite:latest\n",
	}
	for name, contents := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, "install.sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
	}
	native := filepath.Join(root, "native")
	for _, architecture := range []string{"amd64", "arm64"} {
		name := fmt.Sprintf("remnanode-lite_2.8.0_linux_%s.tar.gz", architecture)
		if err := os.WriteFile(filepath.Join(native, name), []byte("fixture-"+architecture), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, native
}

func completeReleasePackageFixture(t *testing.T) finalizeReleaseOptions {
	t.Helper()
	fixture := newBuildFixture(t)
	options := fixture.options
	options.version = "2.8.0"
	options.contractVersion = "2.8.0"

	root := options.projectRoot
	for name, contents := range map[string]string{
		"release/native/install.sh":       "#!/bin/sh\nexit 0\n",
		"compose.yaml":                    "image: ghcr.io/luxiaba/remnanode-lite:2.8.0\n",
		".env.example":                    "REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:2.8.0\n",
		"deploy/compose.single-file.yaml": "image: ghcr.io/luxiaba/remnanode-lite:latest\n",
	} {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, "install.sh") {
			mode = 0o755
		}
		writeFixtureFile(t, filepath.Join(root, name), []byte(contents), mode)
	}

	armNode := filepath.Join(root, "remnanode-lite-arm64")
	armRNLCTL := filepath.Join(root, "rnlctl-arm64")
	binaries := fixtureGoBinaries(t)
	writeFixtureFile(t, armNode, binaries["remnanode-lite-arm64"], 0o755)
	writeFixtureFile(t, armRNLCTL, binaries["rnlctl-arm64"], 0o755)

	nativeDirectory := filepath.Join(root, "native")
	if err := os.MkdirAll(nativeDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		build := options
		build.architecture = architecture
		build.outputPath = filepath.Join(nativeDirectory,
			fmt.Sprintf("remnanode-lite_%s_linux_%s.tar.gz", build.version, architecture))
		if architecture == "arm64" {
			build.nodePath = armNode
			build.rnlctlPath = armRNLCTL
		}
		if err := buildBundle(context.Background(), build); err != nil {
			t.Fatalf("build %s Native bundle: %v", architecture, err)
		}
	}

	directory := filepath.Join(root, "dist", "release")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: nativeDirectory, outputDirectory: directory, version: options.version,
	}); err != nil {
		t.Fatalf("assemble verified release package: %v", err)
	}
	return finalizeReleaseOptions{
		lockPath: options.lockPath, directory: directory, version: options.version,
		contractVersion: options.contractVersion, sourceRevision: options.sourceRevision,
		image: "ghcr.io/luxiaba/remnanode-lite", indexDigest: "sha256:" + strings.Repeat("d", 64),
	}
}

func addReleaseIndexFixture(t *testing.T, directory string) {
	t.Helper()
	if err := writeReleaseIndex(filepath.Join(directory, releaseIndexName), releaseIndex{
		SchemaVersion: 1, Version: "2.8.0", SourceRevision: strings.Repeat("a", 40),
		Image: "ghcr.io/luxiaba/remnanode-lite", IndexDigest: "sha256:" + strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	checksums, err := buildChecksumFile(directory, releasePayloadNames("2.8.0"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), checksums, 0o644); err != nil {
		t.Fatal(err)
	}
}
