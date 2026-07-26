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
	"strings"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/version"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// LookPathFunc resolves an executable without invoking a shell.
type LookPathFunc func(string) (string, error)

// PathExistsFunc reports whether a host path exists.
type PathExistsFunc func(string) bool

// IsTerminalFunc reports whether a writer is connected to an interactive
// terminal. It is injectable so output behavior can be tested without a TTY.
type IsTerminalFunc func(io.Writer) bool

// LookupEnvFunc retrieves one environment variable without exposing the full
// process environment to tests.
type LookupEnvFunc func(string) (string, bool)

// NowFunc returns the current time. Log tests inject it so relative --since
// values produce deterministic journalctl arguments.
type NowFunc func() time.Time

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
	IsTerminal    IsTerminalFunc
	LookupEnv     LookupEnvFunc
	Now           NowFunc
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
	isTerminal    IsTerminalFunc
	lookupEnv     LookupEnvFunc
	now           NowFunc
	quiet         bool
	noColor       bool
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
	if options.IsTerminal == nil {
		options.IsTerminal = isTerminalWriter
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	if options.Now == nil {
		options.Now = time.Now
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
		isTerminal:    options.IsTerminal,
		lookupEnv:     options.LookupEnv,
		now:           options.Now,
	}
}

// Run executes one rnlctl command and returns a process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if ctx == nil {
		ctx = context.Background()
	}
	commandArgs, quiet, noColor, err := parseGlobalOptions(args)
	if err != nil {
		return a.usageError("", err.Error(), usageForCommand())
	}
	scoped := *a
	scoped.quiet = quiet
	scoped.noColor = noColor
	return scoped.run(ctx, commandArgs)
}

func (a *App) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.write(a.stdout, usageForCommand())
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return a.usageError("help", "does not accept arguments", usageForCommand())
		}
		return a.write(a.stdout, usageForCommand())
	case "version", "-version", "--version":
		if code, handled := a.commandHelpOrReject(args, usageForCommand("version")); handled {
			return code
		}
		return a.write(a.stdout, a.versionString+"\n")
	case "start", "stop", "restart":
		if code, handled := a.commandHelpOrReject(args, usageForCommand(args[0])); handled {
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
		return a.lifecycleResult(args[0], result, err)
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
	case "config":
		return a.runConfig(ctx, args[1:])
	case "secret":
		return a.runSecret(ctx, args[1:])
	case "status":
		return a.runStatus(ctx, args[1:])
	case "doctor":
		return a.runDoctor(ctx, args[1:])
	case "logs":
		return a.runLogs(ctx, args[1:])
	case "completion":
		return a.runCompletion(args[1:])
	default:
		return a.usageError("", fmt.Sprintf("unknown command %q", args[0]), usageForCommand())
	}
}

func parseGlobalOptions(args []string) ([]string, bool, bool, error) {
	commandArgs := make([]string, 0, len(args))
	quiet := false
	noColor := false
	for _, argument := range args {
		switch argument {
		case "--quiet", "-q":
			if quiet {
				return nil, false, false, fmt.Errorf("global option --quiet may be specified only once")
			}
			quiet = true
		case "--no-color":
			if noColor {
				return nil, false, false, fmt.Errorf("global option --no-color may be specified only once")
			}
			noColor = true
		default:
			commandArgs = append(commandArgs, argument)
		}
	}
	return commandArgs, quiet, noColor, nil
}

func (a *App) runInstall(ctx context.Context, args []string) int {
	installUsage := usageForCommand("install")
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
	return a.lifecycleResult("install", result, err)
}

func (a *App) runActivate(ctx context.Context, args []string) int {
	activateUsage := usageForCommand("activate")
	flags := a.flagSet("activate", activateUsage)
	request := ActivateRequest{}
	flags.StringVar(&request.SecretFile, "secret-file", "", "")
	if code, ok := a.parseFlags(flags, args, activateUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Activate(ctx, request)
	return a.lifecycleResult("activate", result, err)
}

func (a *App) runUpgrade(ctx context.Context, args []string) int {
	upgradeUsage := usageForCommand("upgrade")
	flags := a.flagSet("upgrade", upgradeUsage)
	request := UpgradeRequest{}
	dryRun := false
	jsonOutput := false
	bindBundleFlags(flags, &request.Bundle)
	flags.StringVar(&request.To, "to", "", "")
	flags.BoolVar(&dryRun, "dry-run", false, "")
	flags.BoolVar(&jsonOutput, "json", false, "")
	if code, ok := a.parseFlags(flags, args, upgradeUsage); !ok {
		return code
	}
	if jsonOutput && !dryRun {
		return a.usageError("upgrade", "--json requires --dry-run", upgradeUsage)
	}
	if dryRun {
		plan, err := a.lifecycle.PreflightUpgrade(ctx, request)
		if err != nil {
			return a.runtimeError("upgrade", err)
		}
		if jsonOutput {
			if err := a.writeJSON(plan); err != nil {
				return a.runtimeError("upgrade", err)
			}
			return exitOK
		}
		return a.write(a.stdout, renderUpgradePlan(plan))
	}
	result, err := a.lifecycle.Upgrade(ctx, request)
	return a.lifecycleResult("upgrade", result, err)
}

func (a *App) runRollback(ctx context.Context, args []string) int {
	rollbackUsage := usageForCommand("rollback")
	flags := a.flagSet("rollback", rollbackUsage)
	request := RollbackRequest{}
	flags.StringVar(&request.GenerationID, "to", "", "")
	if code, ok := a.parseFlags(flags, args, rollbackUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Rollback(ctx, request)
	return a.lifecycleResult("rollback", result, err)
}

func (a *App) runRepair(ctx context.Context, args []string) int {
	repairUsage := usageForCommand("repair")
	flags := a.flagSet("repair", repairUsage)
	request := RepairRequest{}
	bindBundleFlags(flags, &request.Bundle)
	if code, ok := a.parseFlags(flags, args, repairUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Repair(ctx, request)
	return a.lifecycleResult("repair", result, err)
}

func (a *App) runUninstall(ctx context.Context, args []string) int {
	uninstallUsage := usageForCommand("uninstall")
	flags := a.flagSet("uninstall", uninstallUsage)
	request := UninstallRequest{}
	flags.BoolVar(&request.Purge, "purge", false, "")
	flags.BoolVar(&request.Yes, "yes", false, "")
	if code, ok := a.parseFlags(flags, args, uninstallUsage); !ok {
		return code
	}
	result, err := a.lifecycle.Uninstall(ctx, request)
	return a.lifecycleResult("uninstall", result, err)
}

func (a *App) runStatus(ctx context.Context, args []string) int {
	statusUsage := usageForCommand("status")
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, statusUsage)
	}
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		return a.usageError("status", "accepts only --json", statusUsage)
	}
	status, err := a.lifecycle.Status(ctx)
	if err != nil {
		return a.runtimeError("status", err)
	}
	if len(args) == 1 {
		if err := a.writeJSON(status); err != nil {
			return a.runtimeError("status", err)
		}
	} else if !a.quiet {
		if code := a.write(a.stdout, renderStatus(status, a.colorEnabled())); code != exitOK {
			return code
		}
	}
	if !status.Healthy && status.Deployment != "absent" {
		return exitFailure
	}
	return exitOK
}

func (a *App) runDoctor(ctx context.Context, args []string) int {
	doctorUsage := usageForCommand("doctor")
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, doctorUsage)
	}
	if len(args) > 1 || (len(args) == 1 && args[0] != "--json") {
		return a.usageError("doctor", "accepts only --json", doctorUsage)
	}
	report, err := a.lifecycle.Doctor(ctx)
	if err != nil {
		return a.runtimeError("doctor", err)
	}
	if len(args) == 1 {
		if err := a.writeJSON(report); err != nil {
			return a.runtimeError("doctor", err)
		}
	} else if !a.quiet {
		if code := a.write(a.stdout, renderDoctor(report, a.colorEnabled())); code != exitOK {
			return code
		}
	}
	if !report.Healthy {
		return exitFailure
	}
	return exitOK
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
				return a.usageError(flags.Name(), "option --"+name+" may be specified only once", commandUsage), false
			}
			seen[name] = struct{}{}
		}
	}
	if err := flags.Parse(args); err != nil {
		return a.usageError(flags.Name(), err.Error(), commandUsage), false
	}
	if flags.NArg() != 0 {
		return a.usageError(flags.Name(), "unexpected positional arguments", commandUsage), false
	}
	return 0, true
}

func (a *App) lifecycleResult(command string, result Result, err error) int {
	if err != nil {
		return a.runtimeError(command, err)
	}
	if a.quiet {
		return exitOK
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
	return a.usageError(args[0], "does not accept arguments", commandUsage), true
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

func isHelp(argument string) bool {
	return argument == "help" || argument == "-h" || argument == "--help"
}

func (a *App) usageError(command, message, commandUsage string) int {
	if message != "" {
		prefix := "rnlctl"
		if command != "" {
			prefix += ": " + command
		}
		fmt.Fprintf(a.stderr, "%s: %s\n", prefix, message)
	}
	_, _ = io.WriteString(a.stderr, commandUsage)
	return exitUsage
}

func (a *App) runtimeError(command string, err error) int {
	prefix := "rnlctl"
	if command != "" {
		prefix += ": " + command
	}
	fmt.Fprintf(a.stderr, "%s: %v\n", prefix, err)
	return exitFailure
}

func (a *App) write(writer io.Writer, content string) int {
	if _, err := io.WriteString(writer, content); err != nil {
		fmt.Fprintf(a.stderr, "rnlctl: write output: %v\n", err)
		return exitFailure
	}
	return exitOK
}
