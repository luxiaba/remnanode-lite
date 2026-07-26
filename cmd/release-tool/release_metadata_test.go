package main

import (
	"fmt"
	"strings"
	"testing"

	projectversion "github.com/luxiaba/remnanode-lite/internal/version"
)

func TestClassifyRelease(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		contract   string
		channel    string
		prerelease bool
		latest     bool
		wantErr    bool
	}{
		{name: "stable", version: "2.8.0", contract: "2.8.0", channel: "latest", latest: true},
		{name: "preview", version: "2.8.1-rnl.9", contract: "2.8.0", channel: "preview", prerelease: true},
		{name: "stable mismatch", version: "2.8.1", contract: "2.8.0", wantErr: true},
		{name: "invalid preview", version: "2.8.0-rnl.0", contract: "2.8.0", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata, err := classifyRelease(test.version, test.contract)
			if test.wantErr {
				if err == nil {
					t.Fatalf("classifyRelease(%q, %q) unexpectedly succeeded", test.version, test.contract)
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyRelease(%q, %q): %v", test.version, test.contract, err)
			}
			if metadata.Tag != test.version || metadata.Channel != test.channel ||
				metadata.Prerelease != test.prerelease || metadata.MakeLatest != test.latest {
				t.Fatalf("unexpected metadata: %+v", metadata)
			}
		})
	}
}

func TestRunMetadata(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := runMetadata([]string{"--version", projectversion.Version}, &stdout, &stderr); err != nil {
		t.Fatalf("runMetadata(): %v; stderr=%s", err, stderr.String())
	}
	metadata, err := classifyRelease(projectversion.Version, projectversion.ContractVersion)
	if err != nil {
		t.Fatalf("classify source release: %v", err)
	}
	want := fmt.Sprintf(
		"version=%s\ntag=%s\nchannel=%s\nprerelease=%t\nmake_latest=%t\n",
		metadata.Version, metadata.Tag, metadata.Channel, metadata.Prerelease, metadata.MakeLatest,
	)
	if stdout.String() != want {
		t.Fatalf("runMetadata() output = %q, want %q", stdout.String(), want)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runMetadata([]string{"--version", projectversion.Version + "-mismatch"}, &stdout, &stderr); err == nil {
		t.Fatal("runMetadata() accepted a version that differs from source")
	}

	stdout.Reset()
	stderr.Reset()
	if err := runMetadata([]string{"--tag", "2.8.1-rnl.9"}, &stdout, &stderr); err != nil {
		t.Fatalf("runMetadata(--tag): %v", err)
	}
	if !strings.Contains(stdout.String(), "channel=preview\n") {
		t.Fatalf("runMetadata(--tag) output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := runMetadata([]string{"--tag", "v2.8.1-rnl.9"}, &stdout, &stderr); err == nil {
		t.Fatal("runMetadata(--tag) accepted a prefixed release tag")
	}
}
