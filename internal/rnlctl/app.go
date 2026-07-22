// Package rnlctl implements the host administration CLI for Remnanode Lite.
package rnlctl

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/luxiaba/remnanode-lite/internal/version"
)

const (
	defaultLogLines = 50
	maxLogLines     = 100_000

	systemdService = "remnanode-lite.service"
	openRCService  = "remnanode-lite"
	systemdRuntime = "/run/systemd/system"
	openRCRuntime  = "/run/openrc"
	logDirectory   = "/var/log/remnanode-lite"
)

const usage = `Usage: rnlctl <command>

Commands:
  version                         Show the rnlctl version
  install [options]               Install one verified Native bundle
  activate [options]              Activate a prepared installation
  upgrade [options]               Upgrade to one complete generation
  rollback [--to ID]              Roll back to the retained generation
  repair [options]                Recover the committed generation
  uninstall [--purge --yes]       Remove the Native installation
  status [--json]                 Show service or lifecycle status
  doctor [--json]                 Run deployment diagnostics
  start                           Start the service
  stop                            Stop the service
  restart                         Restart the service
  logs <source> [options]         Show logs from node, core, or core-errors

Log options:
  --follow, -f                    Continue following new log entries
  --lines N, -n N                 Show the last N lines (default: 50)

Use "rnlctl logs --help" for log source details.
`

const logsUsage = `Usage: rnlctl logs <node|core|core-errors> [--follow] [--lines N]

Sources:
  node         remnanode-lite service output
  core         rw-core standard output
  core-errors  rw-core standard error

Options:
  --follow, -f       Continue following new log entries
  --lines N, -n N    Show the last N lines (default: 50)
`

// LookPathFunc resolves an executable without invoking a shell.
type LookPathFunc func(string) (string, error)

// PathExistsFunc reports whether a host path exists.
type PathExistsFunc func(string) bool

// Options contains the process and I/O dependencies used by App.
type Options struct {
	Runner        Runner
	LookPath      LookPathFunc
	PathExists    PathExistsFunc
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	VersionString string
	Lifecycle     Lifecycle
}

// App parses rnlctl commands and dispatches them to the host service manager.
type App struct {
	runner        Runner
	lookPath      LookPathFunc
	pathExists    PathExistsFunc
	stdin         io.Reader
	stdout        io.Writer
	stderr        io.Writer
	versionString string
	lifecycle     Lifecycle
}

// New creates an rnlctl application with production defaults for omitted
// dependencies.
func New(options Options) *App {
	if options.Runner == nil {
		options.Runner = NewProcessRunner()
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.PathExists == nil {
		options.PathExists = pathExists
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.VersionString == "" {
		options.VersionString = fmt.Sprintf(
			"rnlctl %s (contract %s)",
			version.Version,
			version.ContractVersion,
		)
	}
	if options.Lifecycle == nil {
		options.Lifecycle = NewEngine(EngineOptions{})
	}
	return &App{
		runner:        options.Runner,
		lookPath:      options.LookPath,
		pathExists:    options.PathExists,
		stdin:         options.Stdin,
		stdout:        options.Stdout,
		stderr:        options.Stderr,
		versionString: options.VersionString,
		lifecycle:     options.Lifecycle,
	}
}

// Run executes one rnlctl command and returns a process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 {
		return a.write(a.stdout, usage)
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return a.usageError("help does not accept arguments", usage)
		}
		return a.write(a.stdout, usage)
	case "version", "-version", "--version":
		if code, handled := a.commandHelpOrReject(args, "Usage: rnlctl version\n"); handled {
			return code
		}
		return a.write(a.stdout, a.versionString+"\n")
	case "start", "stop", "restart":
		if code, handled := a.commandHelpOrReject(args, "Usage: rnlctl "+args[0]+"\n"); handled {
			return code
		}
		var result Result
		var err error
		switch args[0] {
		case "start":
			result, err = a.lifecycle.Start(ctx)
		case "stop":
			result, err = a.lifecycle.Stop(ctx)
		case "restart":
			result, err = a.lifecycle.Restart(ctx)
		}
		return a.lifecycleResult(result, err)
	case "install":
		return a.runInstall(ctx, args[1:])
	case "activate":
		return a.runActivate(ctx, args[1:])
	case "upgrade":
		return a.runUpgrade(ctx, args[1:])
	case "rollback":
		return a.runRollback(ctx, args[1:])
	case "repair":
		return a.runRepair(ctx, args[1:])
	case "uninstall":
		return a.runUninstall(ctx, args[1:])
	case "status":
		return a.runStatus(ctx, args[1:])
	case "doctor":
		return a.runDoctor(ctx, args[1:])
	case "logs":
		return a.runLogs(ctx, args[1:])
	default:
		return a.usageError(fmt.Sprintf("unknown command %q", args[0]), usage)
	}
}

const installUsage = `Usage: rnlctl install (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX) [options]

Options:
  --expected-version VERSION  Require the manifest to contain this exact version
  --port PORT                 Node HTTPS port (default: 2222 on a new install)
  --secret-file PATH          Read the Secret Key from a regular file
  --prepare-only              Install stopped and disabled; Secret may be omitted
`

const activateUsage = `Usage: rnlctl activate [--secret-file PATH]
`

const upgradeUsage = `Usage: rnlctl upgrade (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX | --to VERSION)

Options:
  --expected-version VERSION  Require a local bundle manifest version
  --to VERSION                Download an exact X.Y.Z or X.Y.Z-rnl.N release
`

const rollbackUsage = `Usage: rnlctl rollback [--to GENERATION-ID]
`

const repairUsage = `Usage: rnlctl repair [--bundle-root DIR | --bundle ARCHIVE --sha256 HEX] [--expected-version VERSION]
`

const uninstallUsage = `Usage: rnlctl uninstall [--purge --yes]
`

func (a *App) runInstall(ctx context.Context, args []string) int {
	flags := a.flagSet("install", installUsage)
	request := InstallRequest{}
	bindBundleFlags(flags, &request.Bundle)
	flags.IntVar(&request.Port, "port", 0, "")
	flags.StringVar(&request.SecretFile, "secret-file", "", "")
	flags.BoolVar(&request.PrepareOnly, "prepare-only", false, "")
	if code, ok := a.parseFlags(flags, args, installUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Install(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runActivate(ctx context.Context, args []string) int {
	flags := a.flagSet("activate", activateUsage)
	request := ActivateRequest{}
	flags.StringVar(&request.SecretFile, "secret-file", "", "")
	if code, ok := a.parseFlags(flags, args, activateUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Activate(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runUpgrade(ctx context.Context, args []string) int {
	flags := a.flagSet("upgrade", upgradeUsage)
	request := UpgradeRequest{}
	bindBundleFlags(flags, &request.Bundle)
	flags.StringVar(&request.To, "to", "", "")
	if code, ok := a.parseFlags(flags, args, upgradeUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Upgrade(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runRollback(ctx context.Context, args []string) int {
	flags := a.flagSet("rollback", rollbackUsage)
	request := RollbackRequest{}
	flags.StringVar(&request.GenerationID, "to", "", "")
	if code, ok := a.parseFlags(flags, args, rollbackUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Rollback(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runRepair(ctx context.Context, args []string) int {
	flags := a.flagSet("repair", repairUsage)
	request := RepairRequest{}
	bindBundleFlags(flags, &request.Bundle)
	if code, ok := a.parseFlags(flags, args, repairUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Repair(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runUninstall(ctx context.Context, args []string) int {
	flags := a.flagSet("uninstall", uninstallUsage)
	request := UninstallRequest{}
	flags.BoolVar(&request.Purge, "purge", false, "")
	flags.BoolVar(&request.Yes, "yes", false, "")
	if code, ok := a.parseFlags(flags, args, uninstallUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Uninstall(ctx, request)
	return a.lifecycleResult(result, err)
}

func (a *App) runStatus(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.runServiceCommand(ctx, "status")
	}
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, "Usage: rnlctl status [--json]\n")
	}
	if len(args) != 1 || args[0] != "--json" {
		return a.usageError("status accepts only --json", "Usage: rnlctl status [--json]\n")
	}
	status, err := a.lifecycle.Status(ctx)
	if err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: status: %v\n", err)
		return 1
	}
	if err := a.writeJSON(status); err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: status: %v\n", err)
		return 1
	}
	if !status.Healthy && status.Deployment != "absent" {
		return 1
	}
	return 0
}

func (a *App) runDoctor(ctx context.Context, args []string) int {
	if len(args) == 0 {
		report, err := a.lifecycle.Doctor(ctx)
		if err != nil {
			fmt.Fprintf(a.stderr, "rnlctl: doctor: %v\n", err)
			return 1
		}
		for _, check := range report.Checks {
			line := "[" + strings.ToUpper(check.Status) + "] " + check.Name
			if check.Detail != "" {
				line += " - " + check.Detail
			}
			if code := a.write(a.stdout, line+"\n"); code != 0 {
				return code
			}
		}
		if !report.Healthy {
			return 1
		}
		return 0
	}
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, "Usage: rnlctl doctor [--json]\n")
	}
	if len(args) != 1 || args[0] != "--json" {
		return a.usageError("doctor accepts only --json", "Usage: rnlctl doctor [--json]\n")
	}
	report, err := a.lifecycle.Doctor(ctx)
	if err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: doctor: %v\n", err)
		return 1
	}
	if err := a.writeJSON(report); err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: doctor: %v\n", err)
		return 1
	}
	if !report.Healthy {
		return 1
	}
	return 0
}

func bindBundleFlags(flags *flag.FlagSet, input *BundleInput) {
	flags.StringVar(&input.Root, "bundle-root", "", "")
	flags.StringVar(&input.Archive, "bundle", "", "")
	flags.StringVar(&input.SHA256, "sha256", "", "")
	flags.StringVar(&input.ExpectedVersion, "expected-version", "", "")
}

func (a *App) flagSet(name, commandUsage string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	return flags
}

func (a *App) parseFlags(flags *flag.FlagSet, args []string, commandUsage string) (int, bool) {
	seen := make(map[string]struct{})
	for _, argument := range args {
		if isHelp(argument) {
			return a.write(a.stdout, commandUsage), false
		}
		if strings.HasPrefix(argument, "--") && argument != "--" {
			name := strings.TrimPrefix(argument, "--")
			if index := strings.IndexByte(name, '='); index >= 0 {
				name = name[:index]
			}
			if _, duplicate := seen[name]; duplicate {
				return a.usageError("option --"+name+" may be specified only once", commandUsage), false
			}
			seen[name] = struct{}{}
		}
	}
	if err := flags.Parse(args); err != nil {
		return a.usageError(err.Error(), commandUsage), false
	}
	if flags.NArg() != 0 {
		return a.usageError("unexpected positional arguments", commandUsage), false
	}
	return 0, true
}

func (a *App) lifecycleResult(result Result, err error) int {
	if err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: %v\n", err)
		return 1
	}
	verb := "unchanged"
	if result.Changed {
		verb = "completed"
	}
	line := fmt.Sprintf("%s %s", result.Operation, verb)
	if result.Version != "" {
		line += ": " + result.Version
	}
	return a.write(a.stdout, line+"\n")
}

func (a *App) writeJSON(value any) error {
	encoder := json.NewEncoder(a.stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func (a *App) commandHelpOrReject(args []string, commandUsage string) (int, bool) {
	if len(args) == 1 {
		return 0, false
	}
	if len(args) == 2 && isHelp(args[1]) {
		return a.write(a.stdout, commandUsage), true
	}
	return a.usageError(args[0]+" does not accept arguments", commandUsage), true
}

func (a *App) runServiceCommand(ctx context.Context, action string) int {
	manager, err := a.detectServiceManager()
	if err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: %v\n", err)
		return 1
	}

	var args []string
	switch manager.kind {
	case serviceManagerSystemd:
		if action == "status" {
			args = []string{"--no-pager", "--full", "status", systemdService}
		} else {
			args = []string{action, systemdService}
		}
	case serviceManagerOpenRC:
		args = []string{openRCService, action}
	}
	return a.runExternal(ctx, manager.executable, args)
}

func (a *App) runLogs(ctx context.Context, args []string) int {
	options, showHelp, err := parseLogsArgs(args)
	if showHelp {
		return a.write(a.stdout, logsUsage)
	}
	if err != nil {
		return a.usageError(err.Error(), logsUsage)
	}

	lineCount := strconv.Itoa(options.lines)
	switch options.source {
	case "node":
		manager, detectErr := a.detectServiceManager()
		if detectErr != nil {
			fmt.Fprintf(a.stderr, "rnlctl: %v\n", detectErr)
			return 1
		}
		if manager.kind == serviceManagerSystemd {
			journalctl, findErr := a.requireExecutable("journalctl")
			if findErr != nil {
				fmt.Fprintf(a.stderr, "rnlctl: %v\n", findErr)
				return 1
			}
			journalArgs := []string{
				"--no-pager",
				"--unit", systemdService,
				"--lines", lineCount,
			}
			if options.follow {
				journalArgs = append(journalArgs, "--follow")
			}
			return a.runExternal(ctx, journalctl, journalArgs)
		}
		return a.runTail(ctx, options, []string{
			logDirectory + "/openrc.log",
			logDirectory + "/openrc.err.log",
		})
	case "core":
		return a.runTail(ctx, options, []string{logDirectory + "/xray.out.log"})
	case "core-errors":
		return a.runTail(ctx, options, []string{logDirectory + "/xray.err.log"})
	default:
		panic("unreachable log source")
	}
}

func (a *App) runTail(ctx context.Context, options logOptions, paths []string) int {
	tail, err := a.requireExecutable("tail")
	if err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: %v\n", err)
		return 1
	}
	tailArgs := []string{"-n", strconv.Itoa(options.lines)}
	if options.follow {
		// -F follows the path across the runtime's bounded log rotation.
		tailArgs = append(tailArgs, "-F")
	}
	tailArgs = append(tailArgs, paths...)
	return a.runExternal(ctx, tail, tailArgs)
}

func (a *App) runExternal(ctx context.Context, name string, args []string) int {
	return a.runner.Run(ctx, Command{
		Name:   name,
		Args:   append([]string(nil), args...),
		Stdin:  a.stdin,
		Stdout: a.stdout,
		Stderr: a.stderr,
	})
}

type serviceManagerKind uint8

const (
	serviceManagerSystemd serviceManagerKind = iota + 1
	serviceManagerOpenRC
)

type serviceManager struct {
	kind       serviceManagerKind
	executable string
}

func (a *App) detectServiceManager() (serviceManager, error) {
	systemctl := a.findExecutable("systemctl")
	rcService := a.findExecutable("rc-service")

	if systemctl != "" && rcService != "" {
		switch {
		case a.pathExists(systemdRuntime):
			return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, nil
		case a.pathExists(openRCRuntime):
			return serviceManager{kind: serviceManagerOpenRC, executable: rcService}, nil
		default:
			// Prefer systemd when installed clients exist but runtime markers are
			// unavailable, for example while operating in a recovery chroot.
			return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, nil
		}
	}
	if systemctl != "" {
		return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, nil
	}
	if rcService != "" {
		return serviceManager{kind: serviceManagerOpenRC, executable: rcService}, nil
	}
	return serviceManager{}, fmt.Errorf("neither systemctl nor rc-service is available")
}

func (a *App) findExecutable(name string) string {
	path, err := a.lookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (a *App) requireExecutable(name string) (string, error) {
	path := a.findExecutable(name)
	if path == "" {
		return "", fmt.Errorf("required command %q is unavailable", name)
	}
	return path, nil
}

type logOptions struct {
	source string
	lines  int
	follow bool
}

func parseLogsArgs(args []string) (logOptions, bool, error) {
	options := logOptions{lines: defaultLogLines}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case isHelp(argument):
			return logOptions{}, true, nil
		case argument == "--follow" || argument == "-f":
			options.follow = true
		case argument == "--lines" || argument == "-n":
			index++
			if index >= len(args) {
				return logOptions{}, false, fmt.Errorf("%s requires a line count", argument)
			}
			lines, err := parseLogLines(args[index])
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case strings.HasPrefix(argument, "--lines="):
			lines, err := parseLogLines(strings.TrimPrefix(argument, "--lines="))
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case strings.HasPrefix(argument, "-n="):
			lines, err := parseLogLines(strings.TrimPrefix(argument, "-n="))
			if err != nil {
				return logOptions{}, false, err
			}
			options.lines = lines
		case strings.HasPrefix(argument, "-"):
			return logOptions{}, false, fmt.Errorf("unknown logs option %q", argument)
		case options.source == "":
			options.source = argument
		default:
			return logOptions{}, false, fmt.Errorf("logs accepts exactly one source")
		}
	}

	if options.source == "" {
		return logOptions{}, false, fmt.Errorf("logs requires a source")
	}
	switch options.source {
	case "node", "core", "core-errors":
		return options, false, nil
	default:
		return logOptions{}, false, fmt.Errorf("unknown log source %q", options.source)
	}
}

func parseLogLines(raw string) (int, error) {
	lines, err := strconv.Atoi(raw)
	if err != nil || lines < 1 || lines > maxLogLines {
		return 0, fmt.Errorf("log line count must be between 1 and %d", maxLogLines)
	}
	return lines, nil
}

func isHelp(argument string) bool {
	return argument == "help" || argument == "-h" || argument == "--help"
}

func (a *App) usageError(message, commandUsage string) int {
	if message != "" {
		fmt.Fprintf(a.stderr, "rnlctl: %s\n", message)
	}
	_, _ = io.WriteString(a.stderr, commandUsage)
	return 2
}

func (a *App) write(writer io.Writer, content string) int {
	if _, err := io.WriteString(writer, content); err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: write output: %v\n", err)
		return 1
	}
	return 0
}
