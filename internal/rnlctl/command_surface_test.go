package rnlctl

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

const expectedRootHelp = `Usage: rnlctl [--quiet|-q] [--no-color] [--progress MODE] <command>

Quick start:
  rnlctl overview                 Show a concise operator summary
  rnlctl doctor                   Run deployment diagnostics
  rnlctl logs node                Show recent Node logs

Commands:
  version                         Show the rnlctl version
  install [options]               Install one verified Native bundle
  activate [options]              Activate a prepared installation
  upgrade [options]               Upgrade to one complete generation
  rollback [--to ID]              Roll back to the retained generation
  repair [options]                Recover the committed generation
  uninstall [--purge --yes]       Remove the Native installation
  config <command>                Inspect or change Native Node configuration
  secret set [options]            Replace the managed Panel Secret
  overview                        Show a concise operator summary
  status [--json]                 Show Native lifecycle status
  doctor [--json]                 Run deployment diagnostics
  start                           Start the service
  stop                            Stop the service
  restart                         Restart the service
  logs <source> [options]         Show logs from node, core, or core-errors
  completion <bash|zsh|fish>      Generate shell completion

Global options:
  --quiet, -q                     Suppress routine success and health summaries
  --no-color                      Disable color in human-readable output
  --progress MODE                 Progress output: auto, plain, or never (default: auto)

Log options:
  --follow, -f                    Continue following new log entries
  --lines N, -n N                 Show the last N lines (default: 50)
  --since DURATION                Show recent systemd Node logs (for example 15m)

Use "rnlctl logs --help" for log source details.
`

const expectedInstallHelp = `Usage: rnlctl install (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX) [options]

Options:
  --expected-version VERSION  Require the manifest to contain this exact version
  --port PORT                 Node HTTPS port (default: 2222 on a new install)
  --secret-file PATH          Read the Secret Key from a regular file
  --prepare-only              Install stopped and disabled; Secret may be omitted
`

const expectedUpgradeHelp = `Usage: rnlctl upgrade (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX | --to VERSION)

Options:
  --expected-version VERSION  Require a local bundle manifest version
  --to VERSION                Download an exact X.Y.Z or X.Y.Z-rnl.N release
  --dry-run                   Verify the candidate and known host preconditions
  --json                      Emit the dry-run plan as JSON (requires --dry-run)
`

const expectedConfigHelp = `Usage: rnlctl config <show|get|set|unset|check|apply>

Commands:
  show [--json]                    Show safe administrator-controlled values
  get KEY                          Print one administrator-controlled value
  set KEY=VALUE... [--apply]       Set and validate one or more values
  unset KEY... [--apply]           Remove one or more optional values
  check                            Validate the installed Native configuration
  apply                            Restart the active service and verify health

Managed runtime assignments and secret material are not exposed or editable.
The JSON envelope identifies the managed node.env file without exposing those values.
`

const expectedSecretHelp = `Usage: rnlctl secret set --file PATH [--apply]

The Secret is read from a bounded regular file and is never accepted as a value
argument or written to command output.
`

const expectedLogsHelp = `Usage: rnlctl logs <node|core|core-errors> [--follow] [--lines N] [--since DURATION]

Sources:
  node         remnanode-lite service output
  core         rw-core standard output
  core-errors  rw-core standard error

Options:
  --follow, -f       Continue following new log entries
  --lines N, -n N    Show the last N lines (default: 50)
  --since DURATION   Limit systemd Node logs to a recent duration, such as 15m

--since is available only for the node source on systemd hosts. OpenRC and
rw-core log files do not provide a reliable common timestamp format.
`

const expectedCompletionHelp = `Usage: rnlctl completion <bash|zsh|fish>

Print a shell completion script to standard output. The command does not
install files or modify shell startup configuration.
`

type publicHelpCase struct {
	path string
	args []string
	want string
}

func publicCommandHelpCases() []publicHelpCase {
	return []publicHelpCase{
		{path: "", want: expectedRootHelp},
		{path: "version", args: []string{"version", "--help"}, want: "Usage: rnlctl version\n"},
		{path: "install", args: []string{"install", "--help"}, want: expectedInstallHelp},
		{path: "activate", args: []string{"activate", "--help"}, want: "Usage: rnlctl activate [--secret-file PATH]\n"},
		{path: "upgrade", args: []string{"upgrade", "--help"}, want: expectedUpgradeHelp},
		{path: "rollback", args: []string{"rollback", "--help"}, want: "Usage: rnlctl rollback [--to GENERATION-ID]\n"},
		{path: "repair", args: []string{"repair", "--help"}, want: "Usage: rnlctl repair [--bundle-root DIR | --bundle ARCHIVE --sha256 HEX] [--expected-version VERSION]\n"},
		{path: "uninstall", args: []string{"uninstall", "--help"}, want: "Usage: rnlctl uninstall [--purge --yes]\n"},
		{path: "config", args: []string{"config", "--help"}, want: expectedConfigHelp},
		{path: "config show", args: []string{"config", "show", "--help"}, want: "Usage: rnlctl config show [--json]\n"},
		{path: "config get", args: []string{"config", "get", "--help"}, want: "Usage: rnlctl config get KEY\n"},
		{path: "config set", args: []string{"config", "set", "--help"}, want: "Usage: rnlctl config set KEY=VALUE... [--apply]\n"},
		{path: "config unset", args: []string{"config", "unset", "--help"}, want: "Usage: rnlctl config unset KEY... [--apply]\n"},
		{path: "config check", args: []string{"config", "check", "--help"}, want: "Usage: rnlctl config check\n"},
		{path: "config apply", args: []string{"config", "apply", "--help"}, want: "Usage: rnlctl config apply\n"},
		{path: "secret", args: []string{"secret", "--help"}, want: expectedSecretHelp},
		{path: "secret set", args: []string{"secret", "set", "--help"}, want: expectedSecretHelp},
		{path: "overview", args: []string{"overview", "--help"}, want: "Usage: rnlctl overview\n"},
		{path: "status", args: []string{"status", "--help"}, want: "Usage: rnlctl status [--json]\n"},
		{path: "doctor", args: []string{"doctor", "--help"}, want: "Usage: rnlctl doctor [--json]\n"},
		{path: "start", args: []string{"start", "--help"}, want: "Usage: rnlctl start\n"},
		{path: "stop", args: []string{"stop", "--help"}, want: "Usage: rnlctl stop\n"},
		{path: "restart", args: []string{"restart", "--help"}, want: "Usage: rnlctl restart\n"},
		{path: "logs", args: []string{"logs", "--help"}, want: expectedLogsHelp},
		{path: "completion", args: []string{"completion", "--help"}, want: expectedCompletionHelp},
		{path: "help", args: []string{"help"}, want: expectedRootHelp},
	}
}

func TestPublicCommandHelpAndDispatchSurface(t *testing.T) {
	for _, test := range publicCommandHelpCases() {
		t.Run(testNameForCommandPath(test.path), func(t *testing.T) {
			lifecycle := &fakeLifecycle{}
			runner := &recordingRunner{}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle: lifecycle,
				Runner:    runner,
				Stdout:    &stdout,
				Stderr:    &stderr,
			})

			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("Run(%q) stdout mismatch\n--- got ---\n%s--- want ---\n%s", test.args, got, test.want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("Run(%q) stderr = %q, want empty", test.args, stderr.String())
			}
			if len(lifecycle.called) != 0 || len(runner.commands) != 0 {
				t.Fatalf("Run(%q) caused side effects: lifecycle = %q, commands = %#v", test.args, lifecycle.called, runner.commands)
			}
		})
	}
}

func TestRootHelpAliasesHaveExactOutput(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			application := New(Options{Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", args, code, stderr.String())
			}
			if stdout.String() != expectedRootHelp || stderr.Len() != 0 {
				t.Fatalf("Run(%q): stdout = %q, stderr = %q", args, stdout.String(), stderr.String())
			}
		})
	}
}

type publicOptionSurface struct {
	Long            string
	Short           string
	Value           string
	ValueCandidates []string
	UnavailableWith []string
}

type publicCommandSurface struct {
	Path    string
	Options []publicOptionSurface
}

func expectedPublicCommandSurface() []publicCommandSurface {
	return []publicCommandSurface{
		{Path: "", Options: []publicOptionSurface{
			{Long: "quiet", Short: "q", Value: "none"},
			{Long: "no-color", Value: "none"},
			{Long: "progress", Value: "word", ValueCandidates: []string{"auto", "plain", "never"}},
		}},
		{Path: "version"},
		{Path: "install", Options: []publicOptionSurface{
			{Long: "bundle-root", Value: "directory"},
			{Long: "bundle", Value: "file"},
			{Long: "sha256", Value: "word"},
			{Long: "expected-version", Value: "word"},
			{Long: "port", Value: "word"},
			{Long: "secret-file", Value: "file"},
			{Long: "prepare-only", Value: "none"},
		}},
		{Path: "activate", Options: []publicOptionSurface{{Long: "secret-file", Value: "file"}}},
		{Path: "upgrade", Options: []publicOptionSurface{
			{Long: "bundle-root", Value: "directory"},
			{Long: "bundle", Value: "file"},
			{Long: "sha256", Value: "word"},
			{Long: "expected-version", Value: "word"},
			{Long: "to", Value: "word"},
			{Long: "dry-run", Value: "none"},
			{Long: "json", Value: "none"},
		}},
		{Path: "rollback", Options: []publicOptionSurface{{Long: "to", Value: "word"}}},
		{Path: "repair", Options: []publicOptionSurface{
			{Long: "bundle-root", Value: "directory"},
			{Long: "bundle", Value: "file"},
			{Long: "sha256", Value: "word"},
			{Long: "expected-version", Value: "word"},
		}},
		{Path: "uninstall", Options: []publicOptionSurface{
			{Long: "purge", Value: "none"},
			{Long: "yes", Value: "none"},
		}},
		{Path: "config"},
		{Path: "config show", Options: []publicOptionSurface{{Long: "json", Value: "none"}}},
		{Path: "config get"},
		{Path: "config set", Options: []publicOptionSurface{{Long: "apply", Value: "none"}}},
		{Path: "config unset", Options: []publicOptionSurface{{Long: "apply", Value: "none"}}},
		{Path: "config check"},
		{Path: "config apply"},
		{Path: "secret"},
		{Path: "secret set", Options: []publicOptionSurface{
			{Long: "file", Value: "file"},
			{Long: "apply", Value: "none"},
		}},
		{Path: "overview"},
		{Path: "status", Options: []publicOptionSurface{{Long: "json", Value: "none"}}},
		{Path: "doctor", Options: []publicOptionSurface{{Long: "json", Value: "none"}}},
		{Path: "start"},
		{Path: "stop"},
		{Path: "restart"},
		{Path: "logs", Options: []publicOptionSurface{
			{Long: "follow", Short: "f", Value: "none"},
			{Long: "lines", Short: "n", Value: "word"},
			{Long: "since", Value: "word", UnavailableWith: []string{"core", "core-errors"}},
		}},
		{Path: "completion"},
		{Path: "help"},
	}
}

func TestPublicCommandAndOptionSurfaceMatchesCommandSpecification(t *testing.T) {
	root := rnlctlCommandSpec()
	actual := []publicCommandSurface{{Path: "", Options: publicOptionsFromCommandSpec(root.Options)}}
	visitCompletionCommands(root, nil, func(path []string, command commandSpec) {
		actual = append(actual, publicCommandSurface{
			Path:    strings.Join(path, " "),
			Options: publicOptionsFromCommandSpec(command.Options),
		})
	})

	want := expectedPublicCommandSurface()
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("public command surface mismatch\nactual: %#v\nwant:   %#v", actual, want)
	}

	helpPaths := make([]string, 0, len(publicCommandHelpCases()))
	for _, help := range publicCommandHelpCases() {
		helpPaths = append(helpPaths, help.path)
	}
	surfacePaths := make([]string, 0, len(want))
	for _, command := range want {
		surfacePaths = append(surfacePaths, command.Path)
	}
	if !reflect.DeepEqual(helpPaths, surfacePaths) {
		t.Fatalf("help dispatch paths = %q, want public command paths %q", helpPaths, surfacePaths)
	}
}

func publicOptionsFromCommandSpec(options []commandOptionSpec) []publicOptionSurface {
	if len(options) == 0 {
		return nil
	}
	result := make([]publicOptionSurface, 0, len(options))
	for _, option := range options {
		var valueCandidates []string
		if len(option.ValueCandidates) != 0 {
			valueCandidates = make([]string, 0, len(option.ValueCandidates))
		}
		for _, candidate := range option.ValueCandidates {
			valueCandidates = append(valueCandidates, candidate.Value)
		}
		result = append(result, publicOptionSurface{
			Long:            option.Long,
			Short:           option.Short,
			Value:           commandValueNameForSurfaceTest(option.Value),
			ValueCandidates: valueCandidates,
			UnavailableWith: append([]string(nil), option.UnavailableWith...),
		})
	}
	return result
}

func commandValueNameForSurfaceTest(value commandValueKind) string {
	switch value {
	case commandValueNone:
		return "none"
	case commandValueWord:
		return "word"
	case commandValueFile:
		return "file"
	case commandValueDirectory:
		return "directory"
	default:
		return "unknown"
	}
}

func testNameForCommandPath(path string) string {
	if path == "" {
		return "root"
	}
	return strings.ReplaceAll(path, " ", "_")
}
