package version

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Version is the release version (overridable via -ldflags at build time).
var Version = "2.8.0-rnl.1"

// ContractVersion is the upstream @remnawave/node version reported to Panel as nodeVersion.
// Default must stay in sync with contract.version and contract-sync CI.
// Overridable via -ldflags at build time.
var ContractVersion = "2.8.0"

const releaseRepo = "Luxiaba/remnanode-lite"

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-rnl\.[1-9][0-9]*)?$`)

// ReportedNodeVersion returns the nodeVersion sent to Panel.
// Priority: NODE_CONTRACT_VERSION env > ContractVersion (build-time default).
// Mirrors upstream reading package.json version at bootstrap.
func ReportedNodeVersion() string {
	if v := strings.TrimSpace(os.Getenv("NODE_CONTRACT_VERSION")); v != "" {
		return v
	}
	return ContractVersion
}

func String() string {
	return fmt.Sprintf("remnanode-lite %s (contract %s)", Version, ReportedNodeVersion())
}

func ReleaseAssetURL(tag, arch string) (string, error) {
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("invalid release tag %q", tag)
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported release architecture %q", arch)
	}
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/remnanode-lite_linux_%s.tar.gz",
		releaseRepo,
		tag,
		arch,
	), nil
}

func InstallScriptURL(tag, script string) (string, error) {
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("invalid release tag %q", tag)
	}
	switch script {
	case "install-node.sh", "install-node-alpine.sh", "install-xray.sh", "upgrade.sh", "uninstall.sh":
	default:
		return "", fmt.Errorf("unsupported install script %q", script)
	}
	return fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/scripts/%s",
		releaseRepo,
		tag,
		script,
	), nil
}
