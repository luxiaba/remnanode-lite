package rnlctl

import "strings"

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
