package rnlctl

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const maxFuzzManifestBytes = 256 << 10

func FuzzParseEnvironmentAssignments(f *testing.F) {
	for _, data := range []string{
		"NODE_PORT=2222\nLOW_MEMORY=1\n",
		"export NODE_PORT=\"2222\"\n# comment\n",
		"NODE_PORT=2222\nNODE_PORT=3333\n",
		"EMPTY=\nVALUE=a=b=c\n",
		"=missing-key\n",
		"not-an-assignment\n",
	} {
		f.Add([]byte(data))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxEnvironmentBytes+1 {
			t.Skip()
		}
		first, firstErr := parseEnvironmentAssignments(data)
		second, secondErr := parseEnvironmentAssignments(data)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parseEnvironmentAssignments changed error outcome: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !maps.Equal(first, second) {
			t.Fatalf("parseEnvironmentAssignments is not deterministic: first=%#v second=%#v", first, second)
		}
		for key := range first {
			if key == "" || key != strings.TrimSpace(key) {
				t.Fatalf("parseEnvironmentAssignments accepted a non-canonical key %q", key)
			}
		}
	})
}

func FuzzValidateBundleRelativePath(f *testing.F) {
	for _, value := range []string{
		"bin/remnanode-lite",
		"release-manifest.json",
		"../outside",
		"/absolute",
		`..\outside`,
		"nested//file",
		"nested/./file",
		"nested/\xe6\x96\x87\xe4\xbb\xb6",
		"",
	} {
		f.Add(value)
	}

	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 1_024 {
			t.Skip()
		}
		if err := validateBundleRelativePath(value); err != nil {
			return
		}
		root := filepath.Join(string(filepath.Separator), "bundle-root")
		target := filepath.Join(root, filepath.FromSlash(value))
		relative, err := filepath.Rel(root, target)
		if err != nil {
			t.Fatal(err)
		}
		if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("accepted path %q escapes bundle root as %q", value, target)
		}
		if filepath.ToSlash(relative) != value {
			t.Fatalf("accepted path %q is not canonical; relative path is %q", value, relative)
		}
	})
}

func FuzzDecodeReleaseManifest(f *testing.F) {
	digest := strings.Repeat("a", 64)
	revision := strings.Repeat("b", 40)
	artifact := manifestArtifact{URL: "https://example.invalid/artifact", SHA256: digest, Size: 1}
	payload := manifestRuntimePayload{SHA256: digest, Size: 1, License: "MIT"}
	validManifest := releaseManifest{
		SchemaVersion: manifestSchema, Name: bundleTopDirectory, Version: "2.8.0-rnl.1",
		ContractVersion: "2.8.0", OS: "linux", Architecture: "amd64",
		SourceRevision: revision, SourceDateEpoch: 1_700_000_000,
		RuntimeAssetLockSHA256: digest,
		RuntimeAssets: manifestRuntimeAssets{
			Xray:    manifestXrayRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/xray", Archive: artifact, Core: payload},
			GeoIP:   manifestGeoRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/geoip", SourceArtifact: artifact, Artifact: artifact, License: "MIT"},
			GeoSite: manifestGeoRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/geosite", SourceArtifact: artifact, Artifact: artifact, License: "MIT"},
			ASN:     manifestASNRuntime{Commit: revision, Source: artifact, Output: payload},
		},
	}
	if err := validateManifestIdentity(validManifest, "amd64"); err != nil {
		f.Fatalf("valid fuzz seed failed identity validation: %v", err)
	}
	valid, err := json.Marshal(validManifest)
	if err != nil {
		f.Fatal(err)
	}
	for _, data := range [][]byte{
		valid,
		[]byte(`{}`),
		[]byte(`{"schemaVersion":1,"schemaVersion":2}`),
		[]byte(`{"unknown":true}`),
		[]byte(`{"schemaVersion":1} {}`),
		[]byte(`{"files":[{"path":"../outside"}]}`),
		[]byte(`{"truncated":`),
	} {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzManifestBytes {
			t.Skip()
		}
		var first releaseManifest
		firstErr := decodeStrictJSON(data, &first)
		var second releaseManifest
		secondErr := decodeStrictJSON(data, &second)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("decodeStrictJSON changed error outcome: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("decodeStrictJSON is not deterministic: first=%#v second=%#v", first, second)
		}
		_ = validateManifestIdentity(first, "amd64")
		for _, file := range first.Files {
			_ = validateBundleRelativePath(file.Path)
		}
	})
}
