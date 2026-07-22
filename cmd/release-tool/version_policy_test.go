package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVersionPair(t *testing.T) {
	tests := []struct {
		name          string
		project       string
		contract      string
		wantErr       bool
		wantSubstring string
	}{
		{name: "stable alignment", project: "2.8.0", contract: "2.8.0"},
		{name: "preview may carry older contract", project: "2.8.1-rnl.1", contract: "2.8.0"},
		{name: "zero is a valid component", project: "0.0.0", contract: "0.0.0"},
		{name: "preview zero base", project: "0.0.0-rnl.1", contract: "0.0.0"},
		{name: "project leading zero", project: "02.8.0", contract: "2.8.0", wantErr: true, wantSubstring: "invalid project version"},
		{name: "minor leading zero", project: "2.08.0", contract: "2.8.0", wantErr: true, wantSubstring: "invalid project version"},
		{name: "patch leading zero", project: "2.8.00", contract: "2.8.0", wantErr: true, wantSubstring: "invalid project version"},
		{name: "preview zero revision", project: "2.8.0-rnl.0", contract: "2.8.0", wantErr: true, wantSubstring: "invalid project version"},
		{name: "preview revision leading zero", project: "2.8.0-rnl.01", contract: "2.8.0", wantErr: true, wantSubstring: "invalid project version"},
		{name: "contract leading zero", project: "2.8.1-rnl.1", contract: "02.8.0", wantErr: true, wantSubstring: "invalid contract version"},
		{name: "stable contract mismatch", project: "2.8.0", contract: "2.8.1", wantErr: true, wantSubstring: "must equal contract version"},
		{name: "stable contract prerelease", project: "2.8.0", contract: "2.8.0-rnl.1", wantErr: true, wantSubstring: "invalid contract version"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateVersionPair(test.project, test.contract)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), test.wantSubstring) {
					t.Fatalf("validateVersionPair(%q, %q) = %v, want error containing %q", test.project, test.contract, err, test.wantSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateVersionPair(%q, %q) = %v, want success", test.project, test.contract, err)
			}
		})
	}
}

func TestBuildOptionsValidateUsesVersionPolicy(t *testing.T) {
	base := buildOptions{
		lockPath:         "lock",
		architecture:     "amd64",
		version:          "2.8.0",
		contractVersion:  "2.8.0",
		sourceRevision:   strings.Repeat("a", 40),
		sourceDateEpoch:  1_700_000_000,
		projectRoot:      "root",
		nodePath:         "node",
		rnlctlPath:       "rnlctl",
		asnBuilderPath:   "asn-builder",
		installerPath:    "install.sh",
		supportDirectory: "support",
		cacheDirectory:   "cache",
		outputPath:       filepath.Join(t.TempDir(), "bundle.tar.gz"),
	}

	tests := []struct {
		name     string
		version  string
		contract string
		wantErr  bool
	}{
		{name: "stable mismatch", version: "2.8.0", contract: "2.8.1", wantErr: true},
		{name: "leading zero", version: "02.8.0", contract: "2.8.0", wantErr: true},
		{name: "preview mismatch allowed", version: "2.8.1-rnl.1", contract: "2.8.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := base
			options.version = test.version
			options.contractVersion = test.contract
			err := options.validate()
			if test.wantErr && err == nil {
				t.Fatalf("build options unexpectedly accepted %q/%q", test.version, test.contract)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("build options rejected %q/%q: %v", test.version, test.contract, err)
			}
		})
	}
}

func TestReleaseManifestValidationUsesVersionPolicy(t *testing.T) {
	base := releaseManifest{
		SchemaVersion:   manifestSchemaVersion,
		Name:            bundleName,
		OS:              bundleOS,
		Architecture:    "amd64",
		SourceRevision:  strings.Repeat("a", 40),
		SourceDateEpoch: 1,
	}

	tests := []struct {
		name          string
		version       string
		contract      string
		wantSubstring string
	}{
		{name: "stable mismatch", version: "2.8.0", contract: "2.8.1", wantSubstring: "must equal contract version"},
		{name: "leading zero", version: "02.8.0", contract: "2.8.0", wantSubstring: "invalid project version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			manifest.Version = test.version
			manifest.ContractVersion = test.contract
			err := validateReleaseManifest(manifest, verifyOptions{architecture: "amd64"}, runtimeLock{}, "", xrayArchitecture{})
			if err == nil || !strings.Contains(err.Error(), test.wantSubstring) {
				t.Fatalf("validateReleaseManifest() = %v, want error containing %q", err, test.wantSubstring)
			}
		})
	}
}
