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
	editableKeys := []completionCandidateSpec{
		{Value: "NODE_PORT=", Description: "Node HTTPS port", NoSpace: true},
		{Value: "NODE_BIND_ADDR=", Description: "Local bind address", NoSpace: true},
		{Value: "LOW_MEMORY=", Description: "Small-server memory profile", NoSpace: true},
		{Value: "BODY_LIMIT_MB=", Description: "Request budget in MiB", NoSpace: true},
		{Value: "GOMEMLIMIT=", Description: "Go soft memory limit", NoSpace: true},
		{Value: "DISABLE_HASHED_SET_CHECK=", Description: "Configuration hash debug switch", NoSpace: true},
	}
	getKeys := withoutCandidateSuffix(editableKeys, "=")
	unsetKeys := append([]completionCandidateSpec(nil), getKeys[1:]...)

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

func withoutCandidateSuffix(candidates []completionCandidateSpec, suffix string) []completionCandidateSpec {
	result := make([]completionCandidateSpec, len(candidates))
	for index, candidate := range candidates {
		candidate.Value = strings.TrimSuffix(candidate.Value, suffix)
		candidate.NoSpace = false
		result[index] = candidate
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

func renderBashCompletion(root completionCommandSpec) string {
	var output strings.Builder
	output.WriteString(`# Generated by rnlctl. Do not edit.
_rnlctl_completion_words() {
  local current=$1 candidate
  shift
  for candidate in "$@"; do
    if [[ $candidate == "$current"* ]]; then
      COMPREPLY+=("$candidate")
    fi
  done
}

_rnlctl_completion_path() {
  local current=$1 prefix=$2 directories=$3 candidate
  if [[ $directories == 1 ]]; then
    while IFS= read -r candidate; do
      COMPREPLY+=("${prefix}${candidate}")
    done < <(compgen -d -- "$current")
  else
    while IFS= read -r candidate; do
      COMPREPLY+=("${prefix}${candidate}")
    done < <(compgen -f -- "$current")
  fi
  compopt -o filenames 2>/dev/null || true
}

_rnlctl_completion_argument_used() {
  local wanted=${1%%=*} argument
  shift
  for argument in "$@"; do
    if [[ ${argument%%=*} == "$wanted" ]]; then
      return 0
    fi
  done
  return 1
}

_rnlctl_completion() {
  local current previous command subcommand path token
	local -a used_arguments=()
	local -i command_index=0 subcommand_index=0 argument_start=0 expect_value=0 i
  COMPREPLY=()
  current=${COMP_WORDS[COMP_CWORD]:-}
  previous=
`)
	writeBashGlobalScan(&output, root.Options)
	writeBashCandidates(&output, "    ", append(rootCommandCandidates(root), append(optionCandidates(root.Options),
		completionCandidateSpec{Value: "--version", Description: "Show version"},
	)...))
	output.WriteString("    return\n  fi\n\n  subcommand=\n")
	for _, command := range root.Commands {
		if len(command.Commands) == 0 {
			continue
		}
		fmt.Fprintf(&output, "  if [[ $command == %s ]]; then\n", shellQuote(command.Name))
		output.WriteString("    for (( i=command_index+1; i<COMP_CWORD; i++ )); do\n      token=${COMP_WORDS[i]}\n      if _rnlctl_completion_is_global_option \"$token\"; then\n        continue\n      fi\n      subcommand=$token\n      subcommand_index=$i\n      break\n    done\n    if [[ -z $subcommand ]]; then\n")
		writeBashCandidates(&output, "      ", append(commandCommandCandidates(command), optionCandidates(root.Options)...))
		output.WriteString("      return\n    fi\n  fi\n")
	}
	output.WriteString("  path=$command\n  argument_start=$((command_index + 1))\n  if [[ -n $subcommand ]]; then\n    path+=\" $subcommand\"\n    argument_start=$((subcommand_index + 1))\n  fi\n\n  case $path in\n")
	visitCompletionCommands(root, nil, func(path []string, command completionCommandSpec) {
		if len(path) == 0 || len(command.Commands) != 0 {
			return
		}
		fmt.Fprintf(&output, "    %s)\n", shellQuote(strings.Join(path, " ")))
		writeBashOptionValues(&output, command)
		writeBashArgumentScan(&output, command)
		output.WriteString("      if [[ -z $current || $current == -* ]]; then\n")
		writeBashOptionCandidates(&output, "        ", combinedCompletionOptions(root, command))
		output.WriteString("        if [[ $current == -* ]]; then\n          return\n        fi\n      fi\n")
		if len(command.Arguments) != 0 {
			writeBashArgumentCandidates(&output, "      ", command)
			if hasNoSpaceCandidate(command.Arguments) {
				output.WriteString("      compopt -o nospace 2>/dev/null || true\n")
			}
		}
		output.WriteString("      return\n      ;;\n")
	})
	output.WriteString(`  esac
}

complete -F _rnlctl_completion rnlctl
`)
	return output.String()
}

func writeBashGlobalScan(output *strings.Builder, options []completionOptionSpec) {
	output.WriteString("  _rnlctl_completion_is_global_option() {\n    case $1 in\n      ")
	output.WriteString(strings.Join(completionOptionPatterns(options), "|"))
	output.WriteString(`) return 0 ;;
      *) return 1 ;;
    esac
  }

  command=
  for (( i=1; i<COMP_CWORD; i++ )); do
    token=${COMP_WORDS[i]}
    if _rnlctl_completion_is_global_option "$token"; then
      continue
    fi
    command=$token
    command_index=$i
    break
  done
  for (( i=COMP_CWORD-1; i>0; i-- )); do
    token=${COMP_WORDS[i]}
    if _rnlctl_completion_is_global_option "$token"; then
      continue
    fi
    previous=$token
    break
  done
  if [[ -z $command ]]; then
`)
}

func writeBashOptionValues(output *strings.Builder, command completionCommandSpec) {
	var valueOptions []completionOptionSpec
	for _, option := range command.Options {
		if option.Value != completionValueNone {
			valueOptions = append(valueOptions, option)
		}
	}
	if len(valueOptions) == 0 {
		return
	}
	output.WriteString("      case $previous in\n")
	for _, option := range valueOptions {
		patterns := []string{shellQuote("--" + option.Long)}
		if option.Short != "" {
			patterns = append(patterns, shellQuote("-"+option.Short))
		}
		fmt.Fprintf(output, "        %s)\n", strings.Join(patterns, "|"))
		switch option.Value {
		case completionValueFile:
			output.WriteString("          _rnlctl_completion_path \"$current\" \"\" 0\n")
		case completionValueDirectory:
			output.WriteString("          _rnlctl_completion_path \"$current\" \"\" 1\n")
		}
		output.WriteString("          return\n          ;;\n")
	}
	output.WriteString("      esac\n      if [[ $previous == = && $COMP_CWORD -ge 2 ]]; then\n        case ${COMP_WORDS[COMP_CWORD-2]} in\n")
	for _, option := range valueOptions {
		patterns := []string{shellQuote("--" + option.Long)}
		if option.Short != "" {
			patterns = append(patterns, shellQuote("-"+option.Short))
		}
		fmt.Fprintf(output, "          %s)\n", strings.Join(patterns, "|"))
		switch option.Value {
		case completionValueFile:
			output.WriteString("            _rnlctl_completion_path \"$current\" \"\" 0\n")
		case completionValueDirectory:
			output.WriteString("            _rnlctl_completion_path \"$current\" \"\" 1\n")
		}
		output.WriteString("            return\n            ;;\n")
	}
	output.WriteString("        esac\n      fi\n      case $current in\n")
	for _, option := range valueOptions {
		long := "--" + option.Long + "="
		fmt.Fprintf(output, "        %s*)\n", shellQuote(long))
		switch option.Value {
		case completionValueFile:
			fmt.Fprintf(output, "          _rnlctl_completion_path \"${current#%s}\" %s 0\n", long, shellQuote(long))
		case completionValueDirectory:
			fmt.Fprintf(output, "          _rnlctl_completion_path \"${current#%s}\" %s 1\n", long, shellQuote(long))
		}
		output.WriteString("          return\n          ;;\n")
	}
	output.WriteString("      esac\n")
}

func writeBashArgumentScan(output *strings.Builder, command completionCommandSpec) {
	if len(command.Arguments) == 0 {
		return
	}
	valueOptions := completionValueOptions(command.Options)
	output.WriteString("      used_arguments=()\n      expect_value=0\n      for (( i=argument_start; i<COMP_CWORD; i++ )); do\n        token=${COMP_WORDS[i]}\n        if _rnlctl_completion_is_global_option \"$token\"; then\n          continue\n        fi\n        if (( expect_value )); then\n          if [[ $token == = ]]; then\n            continue\n          fi\n          expect_value=0\n          continue\n        fi\n        case $token in\n")
	if len(valueOptions) != 0 {
		fmt.Fprintf(output, "          %s)\n            expect_value=1\n            continue\n            ;;\n", strings.Join(completionOptionShellPatterns(valueOptions, false), "|"))
		fmt.Fprintf(output, "          %s)\n            continue\n            ;;\n", strings.Join(completionOptionShellPatterns(valueOptions, true), "|"))
	}
	output.WriteString("          -*)\n            continue\n            ;;\n          *)\n            used_arguments+=(\"$token\")\n            ;;\n        esac\n      done\n")
}

func writeBashOptionCandidates(output *strings.Builder, indent string, options []completionOptionSpec) {
	available := make([]completionOptionSpec, 0, len(options))
	for _, option := range options {
		if len(option.UnavailableWith) == 0 {
			available = append(available, option)
		}
	}
	writeBashCandidates(output, indent, optionCandidates(available))
	for _, option := range options {
		if len(option.UnavailableWith) == 0 {
			continue
		}
		fmt.Fprintf(output, "%sif %s; then\n", indent, bashOptionAvailableCondition(option))
		writeBashCandidates(output, indent+"  ", completionOptionCandidates(option))
		output.WriteString(indent + "fi\n")
	}
}

func writeBashArgumentCandidates(output *strings.Builder, indent string, command completionCommandSpec) {
	if !command.RepeatArgs {
		fmt.Fprintf(output, "%sif (( ${#used_arguments[@]} == 0 )); then\n", indent)
		writeBashCandidates(output, indent+"  ", command.Arguments)
		output.WriteString(indent + "fi\n")
		return
	}
	for _, candidate := range command.Arguments {
		fmt.Fprintf(output, "%sif ! _rnlctl_completion_argument_used %s \"${used_arguments[@]}\"; then\n", indent, shellQuote(candidate.Value))
		writeBashCandidates(output, indent+"  ", []completionCandidateSpec{candidate})
		output.WriteString(indent + "fi\n")
	}
}

func bashOptionAvailableCondition(option completionOptionSpec) string {
	conditions := make([]string, 0, len(option.UnavailableWith))
	for _, argument := range option.UnavailableWith {
		conditions = append(conditions, "! _rnlctl_completion_argument_used "+shellQuote(argument)+" \"${used_arguments[@]}\"")
	}
	return strings.Join(conditions, " && ")
}

func writeBashCandidates(output *strings.Builder, indent string, candidates []completionCandidateSpec) {
	output.WriteString(indent + "_rnlctl_completion_words \"$current\"")
	for _, candidate := range candidates {
		output.WriteByte(' ')
		output.WriteString(shellQuote(candidate.Value))
	}
	output.WriteByte('\n')
}

func renderZshCompletion(root completionCommandSpec) string {
	var output strings.Builder
	output.WriteString(`#compdef rnlctl
compdef _rnlctl rnlctl

# Generated by rnlctl. Do not edit.
_rnlctl_completion_argument_used() {
  local wanted=${1%%=*} argument
  shift
  for argument in "$@"; do
    if [[ ${argument%%=*} == "$wanted" ]]; then
      return 0
    fi
  done
  return 1
}

_rnlctl() {
  local current previous command subcommand command_path token
	local -a used_arguments
	integer command_index=0 subcommand_index=0 argument_start=0 expect_value=0 i
  current=${words[CURRENT]:-}
  previous=
`)
	writeZshGlobalScan(&output, root.Options)
	writeZshDescribe(&output, "    ", "commands", "rnlctl command", append(rootCommandCandidates(root), append(optionCandidates(root.Options),
		completionCandidateSpec{Value: "--version", Description: "Show version"},
	)...))
	output.WriteString("    return\n  fi\n\n  subcommand=\n")
	for _, command := range root.Commands {
		if len(command.Commands) == 0 {
			continue
		}
		fmt.Fprintf(&output, "  if [[ $command == %s ]]; then\n", shellQuote(command.Name))
		output.WriteString("    for (( i=command_index+1; i<CURRENT; i++ )); do\n      token=${words[i]}\n      if _rnlctl_completion_is_global_option \"$token\"; then\n        continue\n      fi\n      subcommand=$token\n      subcommand_index=$i\n      break\n    done\n    if [[ -z $subcommand ]]; then\n")
		writeZshDescribe(&output, "      ", "commands", command.Name+" command", append(commandCommandCandidates(command), optionCandidates(root.Options)...))
		output.WriteString("      return\n    fi\n  fi\n")
	}
	output.WriteString("  command_path=$command\n  argument_start=$((command_index + 1))\n  if [[ -n $subcommand ]]; then\n    command_path+=\" $subcommand\"\n    argument_start=$((subcommand_index + 1))\n  fi\n\n  case $command_path in\n")
	visitCompletionCommands(root, nil, func(path []string, command completionCommandSpec) {
		if len(path) == 0 || len(command.Commands) != 0 {
			return
		}
		fmt.Fprintf(&output, "    %s)\n", shellQuote(strings.Join(path, " ")))
		writeZshOptionValues(&output, command)
		writeZshArgumentScan(&output, command)
		output.WriteString("      if [[ -z $current || $current == -* ]]; then\n")
		writeZshOptionCandidates(&output, "        ", combinedCompletionOptions(root, command))
		output.WriteString("        if [[ $current == -* ]]; then\n          return\n        fi\n      fi\n")
		if len(command.Arguments) != 0 {
			writeZshArgumentCandidates(&output, "      ", command)
		}
		output.WriteString("      return\n      ;;\n")
	})
	output.WriteString(`  esac
}

# When loaded through fpath, zsh initially executes this file as _rnlctl.
# When sourced directly, compdef above is sufficient and no completion runs.
if [[ ${funcstack[1]:-} == _rnlctl ]]; then
  _rnlctl "$@"
fi
`)
	return output.String()
}

func writeZshGlobalScan(output *strings.Builder, options []completionOptionSpec) {
	output.WriteString("  _rnlctl_completion_is_global_option() {\n    case $1 in\n      ")
	output.WriteString(strings.Join(completionOptionPatterns(options), "|"))
	output.WriteString(`) return 0 ;;
      *) return 1 ;;
    esac
  }

  command=
  for (( i=2; i<CURRENT; i++ )); do
    token=${words[i]}
    if _rnlctl_completion_is_global_option "$token"; then
      continue
    fi
    command=$token
    command_index=$i
    break
  done
  for (( i=CURRENT-1; i>1; i-- )); do
    token=${words[i]}
    if _rnlctl_completion_is_global_option "$token"; then
      continue
    fi
    previous=$token
    break
  done
  if [[ -z $command ]]; then
`)
}

func writeZshOptionValues(output *strings.Builder, command completionCommandSpec) {
	var valueOptions []completionOptionSpec
	for _, option := range command.Options {
		if option.Value != completionValueNone {
			valueOptions = append(valueOptions, option)
		}
	}
	if len(valueOptions) == 0 {
		return
	}
	output.WriteString("      case $previous in\n")
	for _, option := range valueOptions {
		patterns := []string{shellQuote("--" + option.Long)}
		if option.Short != "" {
			patterns = append(patterns, shellQuote("-"+option.Short))
		}
		fmt.Fprintf(output, "        %s)\n", strings.Join(patterns, "|"))
		switch option.Value {
		case completionValueFile:
			output.WriteString("          _files\n")
		case completionValueDirectory:
			output.WriteString("          _files -/\n")
		}
		output.WriteString("          return\n          ;;\n")
	}
	output.WriteString("      esac\n")
	for _, option := range valueOptions {
		long := "--" + option.Long + "="
		fmt.Fprintf(output, "      if [[ $current == %s* ]]; then\n", shellQuote(long))
		if option.Value == completionValueFile || option.Value == completionValueDirectory {
			output.WriteString("        compset -P '*='\n")
			if option.Value == completionValueDirectory {
				output.WriteString("        _files -/\n")
			} else {
				output.WriteString("        _files\n")
			}
		}
		output.WriteString("        return\n      fi\n")
	}
}

func writeZshArgumentScan(output *strings.Builder, command completionCommandSpec) {
	if len(command.Arguments) == 0 {
		return
	}
	valueOptions := completionValueOptions(command.Options)
	output.WriteString("      used_arguments=()\n      expect_value=0\n      for (( i=argument_start; i<CURRENT; i++ )); do\n        token=${words[i]}\n        if _rnlctl_completion_is_global_option \"$token\"; then\n          continue\n        fi\n        if (( expect_value )); then\n          expect_value=0\n          continue\n        fi\n        case $token in\n")
	if len(valueOptions) != 0 {
		fmt.Fprintf(output, "          %s)\n            expect_value=1\n            continue\n            ;;\n", strings.Join(completionOptionShellPatterns(valueOptions, false), "|"))
		fmt.Fprintf(output, "          %s)\n            continue\n            ;;\n", strings.Join(completionOptionShellPatterns(valueOptions, true), "|"))
	}
	output.WriteString("          -*)\n            continue\n            ;;\n          *)\n            used_arguments+=(\"$token\")\n            ;;\n        esac\n      done\n")
}

func writeZshOptionCandidates(output *strings.Builder, indent string, options []completionOptionSpec) {
	available := make([]completionOptionSpec, 0, len(options))
	conditional := make([]completionOptionSpec, 0, len(options))
	for _, option := range options {
		if len(option.UnavailableWith) == 0 {
			available = append(available, option)
		} else {
			conditional = append(conditional, option)
		}
	}
	output.WriteString(indent + "local -a candidates\n")
	output.WriteString(indent + "candidates=(\n")
	for _, candidate := range optionCandidates(available) {
		fmt.Fprintf(output, "%s  %s\n", indent, shellQuote(candidate.Value+":"+candidate.Description))
	}
	output.WriteString(indent + ")\n")
	for _, option := range conditional {
		fmt.Fprintf(output, "%sif %s; then\n", indent, zshOptionAvailableCondition(option))
		for _, candidate := range completionOptionCandidates(option) {
			fmt.Fprintf(output, "%s  candidates+=(%s)\n", indent, shellQuote(candidate.Value+":"+candidate.Description))
		}
		output.WriteString(indent + "fi\n")
	}
	fmt.Fprintf(output, "%s_describe -t options %s candidates\n", indent, shellQuote("option"))
}

func writeZshArgumentCandidates(output *strings.Builder, indent string, command completionCommandSpec) {
	if !command.RepeatArgs {
		fmt.Fprintf(output, "%sif (( ${#used_arguments} == 0 )); then\n", indent)
		writeZshCandidateValues(output, indent+"  ", command.Arguments)
		output.WriteString(indent + "fi\n")
		return
	}
	if hasNoSpaceCandidate(command.Arguments) {
		for _, candidate := range command.Arguments {
			fmt.Fprintf(output, "%sif ! _rnlctl_completion_argument_used %s \"${used_arguments[@]}\"; then\n", indent, shellQuote(candidate.Value))
			fmt.Fprintf(output, "%s  compadd -S '' -- %s\n", indent, shellQuote(candidate.Value))
			output.WriteString(indent + "fi\n")
		}
		return
	}
	output.WriteString(indent + "local -a candidates\n")
	output.WriteString(indent + "candidates=()\n")
	for _, candidate := range command.Arguments {
		fmt.Fprintf(output, "%sif ! _rnlctl_completion_argument_used %s \"${used_arguments[@]}\"; then\n", indent, shellQuote(candidate.Value))
		fmt.Fprintf(output, "%s  candidates+=(%s)\n", indent, shellQuote(candidate.Value+":"+candidate.Description))
		output.WriteString(indent + "fi\n")
	}
	output.WriteString(indent + "(( ${#candidates} > 0 )) && _describe -t values 'value' candidates\n")
}

func writeZshCandidateValues(output *strings.Builder, indent string, candidates []completionCandidateSpec) {
	if hasNoSpaceCandidate(candidates) {
		output.WriteString(indent + "compadd -S '' --")
		for _, candidate := range candidates {
			output.WriteByte(' ')
			output.WriteString(shellQuote(candidate.Value))
		}
		output.WriteByte('\n')
		return
	}
	writeZshDescribe(output, indent, "values", "value", candidates)
}

func zshOptionAvailableCondition(option completionOptionSpec) string {
	conditions := make([]string, 0, len(option.UnavailableWith))
	for _, argument := range option.UnavailableWith {
		conditions = append(conditions, "! _rnlctl_completion_argument_used "+shellQuote(argument)+" \"${used_arguments[@]}\"")
	}
	return strings.Join(conditions, " && ")
}

func writeZshDescribe(output *strings.Builder, indent, tag, label string, candidates []completionCandidateSpec) {
	output.WriteString(indent + "local -a candidates\n")
	output.WriteString(indent + "candidates=(\n")
	for _, candidate := range candidates {
		fmt.Fprintf(output, "%s  %s\n", indent, shellQuote(candidate.Value+":"+candidate.Description))
	}
	output.WriteString(indent + ")\n")
	fmt.Fprintf(output, "%s_describe -t %s %s candidates\n", indent, tag, shellQuote(label))
}

func renderFishCompletion(root completionCommandSpec) string {
	var output strings.Builder
	output.WriteString("# Generated by rnlctl. Do not edit.\n")
	writeFishHelpers(&output, root)
	output.WriteString("complete -c rnlctl -f\n")
	for _, command := range root.Commands {
		fmt.Fprintf(&output, "complete -c rnlctl -n %s -a %s -d %s\n",
			shellQuote(fishRootCommandCondition()),
			shellQuote(command.Name), shellQuote(command.Description))
	}
	fmt.Fprintf(&output, "complete -c rnlctl -n %s -s h -l help -d 'Show help'\n", shellQuote(fishRootCommandCondition()))
	fmt.Fprintf(&output, "complete -c rnlctl -n %s -l version -d 'Show version'\n", shellQuote(fishRootCommandCondition()))
	for _, option := range root.Options {
		fmt.Fprintf(&output, "complete -c rnlctl")
		if option.Short != "" {
			fmt.Fprintf(&output, " -s %s", shellQuote(option.Short))
		}
		fmt.Fprintf(&output, " -l %s -d %s\n", shellQuote(option.Long), shellQuote(option.Description))
	}
	for _, parent := range root.Commands {
		if len(parent.Commands) == 0 {
			continue
		}
		condition := fishSubcommandCondition(parent)
		for _, command := range parent.Commands {
			fmt.Fprintf(&output, "complete -c rnlctl -n %s -a %s -d %s\n",
				shellQuote(condition), shellQuote(command.Name), shellQuote(command.Description))
		}
	}
	visitCompletionCommands(root, nil, func(path []string, command completionCommandSpec) {
		if len(path) == 0 || len(command.Commands) != 0 {
			return
		}
		for _, option := range command.Options {
			fmt.Fprintf(&output, "complete -c rnlctl -n %s", shellQuote(fishOptionCondition(path, option)))
			if option.Short != "" {
				fmt.Fprintf(&output, " -s %s", shellQuote(option.Short))
			}
			fmt.Fprintf(&output, " -l %s", shellQuote(option.Long))
			if option.Value != completionValueNone {
				output.WriteString(" -r")
			}
			switch option.Value {
			case completionValueFile:
				output.WriteString(" -F")
			case completionValueDirectory:
				output.WriteString(" -a '(__fish_complete_directories)'")
			}
			fmt.Fprintf(&output, " -d %s\n", shellQuote(option.Description))
		}
		for _, candidate := range command.Arguments {
			fmt.Fprintf(&output, "complete -c rnlctl -n %s -a %s -d %s\n",
				shellQuote(fishArgumentCondition(path, command, candidate)), shellQuote(candidate.Value), shellQuote(candidate.Description))
		}
	})
	output.WriteString("complete -c rnlctl -s h -l help -d 'Show help'\n")
	return output.String()
}

func fishSubcommandCondition(parent completionCommandSpec) string {
	return fishPathCondition([]string{parent.Name})
}

func fishPathCondition(path []string) string {
	return "__rnlctl_completion_path_is " + shellQuote(strings.Join(path, " "))
}

func fishRootCommandCondition() string {
	return "__rnlctl_completion_path_is ''"
}

func fishOptionCondition(path []string, option completionOptionSpec) string {
	condition := fishPathCondition(path)
	if len(option.UnavailableWith) == 0 {
		return condition
	}

	arguments := make([]string, 0, len(option.UnavailableWith)+1)
	arguments = append(arguments, shellQuote(strings.Join(path, " ")))
	for _, unavailable := range option.UnavailableWith {
		arguments = append(arguments, shellQuote(unavailable))
	}
	return condition + "; and __rnlctl_completion_option_available " + strings.Join(arguments, " ")
}

func fishArgumentCondition(path []string, command completionCommandSpec, candidate completionCandidateSpec) string {
	condition := fishPathCondition(path)
	if command.RepeatArgs {
		return condition + "; and not __rnlctl_completion_argument_used " + shellQuote(candidate.Value)
	}
	if strings.Join(path, " ") == "logs" {
		return condition + "; and not __rnlctl_completion_logs_source_is_set"
	}

	arguments := make([]string, 0, len(command.Arguments))
	for _, argument := range command.Arguments {
		arguments = append(arguments, shellQuote(argument.Value))
	}
	return condition + "; and not __rnlctl_completion_any_argument_used " + strings.Join(arguments, " ")
}

func writeFishHelpers(output *strings.Builder, root completionCommandSpec) {
	parents := make([]string, 0, len(root.Commands))
	for _, command := range root.Commands {
		if len(command.Commands) != 0 {
			parents = append(parents, command.Name)
		}
	}

	output.WriteString(`function __rnlctl_completion_path
  set -l tokens (commandline -opc)
  set -l command
  set -l subcommand
  for token in $tokens[2..-1]
    switch $token
      case `)
	output.WriteString(strings.Join(completionOptionPatterns(root.Options), " "))
	output.WriteString(`
        continue
    end
    if test -z "$command"
      set command $token
      continue
    end
    if test -z "$subcommand"
      switch $command
        case `)
	output.WriteString(strings.Join(parents, " "))
	output.WriteString(`
          set subcommand $token
      end
    end
  end
  if test -n "$subcommand"
    printf '%s %s\n' "$command" "$subcommand"
  else
    printf '%s\n' "$command"
  end
end

function __rnlctl_completion_path_is
  set -l path (__rnlctl_completion_path)
  test "$path" = "$argv[1]"
end

function __rnlctl_completion_argument_used
  set -l wanted (string replace -r '=.*$' '' -- $argv[1])
  for token in (commandline -opc)
    set -l actual (string replace -r '=.*$' '' -- $token)
    if test "$actual" = "$wanted"
      return 0
    end
  end
  return 1
end

function __rnlctl_completion_any_argument_used
  for wanted in $argv
    if __rnlctl_completion_argument_used "$wanted"
      return 0
    end
  end
  return 1
end

function __rnlctl_completion_logs_source
  set -l after_logs 0
  set -l consumes_value 0
  for token in (commandline -opc)
    if test $after_logs -eq 0
      if test "$token" = logs
        set after_logs 1
      end
      continue
    end
    switch $token
      case --quiet -q --no-color
        continue
      case --lines -n --since
        set consumes_value 1
        continue
      case '-*'
        continue
    end
    if test $consumes_value -eq 1
      set consumes_value 0
      continue
    end
    printf '%s\n' "$token"
    return 0
  end
  return 1
end

function __rnlctl_completion_logs_source_is_set
  set -l source (__rnlctl_completion_logs_source)
  test -n "$source"
end

function __rnlctl_completion_option_available
  set -l path $argv[1]
  set -e argv[1]
  if test "$path" = logs
    set -l source (__rnlctl_completion_logs_source)
    contains -- "$source" $argv
    and return 1
    return 0
  end
  not __rnlctl_completion_any_argument_used $argv
end

`)
}

func visitCompletionCommands(root completionCommandSpec, parent []string, visit func([]string, completionCommandSpec)) {
	for _, command := range root.Commands {
		path := append(append([]string(nil), parent...), command.Name)
		visit(path, command)
		visitCompletionCommands(command, path, visit)
	}
}

func rootCommandCandidates(root completionCommandSpec) []completionCandidateSpec {
	return commandCommandCandidates(root)
}

func commandCommandCandidates(command completionCommandSpec) []completionCandidateSpec {
	candidates := make([]completionCandidateSpec, 0, len(command.Commands))
	for _, child := range command.Commands {
		candidates = append(candidates, completionCandidateSpec{Value: child.Name, Description: child.Description})
	}
	return candidates
}

func optionCandidates(options []completionOptionSpec) []completionCandidateSpec {
	candidates := make([]completionCandidateSpec, 0, len(options)*2+2)
	for _, option := range options {
		if option.Short != "" {
			candidates = append(candidates, completionCandidateSpec{Value: "-" + option.Short, Description: option.Description})
		}
		candidates = append(candidates, completionCandidateSpec{Value: "--" + option.Long, Description: option.Description})
	}
	return append(candidates,
		completionCandidateSpec{Value: "-h", Description: "Show help"},
		completionCandidateSpec{Value: "--help", Description: "Show help"},
	)
}

func combinedCompletionOptions(root, command completionCommandSpec) []completionOptionSpec {
	options := make([]completionOptionSpec, 0, len(root.Options)+len(command.Options))
	options = append(options, root.Options...)
	return append(options, command.Options...)
}

func completionOptionPatterns(options []completionOptionSpec) []string {
	patterns := make([]string, 0, len(options)*2)
	for _, option := range options {
		if option.Short != "" {
			patterns = append(patterns, "-"+option.Short)
		}
		patterns = append(patterns, "--"+option.Long)
	}
	return patterns
}

func completionValueOptions(options []completionOptionSpec) []completionOptionSpec {
	values := make([]completionOptionSpec, 0, len(options))
	for _, option := range options {
		if option.Value != completionValueNone {
			values = append(values, option)
		}
	}
	return values
}

// completionOptionShellPatterns returns shell case patterns for options that
// consume a value. The inline form deliberately matches only --long=value and
// -s=value. Go's flag package also accepts those forms, while a bare option is
// handled separately so its following word is not mistaken for a positional
// argument.
func completionOptionShellPatterns(options []completionOptionSpec, inline bool) []string {
	patterns := make([]string, 0, len(options)*2)
	for _, option := range options {
		long := "--" + option.Long
		if inline {
			patterns = append(patterns, shellQuote(long+"=")+"*")
		} else {
			patterns = append(patterns, shellQuote(long))
		}
		if option.Short != "" {
			short := "-" + option.Short
			if inline {
				patterns = append(patterns, shellQuote(short+"=")+"*")
			} else {
				patterns = append(patterns, shellQuote(short))
			}
		}
	}
	return patterns
}

func completionOptionCandidates(option completionOptionSpec) []completionCandidateSpec {
	return optionCandidates([]completionOptionSpec{option})
}

func hasNoSpaceCandidate(candidates []completionCandidateSpec) bool {
	for _, candidate := range candidates {
		if candidate.NoSpace {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
