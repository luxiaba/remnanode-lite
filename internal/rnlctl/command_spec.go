package rnlctl

import (
	"fmt"
	"strings"
)

type commandValueKind uint8

const (
	commandValueNone commandValueKind = iota
	commandValueWord
	commandValueFile
	commandValueDirectory
)

type commandArgumentSpec struct {
	Value       string
	Description string
	NoSpace     bool
}

type commandOptionSpec struct {
	Long            string
	Short           string
	Description     string
	Value           commandValueKind
	ValueCandidates []commandArgumentSpec
	UnavailableWith []string
	HelpLabel       string
	HelpDescription string
}

type commandSpec struct {
	Name               string
	Description        string
	HelpListing        string
	HelpDescription    string
	HideFromParentHelp bool
	Help               commandHelpSpec
	Options            []commandOptionSpec
	Arguments          []commandArgumentSpec
	RepeatArgs         bool
	Commands           []commandSpec
}

var rnlctlCommandRegistry = buildRNLCTLCommandSpec()

func rnlctlCommandSpec() commandSpec {
	return rnlctlCommandRegistry
}

func findCommandSpec(path ...string) (commandSpec, bool) {
	command := rnlctlCommandRegistry
	for _, name := range path {
		found := false
		for _, child := range command.Commands {
			if child.Name == name {
				command = child
				found = true
				break
			}
		}
		if !found {
			return commandSpec{}, false
		}
	}
	return command, true
}

func buildRNLCTLCommandSpec() commandSpec {
	editableKeys := editableConfigurationCommandArguments(true, false)
	getKeys := editableConfigurationCommandArguments(false, false)
	unsetKeys := editableConfigurationCommandArguments(false, true)

	bundleOptions := []commandOptionSpec{
		{Long: "bundle-root", Description: "Extracted Native bundle directory", Value: commandValueDirectory},
		{Long: "bundle", Description: "Native bundle archive", Value: commandValueFile},
		{Long: "sha256", Description: "Expected bundle SHA-256", Value: commandValueWord},
		{Long: "expected-version", Description: "Required bundle version", Value: commandValueWord},
	}
	installOptions := cloneCommandOptions(bundleOptions)
	installOptions[3].HelpLabel = "--expected-version VERSION"
	installOptions[3].HelpDescription = "Require the manifest to contain this exact version"
	installOptions = append(installOptions,
		commandOptionSpec{
			Long: "port", Description: "Node HTTPS port", Value: commandValueWord,
			HelpLabel: "--port PORT", HelpDescription: "Node HTTPS port (default: 2222 on a new install)",
		},
		commandOptionSpec{
			Long: "secret-file", Description: "Panel Secret file", Value: commandValueFile,
			HelpLabel: "--secret-file PATH", HelpDescription: "Read the Secret Key from a regular file",
		},
		commandOptionSpec{
			Long: "prepare-only", Description: "Install stopped and disabled",
			HelpLabel: "--prepare-only", HelpDescription: "Install stopped and disabled; Secret may be omitted",
		},
	)
	upgradeOptions := cloneCommandOptions(bundleOptions)
	upgradeOptions[3].HelpLabel = "--expected-version VERSION"
	upgradeOptions[3].HelpDescription = "Require a local bundle manifest version"
	upgradeOptions = append(upgradeOptions,
		commandOptionSpec{
			Long: "to", Description: "Exact published version", Value: commandValueWord,
			HelpLabel: "--to VERSION", HelpDescription: "Download an exact X.Y.Z or X.Y.Z-rnl.N release",
		},
		commandOptionSpec{
			Long: "dry-run", Description: "Run upgrade preflight without changing the host",
			HelpLabel: "--dry-run", HelpDescription: "Verify the candidate and known host preconditions",
		},
		commandOptionSpec{
			Long: "json", Description: "Emit machine-readable preflight JSON",
			HelpLabel: "--json", HelpDescription: "Emit the dry-run plan as JSON (requires --dry-run)",
		},
	)

	configCommands := []commandSpec{
		{
			Name: "show", Description: "Show administrator-controlled values",
			HelpListing: "show [--json]", HelpDescription: "Show safe administrator-controlled values",
			Help: commandHelpSpec{Synopsis: "rnlctl config show [--json]"}, Options: jsonCommandOption(),
		},
		{
			Name: "get", Description: "Print one administrator-controlled value", HelpListing: "get KEY",
			Help: commandHelpSpec{Synopsis: "rnlctl config get KEY"}, Arguments: getKeys,
		},
		{
			Name: "set", Description: "Set and validate one or more values", HelpListing: "set KEY=VALUE... [--apply]",
			Help:      commandHelpSpec{Synopsis: "rnlctl config set KEY=VALUE... [--apply]"},
			Options:   []commandOptionSpec{{Long: "apply", Description: "Restart and verify the active service"}},
			Arguments: editableKeys, RepeatArgs: true,
		},
		{
			Name: "unset", Description: "Remove one or more optional values", HelpListing: "unset KEY... [--apply]",
			Help:      commandHelpSpec{Synopsis: "rnlctl config unset KEY... [--apply]"},
			Options:   []commandOptionSpec{{Long: "apply", Description: "Restart and verify the active service"}},
			Arguments: unsetKeys, RepeatArgs: true,
		},
		{
			Name: "check", Description: "Validate the installed Native configuration",
			Help: commandHelpSpec{Synopsis: "rnlctl config check"},
		},
		{
			Name: "apply", Description: "Restart the active service and verify health",
			Help: commandHelpSpec{Synopsis: "rnlctl config apply"},
		},
	}
	configCommand := commandSpec{
		Name: "config", Description: "Inspect or change Native Node configuration", HelpListing: "config <command>",
		Commands: configCommands,
	}
	configCommand.Help = commandHelpSpec{
		Synopsis: "rnlctl config <show|get|set|unset|check|apply>",
		Blocks: []commandHelpBlock{
			{Heading: "Commands", DescriptionColumn: 33, Rows: commandHelpRows(configCommand.Commands)},
			{Text: "Managed runtime assignments and secret material are not exposed or editable.\nThe JSON envelope identifies the managed node.env file without exposing those values."},
		},
	}

	secretHelp := commandHelpSpec{
		Synopsis: "rnlctl secret set --file PATH [--apply]",
		Blocks:   []commandHelpBlock{{Text: "The Secret is read from a bounded regular file and is never accepted as a value\nargument or written to command output."}},
	}
	secretCommand := commandSpec{
		Name: "secret", Description: "Manage the Panel Secret", HelpListing: "secret set [options]",
		HelpDescription: "Replace the managed Panel Secret", Help: secretHelp,
		Commands: []commandSpec{{
			Name: "set", Description: "Replace the managed Panel Secret", HelpListing: "set [options]", Help: secretHelp,
			Options: []commandOptionSpec{
				{Long: "file", Description: "File containing the new Secret", Value: commandValueFile},
				{Long: "apply", Description: "Restart and verify the active service"},
			},
		}},
	}

	logOptions := []commandOptionSpec{
		{
			Long: "follow", Short: "f", Description: "Follow new log entries",
			HelpLabel: "--follow, -f", HelpDescription: "Continue following new log entries",
		},
		{
			Long: "lines", Short: "n", Description: "Number of recent lines", Value: commandValueWord,
			HelpLabel: "--lines N, -n N", HelpDescription: "Show the last N lines (default: 50)",
		},
		{
			Long: "since", Description: "Show entries from a recent duration such as 15m", Value: commandValueWord,
			UnavailableWith: []string{"core", "core-errors"}, HelpLabel: "--since DURATION",
			HelpDescription: "Limit systemd Node logs to a recent duration, such as 15m",
		},
	}
	logArguments := []commandArgumentSpec{
		{Value: "node", Description: "remnanode-lite service output"},
		{Value: "core", Description: "rw-core standard output"},
		{Value: "core-errors", Description: "rw-core standard error"},
	}
	logsCommand := commandSpec{
		Name: "logs", Description: "Show Node or core logs", HelpListing: "logs <source> [options]",
		HelpDescription: "Show logs from node, core, or core-errors", Options: logOptions, Arguments: logArguments,
		Help: commandHelpSpec{
			Synopsis: "rnlctl logs <node|core|core-errors> [--follow] [--lines N] [--since DURATION]",
			Blocks: []commandHelpBlock{
				{Heading: "Sources", DescriptionColumn: 13, Rows: argumentHelpRows(logArguments)},
				{Heading: "Options", DescriptionColumn: 19, Rows: optionHelpRows(logOptions)},
				{Text: "--since is available only for the node source on systemd hosts. OpenRC and\nrw-core log files do not provide a reliable common timestamp format."},
			},
		},
	}
	rootLogOptions := cloneCommandOptions(logOptions)
	rootLogOptions[2].HelpDescription = "Show recent systemd Node logs (for example 15m)"

	rootOptions := []commandOptionSpec{
		{
			Long: "quiet", Short: "q", Description: "Suppress routine success and health summaries", HelpLabel: "--quiet, -q",
			HelpDescription: "Suppress routine success and health summaries",
		},
		{
			Long: "no-color", Description: "Disable terminal colors", HelpLabel: "--no-color",
			HelpDescription: "Disable color in human-readable output",
		},
		{
			Long: "progress", Description: "Set progress output mode", Value: commandValueWord,
			ValueCandidates: []commandArgumentSpec{
				{Value: "auto", Description: "Use terminal progress or plain output automatically"},
				{Value: "plain", Description: "Use stable line-oriented progress output"},
				{Value: "never", Description: "Disable progress output"},
			},
			HelpLabel: "--progress MODE", HelpDescription: "Progress output: auto, plain, or never (default: auto)",
		},
	}
	root := commandSpec{
		Name: "rnlctl", Description: "Remnanode Lite Native administration CLI", Options: rootOptions,
		Commands: []commandSpec{
			{Name: "version", Description: "Show the rnlctl version", Help: commandHelpSpec{Synopsis: "rnlctl version"}},
			{
				Name: "install", Description: "Install one verified Native bundle", HelpListing: "install [options]", Options: installOptions,
				Help: commandHelpSpec{
					Synopsis: "rnlctl install (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX) [options]",
					Blocks:   []commandHelpBlock{{Heading: "Options", DescriptionColumn: 28, Rows: optionHelpRows(installOptions)}},
				},
			},
			{
				Name: "activate", Description: "Activate a prepared installation", HelpListing: "activate [options]",
				Help:    commandHelpSpec{Synopsis: "rnlctl activate [--secret-file PATH]"},
				Options: []commandOptionSpec{{Long: "secret-file", Description: "Panel Secret file", Value: commandValueFile}},
			},
			{
				Name: "upgrade", Description: "Upgrade to one complete generation", HelpListing: "upgrade [options]", Options: upgradeOptions,
				Help: commandHelpSpec{
					Synopsis: "rnlctl upgrade (--bundle-root DIR | --bundle ARCHIVE --sha256 HEX | --to VERSION)",
					Blocks:   []commandHelpBlock{{Heading: "Options", DescriptionColumn: 28, Rows: optionHelpRows(upgradeOptions)}},
				},
			},
			{
				Name: "rollback", Description: "Roll back to the retained generation", HelpListing: "rollback [--to ID]",
				Help:    commandHelpSpec{Synopsis: "rnlctl rollback [--to GENERATION-ID]"},
				Options: []commandOptionSpec{{Long: "to", Description: "Retained generation ID", Value: commandValueWord}},
			},
			{
				Name: "repair", Description: "Recover the committed generation", HelpListing: "repair [options]",
				Help:    commandHelpSpec{Synopsis: "rnlctl repair [--bundle-root DIR | --bundle ARCHIVE --sha256 HEX] [--expected-version VERSION]"},
				Options: cloneCommandOptions(bundleOptions),
			},
			{
				Name: "uninstall", Description: "Remove the Native installation", HelpListing: "uninstall [--purge --yes]",
				Help: commandHelpSpec{Synopsis: "rnlctl uninstall [--purge --yes]"},
				Options: []commandOptionSpec{
					{Long: "purge", Description: "Remove retained state and owned account"},
					{Long: "yes", Description: "Confirm destructive purge"},
				},
			},
			configCommand,
			secretCommand,
			{
				Name: "status", Description: "Show service or lifecycle status", HelpListing: "status [--json]",
				HelpDescription: "Show Native lifecycle status", Help: commandHelpSpec{Synopsis: "rnlctl status [--json]"},
				Options: jsonCommandOption(),
			},
			{
				Name: "doctor", Description: "Run deployment diagnostics", HelpListing: "doctor [--json]",
				Help: commandHelpSpec{Synopsis: "rnlctl doctor [--json]"}, Options: jsonCommandOption(),
			},
			{Name: "start", Description: "Start the service", Help: commandHelpSpec{Synopsis: "rnlctl start"}},
			{Name: "stop", Description: "Stop the service", Help: commandHelpSpec{Synopsis: "rnlctl stop"}},
			{Name: "restart", Description: "Restart the service", Help: commandHelpSpec{Synopsis: "rnlctl restart"}},
			logsCommand,
			{
				Name: "completion", Description: "Print a shell completion script", HelpListing: "completion <bash|zsh|fish>",
				HelpDescription: "Generate shell completion",
				Help: commandHelpSpec{
					Synopsis: "rnlctl completion <bash|zsh|fish>",
					Blocks:   []commandHelpBlock{{Text: "Print a shell completion script to standard output. The command does not\ninstall files or modify shell startup configuration."}},
				},
				Arguments: []commandArgumentSpec{
					{Value: "bash", Description: "Bash completion"},
					{Value: "zsh", Description: "Zsh completion"},
					{Value: "fish", Description: "Fish completion"},
				},
			},
			{Name: "help", Description: "Show rnlctl help", HideFromParentHelp: true},
		},
	}
	root.Help = commandHelpSpec{
		Synopsis: "rnlctl [--quiet|-q] [--no-color] [--progress MODE] <command>",
		Blocks: []commandHelpBlock{
			{Heading: "Commands", DescriptionColumn: 32, Rows: commandHelpRows(root.Commands)},
			{Heading: "Global options", DescriptionColumn: 32, Rows: optionHelpRows(root.Options)},
			{Heading: "Log options", DescriptionColumn: 32, Rows: optionHelpRows(rootLogOptions)},
			{Text: "Use \"rnlctl logs --help\" for log source details."},
		},
	}
	for index := range root.Commands {
		if root.Commands[index].Name == "help" {
			root.Commands[index].Help = root.Help
			break
		}
	}
	return root
}

func cloneCommandOptions(options []commandOptionSpec) []commandOptionSpec {
	return append([]commandOptionSpec(nil), options...)
}

func jsonCommandOption() []commandOptionSpec {
	return []commandOptionSpec{{Long: "json", Description: "Emit machine-readable JSON"}}
}

func editableConfigurationCommandArguments(assignments, optionalOnly bool) []commandArgumentSpec {
	result := make([]commandArgumentSpec, 0, len(editableConfigurationKeySpecs))
	for _, spec := range editableConfigurationKeySpecs {
		if optionalOnly && !spec.Optional {
			continue
		}
		value := spec.Name
		if assignments {
			value += "="
		}
		result = append(result, commandArgumentSpec{
			Value:       value,
			Description: spec.Description,
			NoSpace:     assignments,
		})
	}
	return result
}

func validateCommandSpec(root commandSpec) error {
	if root.Name != "rnlctl" || len(root.Commands) == 0 {
		return fmt.Errorf("root command is incomplete")
	}
	var validate func(commandSpec, string, int) error
	validate = func(command commandSpec, parent string, depth int) error {
		path := strings.TrimSpace(parent + " " + command.Name)
		if !validCommandName(command.Name, false) || command.Description == "" {
			return fmt.Errorf("command %q has no name or description", path)
		}
		if err := validateCommandHelp(path, command.Help); err != nil {
			return err
		}
		if depth > 2 || depth == 2 && len(command.Commands) != 0 {
			return fmt.Errorf("command %q exceeds the supported depth", path)
		}
		children := make(map[string]struct{}, len(command.Commands))
		for _, child := range command.Commands {
			if _, exists := children[child.Name]; exists {
				return fmt.Errorf("command %q repeats subcommand %q", path, child.Name)
			}
			children[child.Name] = struct{}{}
			if err := validate(child, path, depth+1); err != nil {
				return err
			}
		}
		options := make(map[string]struct{}, len(command.Options)*2)
		for _, option := range command.Options {
			if !validCommandName(option.Long, false) || option.Description == "" || option.Short != "" && !validCommandName(option.Short, true) {
				return fmt.Errorf("command %q has an invalid option", path)
			}
			if option.HelpDescription != "" && option.HelpLabel == "" {
				return fmt.Errorf("command %q option %q has help text without a label", path, option.Long)
			}
			for _, name := range []string{option.Long, option.Short} {
				if name == "" {
					continue
				}
				if _, exists := options[name]; exists {
					return fmt.Errorf("command %q repeats option %q", path, name)
				}
				options[name] = struct{}{}
			}
			if option.Value > commandValueDirectory {
				return fmt.Errorf("command %q has an invalid value kind", path)
			}
			if len(option.ValueCandidates) != 0 && option.Value != commandValueWord {
				return fmt.Errorf("command %q option %q has finite values without a word value", path, option.Long)
			}
			values := make(map[string]struct{}, len(option.ValueCandidates))
			for _, candidate := range option.ValueCandidates {
				if candidate.Value == "" || candidate.Description == "" || candidate.NoSpace || strings.ContainsAny(candidate.Value, "\t\r\n ") {
					return fmt.Errorf("command %q option %q has an invalid finite value", path, option.Long)
				}
				if _, exists := values[candidate.Value]; exists {
					return fmt.Errorf("command %q option %q repeats finite value %q", path, option.Long, candidate.Value)
				}
				values[candidate.Value] = struct{}{}
			}
		}
		arguments := make(map[string]struct{}, len(command.Arguments))
		for _, argument := range command.Arguments {
			if argument.Value == "" || argument.Description == "" || strings.ContainsAny(argument.Value, "\t\r\n") || argument.NoSpace && !strings.HasSuffix(argument.Value, "=") {
				return fmt.Errorf("command %q has an invalid argument candidate", path)
			}
			if _, exists := arguments[argument.Value]; exists {
				return fmt.Errorf("command %q repeats argument %q", path, argument.Value)
			}
			arguments[argument.Value] = struct{}{}
		}
		if command.RepeatArgs && len(command.Arguments) == 0 {
			return fmt.Errorf("command %q repeats arguments but has no argument candidates", path)
		}
		for _, option := range command.Options {
			for _, unavailable := range option.UnavailableWith {
				if _, exists := arguments[unavailable]; !exists {
					return fmt.Errorf("command %q option %q refers to unknown argument %q", path, option.Long, unavailable)
				}
			}
		}
		return nil
	}
	return validate(root, "", 0)
}

func validCommandName(value string, short bool) bool {
	if value == "" || short && len(value) != 1 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || !short && character == '-' {
			continue
		}
		return false
	}
	return true
}
