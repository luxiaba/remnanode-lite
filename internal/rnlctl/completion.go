package rnlctl

import "fmt"

func (a *App) runCompletion(args []string) int {
	commandUsage := usageForCommand("completion")
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, commandUsage)
	}
	if len(args) != 1 {
		return a.usageError("completion", "requires exactly one shell", commandUsage)
	}

	script, err := renderCompletion(args[0])
	if err != nil {
		return a.usageError("completion", err.Error(), commandUsage)
	}
	return a.write(a.stdout, script)
}

func renderCompletion(shell string) (string, error) {
	spec := rnlctlCommandSpec()
	if err := validateCommandSpec(spec); err != nil {
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
