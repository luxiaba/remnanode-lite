package main

import (
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
	if strings.Join(names, "\n") != strings.Join(releaseAssetNames("2.8.0"), "\n") {
		t.Fatalf("assembled names = %v, want %v", names, releaseAssetNames("2.8.0"))
	}
	singleFile, err := os.ReadFile(filepath.Join(output, "docker-compose.single-file.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(singleFile), ":latest") ||
		!strings.Contains(string(singleFile), "ghcr.io/luxiaba/remnanode-lite:2.8.0") {
		t.Fatalf("single-file Compose was not pinned: %s", singleFile)
	}
	wantChecksums, err := buildChecksumFile(output, releasePayloadNames("2.8.0"))
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

func TestVerifyReleaseSnapshot(t *testing.T) {
	root, native := releasePackageFixture(t)
	output := filepath.Join(root, "release-output")
	if err := assembleReleasePackage(assembleOptions{
		projectRoot: root, nativeDirectory: native, outputDirectory: output, version: "2.8.0",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := githubReleaseSnapshot{
		TagName: "v2.8.0", TargetCommitish: strings.Repeat("a", 40), Draft: true,
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
	if err := verifyReleaseSnapshot(snapshotPath, output, "v2.8.0", strings.Repeat("a", 40), true, false, false); err != nil {
		t.Fatalf("verifyReleaseSnapshot(): %v", err)
	}

	snapshot.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseSnapshot(snapshotPath, output, "v2.8.0", strings.Repeat("a", 40), true, false, false); err == nil {
		t.Fatal("verifyReleaseSnapshot() accepted a mismatched asset digest")
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
