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

const defaultEnvPath = "/etc/remnanode/node.env"
const defaultUnitPath = "/etc/systemd/system/remnawave-node.service"

type result struct {
	level   string
	title   string
	detail  string
	fixHint string
}

// Run performs deployment health checks and returns exit code 0 (ok) or 1 (errors).
func Run(args []string) int {
	envPath := defaultEnvPath
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

	results = append(results, checkSystemdCapNetAdmin())
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
		fixHint: "Start with systemd: confirm that the unit contains AmbientCapabilities=CAP_NET_ADMIN, then run systemctl daemon-reload && systemctl restart remnawave-node",
	}
}

func checkSystemdCapNetAdmin() result {
	data, err := os.ReadFile(defaultUnitPath)
	if err != nil {
		return result{
			level:   "WARN",
			title:   "systemd unit",
			detail:  defaultUnitPath + " not found",
			fixHint: "Run install-node.sh or upgrade.sh to install the provided unit",
		}
	}
	content := string(data)
	if strings.Contains(content, "AmbientCapabilities=CAP_NET_ADMIN") {
		return result{level: "OK", title: "systemd unit", detail: "AmbientCapabilities=CAP_NET_ADMIN is configured"}
	}
	return result{
		level:   "WARN",
		title:   "systemd unit",
		detail:  "AmbientCapabilities=CAP_NET_ADMIN is missing",
		fixHint: "sudo curl -fsSL https://raw.githubusercontent.com/luxiaba/remnanode-lite/v" + version.Version + "/deploy/remnawave-node.service -o " + defaultUnitPath + " && sudo systemctl daemon-reload && sudo systemctl restart remnawave-node",
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
		fixHint: "Write the complete key supplied by the Panel to /etc/remnanode/secret.key, then restart the remnawave-node service",
	}}
}

func checkXrayBinary(bin string) []result {
	if bin == "" {
		bin = "/usr/local/lib/remnanode/rw-core"
	}
	info, err := os.Stat(bin)
	if err != nil {
		return []result{{
			level:   "ERROR",
			title:   "rw-core",
			detail:  bin + " does not exist",
			fixHint: "Run scripts/install-xray.sh or install-node.sh without --skip-xray",
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
		dir = "/usr/local/share/remnanode/xray"
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
		fixHint: "Run install-xray.sh again or copy the files from an Xray release to " + dir,
	}}
}

func checkASNDatabase(path string) []result {
	if path == "" {
		path = "/usr/local/share/remnanode/asn/asn-prefixes.bin"
	}
	database, err := asn.Open(path)
	if err != nil {
		return []result{{
			level:   "WARN",
			title:   "ASN database",
			detail:  fmt.Sprintf("%s cannot be loaded by the runtime: %v (the plugin asList shared list falls back to empty)", path, err),
			fixHint: "Set ASN_DB_URL and rerun install-xray.sh, or generate the database with cmd/asn-builder and place it at this path",
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
			fixHint: "Set ASN_DB_URL and rerun install-xray.sh, or generate a replacement with cmd/asn-builder",
		}}
	}
	return []result{{level: "OK", title: "ASN database", detail: path + " passed the runtime open check"}}
}

func checkCommand(name, purpose string) []result {
	if path, err := exec.LookPath(name); err == nil {
		return []result{{level: "OK", title: name, detail: path + " (" + purpose + ")"}}
	}
	return []result{{
		level:   "WARN",
		title:   name,
		detail:  "not installed (" + purpose + ")",
		fixHint: "Debian/Ubuntu: apt install iproute2 " + name,
	}}
}
