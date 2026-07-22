package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/asn"
	"github.com/luxiaba/remnanode-lite/internal/config"
	"github.com/luxiaba/remnanode-lite/internal/executil"
	"github.com/luxiaba/remnanode-lite/internal/netadmin"
	"github.com/luxiaba/remnanode-lite/internal/secret"
	"github.com/luxiaba/remnanode-lite/internal/version"
)

const (
	defaultSystemdUnitPath = "/usr/local/lib/systemd/system/remnanode-lite.service"
	defaultOpenRCUnitPath  = "/etc/init.d/remnanode-lite"
)

type result struct {
	level   string
	title   string
	detail  string
	fixHint string
}

// Run performs deployment health checks and returns exit code 0 (ok) or 1 (errors).
func Run(args []string) int {
	envPath := config.DefaultEnvPath
	for i := 0; i < len(args); i++ {
		if args[i] == "--env" && i+1 < len(args) {
			envPath = args[i+1]
			i++
		}
	}
	if override := strings.TrimSpace(os.Getenv("REMNANODE_ENV")); override != "" {
		envPath = override
	}

	fmt.Println(version.String())
	fmt.Println("-- Deployment diagnostics --")

	var results []result

	results = append(results, checkServiceDefinition())
	results = append(results, checkCapNetAdmin())

	cfg, cfgErr := loadConfig(envPath)
	if cfgErr != nil {
		results = append(results, result{
			level:   "ERROR",
			title:   "Configuration file",
			detail:  cfgErr.Error(),
			fixHint: "Create " + envPath + " or specify --env PATH",
		})
	} else {
		results = append(results, checkSecret(cfg)...)
		results = append(results, checkXrayBinary(cfg.XrayBin)...)
		results = append(results, checkGeoFiles(cfg.GeoDir)...)
		results = append(results, checkASNDatabase(cfg.ASNDBPath)...)
		results = append(results, checkCommand("nft", "nftables CLI (plugin IP blocking)")...)
		results = append(results, checkCommand("ss", "ss command (deployed listening-port ownership checks)")...)
	}

	exitCode := 0
	for _, item := range results {
		fmt.Printf("[%s] %s", item.level, item.title)
		if item.detail != "" {
			fmt.Printf(" - %s", item.detail)
		}
		fmt.Println()
		if item.fixHint != "" {
			fmt.Printf("      -> %s\n", item.fixHint)
		}
		if item.level == "ERROR" {
			exitCode = 1
		}
	}

	if exitCode == 0 {
		fmt.Println("-- Result: core checks passed (WARN items do not prevent basic Panel connectivity) --")
	} else {
		fmt.Println("-- Result: ERROR items found; resolve them before connecting to the Panel --")
	}
	return exitCode
}

func loadConfig(envPath string) (config.Config, error) {
	if _, err := os.Stat(envPath); err != nil {
		if envPath != ".env" {
			if _, err2 := os.Stat(".env"); err2 == nil {
				return config.Load(".env")
			}
		}
		return config.Config{}, fmt.Errorf("%s not found", envPath)
	}
	return config.Load(envPath)
}

func checkCapNetAdmin() result {
	if netadmin.HasCapNetAdmin() {
		return result{level: "OK", title: "CAP_NET_ADMIN", detail: "available to the current process"}
	}
	return result{
		level:   "WARN",
		title:   "CAP_NET_ADMIN",
		detail:  "not available to the current process (nftables and NETLINK_SOCK_DIAG socket destruction are unavailable)",
		fixHint: "Run diagnostics in the service context, or repair and restart remnanode-lite",
	}
}

func checkServiceDefinition() result {
	return checkServiceDefinitionAt(defaultSystemdUnitPath, defaultOpenRCUnitPath)
}

func checkServiceDefinitionAt(systemdPath, openRCPath string) result {
	if data, err := os.ReadFile(systemdPath); err == nil {
		content := string(data)
		if strings.Contains(content, "AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE") &&
			strings.Contains(content, "CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE") {
			return result{level: "OK", title: "systemd unit", detail: "the required NET_ADMIN and NET_BIND_SERVICE capabilities are configured"}
		}
		return result{
			level:   "WARN",
			title:   "systemd unit",
			detail:  "the required NET_ADMIN or NET_BIND_SERVICE capability is missing",
			fixHint: "Run sudo rnlctl repair, then restart remnanode-lite.service",
		}
	}

	if data, err := os.ReadFile(openRCPath); err == nil {
		content := string(data)
		if strings.Contains(content, "cap_net_admin") &&
			strings.Contains(content, "cap_net_bind_service") &&
			strings.Contains(content, "no_new_privs=yes") {
			return result{level: "OK", title: "OpenRC service", detail: "the required NET_ADMIN and NET_BIND_SERVICE capabilities and no_new_privs are configured"}
		}
		return result{
			level:   "WARN",
			title:   "OpenRC service",
			detail:  "the required NET_ADMIN or NET_BIND_SERVICE capability or no_new_privs is missing",
			fixHint: "Run rnlctl repair, then restart the remnanode-lite service",
		}
	}

	return result{
		level:   "WARN",
		title:   "service definition",
		detail:  systemdPath + " and " + openRCPath + " were not found",
		fixHint: "Install or repair the Native deployment with the matching release bundle",
	}
}

func checkSecret(cfg config.Config) []result {
	if _, err := secret.Parse(cfg.SecretKey); err == nil {
		return []result{{level: "OK", title: "Secret Key", detail: "configured and valid"}}
	}
	detail := "invalid format (the key supplied by the Panel cannot be parsed or required fields are missing)"
	if strings.TrimSpace(cfg.SecretKey) == "" {
		detail = "not configured (SECRET_KEY and SECRET_KEY_FILE are empty)"
	}
	return []result{{
		level:   "ERROR",
		title:   "Secret Key",
		detail:  detail,
		fixHint: "Write the complete key supplied by the Panel to " + config.DefaultSecretPath + ", then restart remnanode-lite",
	}}
}

func checkXrayBinary(bin string) []result {
	if bin == "" {
		bin = config.DefaultXrayBinPath
	}
	info, err := os.Stat(bin)
	if err != nil {
		return []result{{
			level:   "ERROR",
			title:   "rw-core",
			detail:  bin + " does not exist",
			fixHint: "Run rnlctl repair to restore the core from the installed release bundle",
		}}
	}
	if info.Mode()&0o111 == 0 {
		return []result{{
			level:   "ERROR",
			title:   "rw-core",
			detail:  bin + " is not executable",
			fixHint: "sudo chmod +x " + bin,
		}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command, err := executil.Run(ctx, nil, 4<<10, bin, "version")
	if err != nil {
		return []result{{
			level:  "WARN",
			title:  "rw-core",
			detail: bin + " exists, but the version command failed",
		}}
	}
	line := strings.TrimSpace(strings.Split(string(command.Stdout), "\n")[0])
	return []result{{level: "OK", title: "rw-core", detail: line}}
}

func checkGeoFiles(dir string) []result {
	if dir == "" {
		dir = config.DefaultGeoDir
	}
	var missing []string
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		detail := dir + " contains geoip.dat and geosite.dat"
		var extras []string
		for _, name := range []string{"geo-zapret.dat", "ip-zapret.dat"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				extras = append(extras, name)
			}
		}
		if len(extras) > 0 {
			detail += "; optional: " + strings.Join(extras, ", ")
		}
		return []result{{level: "OK", title: "Geo data", detail: detail}}
	}
	return []result{{
		level:   "WARN",
		title:   "Geo data",
		detail:  "missing " + strings.Join(missing, ", "),
		fixHint: "Run rnlctl repair to restore the data files from the installed release bundle",
	}}
}

func checkASNDatabase(path string) []result {
	if path == "" {
		path = config.DefaultASNDBPath
	}
	database, err := asn.Open(path)
	if err != nil {
		return []result{{
			level:   "WARN",
			title:   "ASN database",
			detail:  fmt.Sprintf("%s cannot be loaded by the runtime: %v (the plugin asList shared list falls back to empty)", path, err),
			fixHint: "Run rnlctl repair to restore the database from the installed release bundle",
		}}
	}
	available := database.Available()
	if err := database.Close(); err != nil {
		return []result{{
			level:  "WARN",
			title:  "ASN database",
			detail: fmt.Sprintf("%s cannot be closed cleanly: %v (the plugin asList shared list may be unavailable)", path, err),
		}}
	}
	if !available {
		return []result{{
			level:   "WARN",
			title:   "ASN database",
			detail:  path + " contains no ASN entries (the plugin asList shared list falls back to empty)",
			fixHint: "Run rnlctl repair to restore the database from the installed release bundle",
		}}
	}
	return []result{{level: "OK", title: "ASN database", detail: path + " passed the runtime open check"}}
}

func checkCommand(name, purpose string) []result {
	if path, err := exec.LookPath(name); err == nil {
		return []result{{level: "OK", title: name, detail: path + " (" + purpose + ")"}}
	}
	packages := "Rocky: dnf install iproute; Debian: apt install iproute2"
	if name == "nft" {
		packages = "Rocky: dnf install nftables; Debian: apt install nftables"
	}
	return []result{{
		level:   "WARN",
		title:   name,
		detail:  "not installed (" + purpose + ")",
		fixHint: packages,
	}}
}
