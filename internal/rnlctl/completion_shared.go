package rnlctl

import "strings"

func visitCompletionCommands(root commandSpec, parent []string, visit func([]string, commandSpec)) {
	for _, command := range root.Commands {
		path := append(append([]string(nil), parent...), command.Name)
		visit(path, command)
		visitCompletionCommands(command, path, visit)
	}
}

func rootCommandCandidates(root commandSpec) []commandArgumentSpec {
	return commandCommandCandidates(root)
}

func commandCommandCandidates(command commandSpec) []commandArgumentSpec {
	candidates := make([]commandArgumentSpec, 0, len(command.Commands))
	for _, child := range command.Commands {
		candidates = append(candidates, commandArgumentSpec{Value: child.Name, Description: child.Description})
	}
	return candidates
}

func optionCandidates(options []commandOptionSpec) []commandArgumentSpec {
	candidates := make([]commandArgumentSpec, 0, len(options)*2+2)
	for _, option := range options {
		if option.Short != "" {
			candidates = append(candidates, commandArgumentSpec{Value: "-" + option.Short, Description: option.Description})
		}
		candidates = append(candidates, commandArgumentSpec{Value: "--" + option.Long, Description: option.Description})
	}
	return append(candidates,
		commandArgumentSpec{Value: "-h", Description: "Show help"},
		commandArgumentSpec{Value: "--help", Description: "Show help"},
	)
}

func combinedCompletionOptions(root, command commandSpec) []commandOptionSpec {
	options := make([]commandOptionSpec, 0, len(root.Options)+len(command.Options))
	options = append(options, root.Options...)
	return append(options, command.Options...)
}

func completionOptionPatterns(options []commandOptionSpec) []string {
	patterns := make([]string, 0, len(options)*2)
	for _, option := range options {
		if option.Short != "" {
			patterns = append(patterns, "-"+option.Short)
		}
		patterns = append(patterns, "--"+option.Long)
	}
	return patterns
}

func completionValueOptions(options []commandOptionSpec) []commandOptionSpec {
	values := make([]commandOptionSpec, 0, len(options))
	for _, option := range options {
		if option.Value != commandValueNone {
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
func completionOptionShellPatterns(options []commandOptionSpec, inline bool) []string {
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

func completionOptionCandidates(option commandOptionSpec) []commandArgumentSpec {
	return optionCandidates([]commandOptionSpec{option})
}

func hasNoSpaceCandidate(candidates []commandArgumentSpec) bool {
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
