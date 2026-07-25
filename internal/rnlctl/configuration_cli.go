package rnlctl

import (
	"context"
	"fmt"
	"strings"
)

const configUsage = `Usage: rnlctl config <show|get|set|unset|check|apply>

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

const secretUsage = `Usage: rnlctl secret set --file PATH [--apply]

The Secret is read from a bounded regular file and is never accepted as a value
argument or written to command output.
`

func (a *App) runConfig(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.usageError("config", "a command is required", configUsage)
	}
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, configUsage)
	}
	switch args[0] {
	case "show":
		if len(args) == 2 && isHelp(args[1]) {
			return a.write(a.stdout, "Usage: rnlctl config show [--json]\n")
		}
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return a.usageError("config show", "accepts only --json", "Usage: rnlctl config show [--json]\n")
		}
		configuration, err := a.lifecycle.ReadConfiguration(ctx)
		if err != nil {
			return a.runtimeError("config show", err)
		}
		if len(args) == 2 {
			if err := a.writeJSON(configuration); err != nil {
				return a.runtimeError("config show", err)
			}
			return exitOK
		}
		for _, key := range editableConfigurationOrder() {
			if code := a.write(a.stdout, key+"="+configuration.Values[key]+"\n"); code != exitOK {
				return code
			}
		}
		return exitOK
	case "get":
		if len(args) == 2 && isHelp(args[1]) {
			return a.write(a.stdout, "Usage: rnlctl config get KEY\n")
		}
		if len(args) != 2 || !isEditableConfigurationKey(args[1]) {
			return a.usageError("config get", "requires one administrator-editable key", "Usage: rnlctl config get KEY\n")
		}
		configuration, err := a.lifecycle.ReadConfiguration(ctx)
		if err != nil {
			return a.runtimeError("config get", err)
		}
		return a.write(a.stdout, configuration.Values[args[1]]+"\n")
	case "set":
		request, showHelp, err := parseConfigurationSetArgs(args[1:])
		if showHelp {
			return a.write(a.stdout, "Usage: rnlctl config set KEY=VALUE... [--apply]\n")
		}
		if err != nil {
			return a.usageError("config set", err.Error(), "Usage: rnlctl config set KEY=VALUE... [--apply]\n")
		}
		result, err := a.lifecycle.UpdateConfiguration(ctx, request)
		return a.lifecycleResult("config set", result, err)
	case "unset":
		request, showHelp, err := parseConfigurationUnsetArgs(args[1:])
		if showHelp {
			return a.write(a.stdout, "Usage: rnlctl config unset KEY... [--apply]\n")
		}
		if err != nil {
			return a.usageError("config unset", err.Error(), "Usage: rnlctl config unset KEY... [--apply]\n")
		}
		result, err := a.lifecycle.UpdateConfiguration(ctx, request)
		return a.lifecycleResult("config unset", result, err)
	case "check":
		if len(args) == 2 && isHelp(args[1]) {
			return a.write(a.stdout, "Usage: rnlctl config check\n")
		}
		if len(args) != 1 {
			return a.usageError("config check", "does not accept arguments", "Usage: rnlctl config check\n")
		}
		if err := a.lifecycle.CheckConfiguration(ctx); err != nil {
			return a.runtimeError("config check", err)
		}
		if a.quiet {
			return exitOK
		}
		return a.write(a.stdout, "configuration ok\n")
	case "apply":
		if len(args) == 2 && isHelp(args[1]) {
			return a.write(a.stdout, "Usage: rnlctl config apply\n")
		}
		if len(args) != 1 {
			return a.usageError("config apply", "does not accept arguments", "Usage: rnlctl config apply\n")
		}
		result, err := a.lifecycle.ApplyConfiguration(ctx)
		return a.lifecycleResult("config apply", result, err)
	default:
		return a.usageError("config", fmt.Sprintf("unknown command %q", args[0]), configUsage)
	}
}

func (a *App) runSecret(ctx context.Context, args []string) int {
	if len(args) == 1 && isHelp(args[0]) {
		return a.write(a.stdout, secretUsage)
	}
	if len(args) == 0 || args[0] != "set" {
		return a.usageError("secret", "supports only the set command", secretUsage)
	}
	flags := a.flagSet("secret set", secretUsage)
	request := SecretUpdateRequest{}
	flags.StringVar(&request.File, "file", "", "")
	flags.BoolVar(&request.Apply, "apply", false, "")
	if code, ok := a.parseFlags(flags, args[1:], secretUsage); !ok {
		return code
	}
	if strings.TrimSpace(request.File) == "" {
		return a.usageError("secret set", "--file is required", secretUsage)
	}
	result, err := a.lifecycle.SetSecret(ctx, request)
	return a.lifecycleResult("secret set", result, err)
}

func parseConfigurationSetArgs(args []string) (ConfigurationUpdateRequest, bool, error) {
	request := ConfigurationUpdateRequest{Set: make(map[string]string)}
	for _, argument := range args {
		switch {
		case isHelp(argument):
			return ConfigurationUpdateRequest{}, true, nil
		case argument == "--apply":
			if request.Apply {
				return ConfigurationUpdateRequest{}, false, fmt.Errorf("option --apply may be specified only once")
			}
			request.Apply = true
		case strings.HasPrefix(argument, "--"):
			return ConfigurationUpdateRequest{}, false, fmt.Errorf("unknown option %s", argument)
		default:
			parts := strings.SplitN(argument, "=", 2)
			if len(parts) != 2 || !isEditableConfigurationKey(parts[0]) {
				return ConfigurationUpdateRequest{}, false, fmt.Errorf("assignment %q must use an administrator-editable KEY=VALUE", argument)
			}
			if _, duplicate := request.Set[parts[0]]; duplicate {
				return ConfigurationUpdateRequest{}, false, fmt.Errorf("configuration key %s is repeated", parts[0])
			}
			request.Set[parts[0]] = parts[1]
		}
	}
	if len(request.Set) == 0 {
		return ConfigurationUpdateRequest{}, false, fmt.Errorf("at least one KEY=VALUE assignment is required")
	}
	return request, false, nil
}

func parseConfigurationUnsetArgs(args []string) (ConfigurationUpdateRequest, bool, error) {
	request := ConfigurationUpdateRequest{}
	seen := make(map[string]struct{})
	for _, argument := range args {
		switch {
		case isHelp(argument):
			return ConfigurationUpdateRequest{}, true, nil
		case argument == "--apply":
			if request.Apply {
				return ConfigurationUpdateRequest{}, false, fmt.Errorf("option --apply may be specified only once")
			}
			request.Apply = true
		case strings.HasPrefix(argument, "--"):
			return ConfigurationUpdateRequest{}, false, fmt.Errorf("unknown option %s", argument)
		case !isEditableConfigurationKey(argument):
			return ConfigurationUpdateRequest{}, false, fmt.Errorf("%q is not an administrator-editable key", argument)
		default:
			if _, duplicate := seen[argument]; duplicate {
				return ConfigurationUpdateRequest{}, false, fmt.Errorf("configuration key %s is repeated", argument)
			}
			seen[argument] = struct{}{}
			request.Unset = append(request.Unset, argument)
		}
	}
	if len(request.Unset) == 0 {
		return ConfigurationUpdateRequest{}, false, fmt.Errorf("at least one key is required")
	}
	return request, false, nil
}
