package version

import (
	"os"
	"strings"
	"testing"
)

func TestContractVersionMatchesBaselineFile(t *testing.T) {
	raw, err := os.ReadFile("contract.version")
	if err != nil {
		t.Fatalf("read contract.version: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if ContractVersion != want {
		t.Fatalf("ContractVersion = %q, contract.version = %q", ContractVersion, want)
	}
}

func TestResolveContractVersionConfigured(t *testing.T) {
	if got := ResolveContractVersion(" 3.0.0 "); got != "3.0.0" {
		t.Fatalf("ResolveContractVersion() = %q, want 3.0.0", got)
	}
}

func TestResolveContractVersionDefault(t *testing.T) {
	if got := ResolveContractVersion(" "); got != ContractVersion {
		t.Fatalf("ResolveContractVersion() = %q, want default %q", got, ContractVersion)
	}
}

func TestStringIncludesBothVersions(t *testing.T) {
	t.Setenv("NODE_CONTRACT_VERSION", "9.9.9")
	got := String()
	if got == "" {
		t.Fatal("String() returned empty")
	}
	if !strings.Contains(got, Version) {
		t.Fatalf("String() missing lite version: %q", got)
	}
	if !strings.Contains(got, ContractVersion) {
		t.Fatalf("String() missing contract version: %q", got)
	}
	if strings.Contains(got, "9.9.9") {
		t.Fatalf("String() read mutable runtime environment: %q", got)
	}
}

func TestReleaseAssetURLValidatesInputs(t *testing.T) {
	got, err := ReleaseAssetURL("v2.8.0-rnl.1", "arm64")
	if err != nil || !strings.HasSuffix(got, "/v2.8.0-rnl.1/remnanode-lite_linux_arm64.tar.gz") {
		t.Fatalf("ReleaseAssetURL() = %q, %v", got, err)
	}
	for _, test := range []struct {
		tag  string
		arch string
	}{
		{tag: "../main", arch: "amd64"},
		{tag: "v2.8.0-rnl.0", arch: "amd64"},
		{tag: "v2.8.0", arch: "../amd64"},
		{tag: "v2.8.0", arch: "386"},
	} {
		if _, err := ReleaseAssetURL(test.tag, test.arch); err == nil {
			t.Fatalf("ReleaseAssetURL(%q, %q) succeeded", test.tag, test.arch)
		}
	}
}

func TestInstallScriptURLValidatesInputs(t *testing.T) {
	got, err := InstallScriptURL("v2.8.0", "install-node.sh")
	if err != nil || !strings.HasSuffix(got, "/v2.8.0/scripts/install-node.sh") {
		t.Fatalf("InstallScriptURL() = %q, %v", got, err)
	}
	for _, test := range []struct {
		tag    string
		script string
	}{
		{tag: "main", script: "install-node.sh"},
		{tag: "v2.8.0", script: "../install-node.sh"},
		{tag: "v2.8.0", script: "arbitrary.sh"},
	} {
		if _, err := InstallScriptURL(test.tag, test.script); err == nil {
			t.Fatalf("InstallScriptURL(%q, %q) succeeded", test.tag, test.script)
		}
	}
}
