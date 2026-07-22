package version

import (
	"fmt"
	"regexp"
	"strings"
)

// Version is the release version (overridable via -ldflags at build time).
var Version = "2.8.0"

// ContractVersion is the upstream @remnawave/node version reported to Panel as nodeVersion.
// Default must stay in sync with contract.version and contract-sync CI.
// Overridable via -ldflags at build time.
var ContractVersion = "2.8.0"

const releaseRepo = "luxiaba/remnanode-lite"

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-rnl\.[1-9][0-9]*)?$`)

// ResolveContractVersion returns the immutable nodeVersion for one daemon
// instance. The composition root resolves configuration once at startup.
func ResolveContractVersion(configured string) string {
	if version := strings.TrimSpace(configured); version != "" {
		return version
	}
	return ContractVersion
}

func String() string {
	return fmt.Sprintf("remnanode-lite %s (contract %s)", Version, ContractVersion)
}

func ReleaseAssetURL(tag, arch string) (string, error) {
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("invalid release tag %q", tag)
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported release architecture %q", arch)
	}
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/remnanode-lite_%s_linux_%s.tar.gz",
		releaseRepo, tag, strings.TrimPrefix(tag, "v"), arch,
	), nil
}

func InstallScriptURL(tag, script string) (string, error) {
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("invalid release tag %q", tag)
	}
	if script != "install.sh" {
		return "", fmt.Errorf("unsupported install script %q", script)
	}
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/%s",
		releaseRepo,
		tag,
		script,
	), nil
}
