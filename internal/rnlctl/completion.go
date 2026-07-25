package rnlctl

import (
	"fmt"
	"strings"
)

const completionUsage = `Usage: rnlctl completion <bash|zsh|fish>

Print a shell completion script to standard output. The command does not
install files or modify shell startup configuration.
`

type completionValueKind uint8

const (
	completionValueNone completionValueKind = iota
	completionValueWord
	completionValueFile
	completionValueDirectory
)

type completionCandidateSpec struct {
	Value       string
	Description string
	NoSpace     bool
}

type completionOptionSpec struct {
	Long            string
	Short           string
	Description     string
	Value           completionValueKind
	UnavailableWith []string
}

type completionCommandSpec struct {
	Name        string
	Description string
	Options     []completionOptionSpec
	Arguments   []completionCandidateSpec
	RepeatArgs  bool
	Commands    []completionCommandSpec
}

func (a *App) runCompletion(args []string) int {
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, completionUsage)
	}
	if len(args) != 1 {
		return a.usageError("completion", "requires exactly one shell", completionUsage)
	}

	script, err := renderCompletion(args[0])
	if err != nil {
		return a.usageError("completion", err.Error(), completionUsage)
	}
	return a.write(a.stdout, script)
}

func renderCompletion(shell string) (string, error) {
	spec := rnlctlCompletionSpec()
	if err := validateCompletionSpec(spec); err != nil {
		return "", fmt.Errorf("invalid completion specification: %w", err)
	}
	switch shell {
	case "bash":
		return renderBashCompletion(spec), nil
	case "zsh":
		return renderZshCompletion(spec), nil
	case "fish":
		return renderFishCompletion(spec), nil
	default:
		return "", fmt.Errorf("unsupported completion shell %q", shell)
	}
}

func rnlctlCompletionSpec() completionCommandSpec {
	editableKeys := editableConfigurationCompletionCandidates(true, false)
	getKeys := editableConfigurationCompletionCandidates(false, false)
	unsetKeys := editableConfigurationCompletionCandidates(false, true)

	bundleOptions := []completionOptionSpec{
		{Long: "bundle-root", Description: "Extracted Native bundle directory", Value: completionValueDirectory},
		{Long: "bundle", Description: "Native bundle archive", Value: completionValueFile},
		{Long: "sha256", Description: "Expected bundle SHA-256", Value: completionValueWord},
		{Long: "expected-version", Description: "Required bundle version", Value: completionValueWord},
	}

	return completionCommandSpec{
		Name:        "rnlctl",
		Description: "Remnanode Lite Native administration CLI",
		Options: []completionOptionSpec{
			{Long: "quiet", Short: "q", Description: "Hide successful output"},
			{Long: "no-color", Description: "Disable terminal colors"},
		},
		Commands: []completionCommandSpec{
			{Name: "version", Description: "Show the rnlctl version"},
			{
				Name: "install", Description: "Install one verified Native bundle",
				Options: append(cloneCompletionOptions(bundleOptions),
					completionOptionSpec{Long: "port", Description: "Node HTTPS port", Value: completionValueWord},
					completionOptionSpec{Long: "secret-file", Description: "Panel Secret file", Value: completionValueFile},
					completionOptionSpec{Long: "prepare-only", Description: "Install stopped and disabled"},
				),
			},
			{
				Name: "activate", Description: "Activate a prepared installation",
				Options: []completionOptionSpec{{Long: "secret-file", Description: "Panel Secret file", Value: completionValueFile}},
			},
			{
				Name: "upgrade", Description: "Upgrade to one complete generation",
				Options: append(cloneCompletionOptions(bundleOptions),
					completionOptionSpec{Long: "to", Description: "Exact published version", Value: completionValueWord},
					completionOptionSpec{Long: "dry-run", Description: "Run upgrade preflight without changing the host"},
					completionOptionSpec{Long: "json", Description: "Emit machine-readable preflight JSON"},
				),
			},
			{
				Name: "rollback", Description: "Roll back to the retained generation",
				Options: []completionOptionSpec{{Long: "to", Description: "Retained generation ID", Value: completionValueWord}},
			},
			{Name: "repair", Description: "Recover the committed generation", Options: cloneCompletionOptions(bundleOptions)},
			{
				Name: "uninstall", Description: "Remove the Native installation",
				Options: []completionOptionSpec{
					{Long: "purge", Description: "Remove retained state and owned account"},
					{Long: "yes", Description: "Confirm destructive purge"},
				},
			},
			{
				Name: "config", Description: "Inspect or change Native Node configuration",
				Commands: []completionCommandSpec{
					{Name: "show", Description: "Show administrator-controlled values", Options: jsonCompletionOption()},
					{Name: "get", Description: "Print one administrator-controlled value", Arguments: getKeys},
					{
						Name: "set", Description: "Set and validate one or more values",
						Options:    []completionOptionSpec{{Long: "apply", Description: "Restart and verify the active service"}},
						Arguments:  editableKeys,
						RepeatArgs: true,
					},
					{
						Name: "unset", Description: "Remove one or more optional values",
						Options:    []completionOptionSpec{{Long: "apply", Description: "Restart and verify the active service"}},
						Arguments:  unsetKeys,
						RepeatArgs: true,
					},
					{Name: "check", Description: "Validate the installed Native configuration"},
					{Name: "apply", Description: "Restart the active service and verify health"},
				},
			},
			{
				Name: "secret", Description: "Manage the Panel Secret",
				Commands: []completionCommandSpec{
					{
						Name: "set", Description: "Replace the managed Panel Secret",
						Options: []completionOptionSpec{
							{Long: "file", Description: "File containing the new Secret", Value: completionValueFile},
							{Long: "apply", Description: "Restart and verify the active service"},
						},
					},
				},
			},
			{Name: "status", Description: "Show service or lifecycle status", Options: jsonCompletionOption()},
			{Name: "doctor", Description: "Run deployment diagnostics", Options: jsonCompletionOption()},
			{Name: "start", Description: "Start the service"},
			{Name: "stop", Description: "Stop the service"},
			{Name: "restart", Description: "Restart the service"},
			{
				Name: "logs", Description: "Show Node or core logs",
				Options: []completionOptionSpec{
					{Long: "follow", Short: "f", Description: "Follow new log entries"},
					{Long: "lines", Short: "n", Description: "Number of recent lines", Value: completionValueWord},
					{
						Long: "since", Description: "Show entries from a recent duration such as 15m",
						Value: completionValueWord, UnavailableWith: []string{"core", "core-errors"},
					},
				},
				Arguments: []completionCandidateSpec{
					{Value: "node", Description: "remnanode-lite service output"},
					{Value: "core", Description: "rw-core standard output"},
					{Value: "core-errors", Description: "rw-core standard error"},
				},
			},
			{
				Name: "completion", Description: "Print a shell completion script",
				Arguments: []completionCandidateSpec{
					{Value: "bash", Description: "Bash completion"},
					{Value: "zsh", Description: "Zsh completion"},
					{Value: "fish", Description: "Fish completion"},
				},
			},
			{Name: "help", Description: "Show rnlctl help"},
		},
	}
}

func cloneCompletionOptions(options []completionOptionSpec) []completionOptionSpec {
	return append([]completionOptionSpec(nil), options...)
}

func jsonCompletionOption() []completionOptionSpec {
	return []completionOptionSpec{{Long: "json", Description: "Emit machine-readable JSON"}}
}

func editableConfigurationCompletionCandidates(assignments, optionalOnly bool) []completionCandidateSpec {
	result := make([]completionCandidateSpec, 0, len(editableConfigurationKeySpecs))
	for _, spec := range editableConfigurationKeySpecs {
		if optionalOnly && !spec.Optional {
			continue
		}
		value := spec.Name
		if assignments {
			value += "="
		}
		result = append(result, completionCandidateSpec{
			Value:       value,
			Description: spec.Description,
			NoSpace:     assignments,
		})
	}
	return result
}

func validateCompletionSpec(root completionCommandSpec) error {
	if root.Name != "rnlctl" || len(root.Commands) == 0 {
		return fmt.Errorf("root command is incomplete")
	}
	for _, option := range root.Options {
		if option.Value != completionValueNone {
			return fmt.Errorf("global completion option %q cannot accept a value", option.Long)
		}
	}
	var validate func(completionCommandSpec, string, int) error
	validate = func(command completionCommandSpec, parent string, depth int) error {
		path := strings.TrimSpace(parent + " " + command.Name)
		if !validCompletionName(command.Name, false) || command.Description == "" {
			return fmt.Errorf("command %q has no name or description", path)
		}
		if depth > 2 || depth == 2 && len(command.Commands) != 0 {
			return fmt.Errorf("command %q exceeds the supported completion depth", path)
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
			if !validCompletionName(option.Long, false) || option.Description == "" || option.Short != "" && !validCompletionName(option.Short, true) {
				return fmt.Errorf("command %q has an invalid option", path)
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
			if option.Value > completionValueDirectory {
				return fmt.Errorf("command %q has an invalid value kind", path)
			}
		}
		arguments := make(map[string]struct{}, len(command.Arguments))
		for _, candidate := range command.Arguments {
			if candidate.Value == "" || candidate.Description == "" || strings.ContainsAny(candidate.Value, "\t\r\n") || candidate.NoSpace && !strings.HasSuffix(candidate.Value, "=") {
				return fmt.Errorf("command %q has an invalid argument candidate", path)
			}
			if _, exists := arguments[candidate.Value]; exists {
				return fmt.Errorf("command %q repeats argument %q", path, candidate.Value)
			}
			arguments[candidate.Value] = struct{}{}
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

func validCompletionName(value string, short bool) bool {
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
