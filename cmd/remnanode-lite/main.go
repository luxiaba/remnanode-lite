package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/asn"
	"github.com/luxiaba/remnanode-lite/internal/auth"
	"github.com/luxiaba/remnanode-lite/internal/bodylimit"
	"github.com/luxiaba/remnanode-lite/internal/config"
	"github.com/luxiaba/remnanode-lite/internal/connections"
	"github.com/luxiaba/remnanode-lite/internal/doctor"
	"github.com/luxiaba/remnanode-lite/internal/httpserver"
	"github.com/luxiaba/remnanode-lite/internal/netadmin"
	"github.com/luxiaba/remnanode-lite/internal/nodehandler"
	"github.com/luxiaba/remnanode-lite/internal/plugin"
	"github.com/luxiaba/remnanode-lite/internal/secret"
	"github.com/luxiaba/remnanode-lite/internal/stats"
	"github.com/luxiaba/remnanode-lite/internal/system"
	"github.com/luxiaba/remnanode-lite/internal/unixconfig"
	"github.com/luxiaba/remnanode-lite/internal/version"
	"github.com/luxiaba/remnanode-lite/internal/xray"
)

const nodeShutdownTimeout = 25 * time.Second
const internalHealthcheckTimeout = 2 * time.Second

const cliUsage = `usage: remnanode-lite [version|healthcheck|doctor|kill-sockets|validate-secret|canonicalize-secret|release-url|install-script-url]
  kill-sockets, --kill-sockets, -k  Kill connected sockets matching a local or remote IP`

type socketKiller func(context.Context, string) error

func main() {
	code := runCLI(
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		runNode,
		doctor.Run,
		netadmin.KillSocketsByIP,
	)
	if code != 0 {
		os.Exit(code)
	}
}

func runCLI(
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	runDaemon func() error,
	runDoctor func([]string) int,
	killSockets socketKiller,
) int {
	if len(args) == 0 {
		if err := runDaemon(); err != nil {
			fmt.Fprintf(stderr, "remnanode-lite stopped with error: %v\n", err)
			return 1
		}
		return 0
	}

	usageError := func(usage string) int {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	writeLine := func(value string) int {
		if _, err := fmt.Fprintln(stdout, value); err != nil {
			fmt.Fprintf(stderr, "write command output: %v\n", err)
			return 1
		}
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return usageError(cliUsage)
		}
		return writeLine(cliUsage)
	case "version", "-version", "--version":
		if len(args) != 1 {
			return usageError("usage: remnanode-lite version")
		}
		return writeLine(version.String())
	case "healthcheck":
		if len(args) != 1 {
			return usageError("usage: remnanode-lite healthcheck")
		}
		return internalHealthcheck(stderr)
	case "doctor":
		doctorArgs := args[1:]
		if len(doctorArgs) != 0 && (len(doctorArgs) != 2 || doctorArgs[0] != "--env" || doctorArgs[1] == "") {
			return usageError("usage: remnanode-lite doctor [--env PATH]")
		}
		return runDoctor(doctorArgs)
	case "kill-sockets", "--kill-sockets", "-k":
		if len(args) != 1 {
			return usageError("usage: remnanode-lite kill-sockets")
		}
		return killSocketsCommand(stdin, stdout, stderr, killSockets)
	case "validate-secret":
		if len(args) != 1 {
			return usageError("usage: remnanode-lite validate-secret < SECRET_KEY")
		}
		return validateSecretCommand(stdin, stderr)
	case "canonicalize-secret":
		if len(args) != 2 {
			return usageError("usage: remnanode-lite canonicalize-secret <path|->")
		}
		return canonicalizeSecretCommand(args[1], stdin, stdout, stderr)
	case "release-url":
		if len(args) != 3 {
			return usageError("usage: remnanode-lite release-url <tag> <arch>")
		}
		assetURL, err := version.ReleaseAssetURL(args[1], args[2])
		if err != nil {
			fmt.Fprintf(stderr, "release-url: %v\n", err)
			return 2
		}
		return writeLine(assetURL)
	case "install-script-url":
		if len(args) != 3 {
			return usageError("usage: remnanode-lite install-script-url <tag> <script>")
		}
		scriptURL, err := version.InstallScriptURL(args[1], args[2])
		if err != nil {
			fmt.Fprintf(stderr, "install-script-url: %v\n", err)
			return 2
		}
		return writeLine(scriptURL)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", args[0])
		fmt.Fprintln(stderr, cliUsage)
		return 1
	}
}

func internalHealthcheck(stderr io.Writer) int {
	path := strings.TrimSpace(os.Getenv("INTERNAL_SOCKET_PATH"))
	if path == "" {
		path = config.DefaultInternalSocketPath
	}
	ctx, cancel := context.WithTimeout(context.Background(), internalHealthcheckTimeout)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		fmt.Fprintf(stderr, "internal healthcheck failed: %v\n", err)
		return 1
	}
	if err := connection.Close(); err != nil {
		fmt.Fprintf(stderr, "internal healthcheck failed: close socket: %v\n", err)
		return 1
	}
	return 0
}

func killSocketsCommand(input io.Reader, stdout, stderr io.Writer, killSockets socketKiller) int {
	if _, err := io.WriteString(stdout, "Enter local or remote IP address to match: "); err != nil {
		fmt.Fprintf(stderr, "Failed to kill sockets: write prompt: %v\n", err)
		return 1
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64), 128)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(stderr, "Failed to kill sockets: read IP address: %v\n", err)
		} else {
			fmt.Fprintln(stderr, "Failed to kill sockets: IP address is required")
		}
		return 1
	}

	ipAddress := strings.TrimSpace(scanner.Text())
	if ipAddress == "" {
		fmt.Fprintln(stderr, "Failed to kill sockets: IP address is required")
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "Killing connected sockets whose local or remote IP matches: %s...\n", ipAddress); err != nil {
		fmt.Fprintf(stderr, "Failed to kill sockets: write progress: %v\n", err)
		return 1
	}
	if err := killSockets(context.Background(), ipAddress); err != nil {
		fmt.Fprintf(stderr, "Failed to kill sockets: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "Sockets killed successfully."); err != nil {
		fmt.Fprintf(stderr, "Failed to kill sockets: write result: %v\n", err)
		return 1
	}
	return 0
}

func validateSecretCommand(input io.Reader, stderr io.Writer) int {
	canonical, err := canonicalSecretFromReader(input)
	if err != nil {
		fmt.Fprintf(stderr, "read SECRET_KEY: %v\n", err)
		return 1
	}
	if _, err := secret.Parse(canonical); err != nil {
		fmt.Fprintf(stderr, "invalid SECRET_KEY: %v\n", err)
		return 1
	}
	return 0
}

func canonicalizeSecretCommand(path string, stdin io.Reader, stdout, stderr io.Writer) int {
	var (
		canonical string
		err       error
	)
	if path == "-" {
		canonical, err = canonicalSecretFromReader(stdin)
	} else {
		canonical, err = config.ReadSecretFile(path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "read Secret Key source: %v\n", err)
		return 1
	}
	if _, err := secret.Parse(canonical); err != nil {
		fmt.Fprintf(stderr, "invalid SECRET_KEY: %v\n", err)
		return 1
	}
	if _, err := io.WriteString(stdout, canonical); err != nil {
		fmt.Fprintf(stderr, "write canonical SECRET_KEY: %v\n", err)
		return 1
	}
	return 0
}

func canonicalSecretFromReader(input io.Reader) (string, error) {
	maxInputBytes := int64(secret.MaxEncodedBytes + 2)
	raw, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(raw)) > maxInputBytes {
		return "", fmt.Errorf("SECRET_KEY input exceeds %d bytes", maxInputBytes)
	}
	return config.CanonicalizeSecretFileContent(raw)
}

func runNode() (runErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(runtimeEnvPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	bodyBudget, err := bodylimit.New(cfg.LowMemory, cfg.BodyLimitMB)
	if err != nil {
		return fmt.Errorf("configure request body limit: %w", err)
	}
	applyMemoryLimit(cfg)
	if !netadmin.HasCapNetAdmin() {
		log.Printf("warning: CAP_NET_ADMIN not available — nftables and NETLINK_SOCK_DIAG socket destroy are disabled (check systemd AmbientCapabilities)")
	}

	payload, err := secret.Parse(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("parse SECRET_KEY: %w", err)
	}

	validator, err := auth.NewJWTValidator(payload.JWTPublicKey)
	if err != nil {
		return fmt.Errorf("initialize JWT validator: %w", err)
	}

	pluginState := plugin.NewState()
	if asnDB, err := asn.Open(cfg.ASNDBPath); err != nil {
		log.Printf("ASN database unavailable (%s): %v — asList shared lists resolve empty", cfg.ASNDBPath, err)
	} else {
		pluginState.SetASNResolver(asnDB)
		defer func() {
			if err := asnDB.Close(); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close ASN database: %w", err))
			}
		}()
		log.Printf("ASN database loaded from %s", cfg.ASNDBPath)
	}

	networkMonitor := system.NewNetworkMonitor()
	cleanup := &nodeComponentCleanup{stopNetwork: networkMonitor.Stop}
	cleanupComponents := cleanup.Run
	cleanupCompleted := false
	defer func() {
		if !cleanupCompleted {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), nodeShutdownTimeout)
			runErr = errors.Join(runErr, cleanupComponents(cleanupCtx))
			cancelCleanup()
		}
	}()

	systemCollector := system.NewCollector(networkMonitor)
	manager, err := xray.NewManager(xray.Options{
		Lifetime:           ctx,
		XrayBin:            cfg.XrayBin,
		GeoDir:             cfg.GeoDir,
		LogDir:             cfg.LogDir,
		InternalSocketPath: cfg.InternalSocketPath,
		InternalRESTToken:  cfg.InternalRESTToken,
		DisableHashCheck:   cfg.DisableHashedSetCheck,
		LowMemory:          cfg.LowMemory,
		NodeVersion:        version.ResolveContractVersion(cfg.NodeContractVersion),
		CoreVersion:        cfg.XrayCoreVersion,
		System:             systemCollector,
		TorrentBlocker:     pluginState,
	})
	if err != nil {
		return fmt.Errorf("initialize Xray manager: %w", err)
	}
	cleanup.shutdownManager = manager.Shutdown
	cleanup.stopCore = func() error {
		if response := manager.Stop(); !response.IsStopped {
			return errors.New("process did not stop")
		}
		return nil
	}
	dropper := connections.NewDropper(pluginState.IsWhitelisted)
	pluginService := plugin.NewService(pluginState, dropper, manager)
	cleanup.closePlugin = pluginService.CloseContext
	if err := pluginService.InitializeContext(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("initialize plugin service: %w", ctx.Err())
		}
		log.Printf("warning: plugin nftables unavailable; nft-dependent plugins are disabled: %v", err)
	}

	statsService := stats.NewService(manager, pluginService, systemCollector)
	handlerService := nodehandler.NewService(manager, dropper)

	server, err := httpserver.New(cfg, payload, httpserver.Dependencies{
		Validator: validator,
		Xray:      manager,
		Stats:     statsService,
		Handler:   handlerService,
		Plugins:   pluginService,
		Body:      bodyBudget,
	})
	if err != nil {
		return fmt.Errorf("initialize HTTPS server: %w", err)
	}

	unixServer := &unixconfig.Server{
		Path:     cfg.InternalSocketPath,
		Token:    cfg.InternalRESTToken,
		Provider: manager,
		Webhook:  pluginService,
	}

	serveErrors := make(chan error, 2)
	var servers sync.WaitGroup
	startServer := func(name string, serve func() error) {
		servers.Add(1)
		go func() {
			defer servers.Done()
			if err := serve(); err != nil {
				serveErrors <- fmt.Errorf("%s stopped: %w", name, err)
				return
			}
			if ctx.Err() == nil {
				serveErrors <- fmt.Errorf("%s stopped unexpectedly", name)
			}
		}()
	}

	log.Printf("internal config socket listening on %s", cfg.InternalSocketPath)
	startServer("internal config socket", func() error { return unixServer.ListenAndServe(ctx) })
	log.Printf("remnanode-lite listening on %s", cfg.HTTPAddr())
	startServer("HTTPS server", func() error { return server.ListenAndServeTLS(ctx) })

	servers.Add(1)
	go func() {
		defer servers.Done()
		manager.StartLogRotation(ctx)
	}()

	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		runErr = errors.Join(runErr, err)
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), nodeShutdownTimeout)
	defer cancel()
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- cleanupComponents(shutdownCtx) }()
	if err := server.Shutdown(shutdownCtx); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("shutdown HTTPS server: %w", err))
		if closeErr := server.Close(); closeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("force close HTTPS server: %w", closeErr))
		}
	}

	serversDone := make(chan struct{})
	go func() {
		servers.Wait()
		close(serversDone)
	}()
	select {
	case <-serversDone:
	case <-shutdownCtx.Done():
		runErr = errors.Join(runErr, fmt.Errorf("wait for servers: %w", shutdownCtx.Err()))
	}
	runErr = errors.Join(runErr, <-cleanupDone)
	cleanupCompleted = true
	for {
		select {
		case err := <-serveErrors:
			runErr = errors.Join(runErr, err)
		default:
			return runErr
		}
	}
}

func runtimeEnvPath() string {
	if path := strings.TrimSpace(os.Getenv("REMNANODE_ENV")); path != "" {
		return path
	}
	return config.ResolveEnvPath()
}

// applyMemoryLimit runs after the bounded config read and before Secret parsing
// or server construction. Explicit GOMEMLIMIT data wins over LOW_MEMORY.
func applyMemoryLimit(cfg config.Config) {
	if cfg.GoMemoryLimitSet {
		debug.SetMemoryLimit(cfg.GoMemoryLimitBytes)
		return
	}
	if cfg.LowMemory {
		debug.SetMemoryLimit(180 << 20)
		log.Printf("low-memory mode: Go soft memory limit set to 180MiB (override with GOMEMLIMIT)")
	}
}
