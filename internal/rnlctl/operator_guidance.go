package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type overviewReport struct {
	status               Status
	endpoint             string
	configurationProblem string
}

func newOverviewReport(status Status, configuration Configuration, configurationErr error) overviewReport {
	report := overviewReport{status: status}
	if !status.Installed {
		return report
	}
	if configurationErr != nil {
		report.configurationProblem = "read configuration: " + configurationErr.Error()
		return report
	}
	report.endpoint = configurationEndpoint(configuration)
	return report
}

func (report overviewReport) healthy() bool {
	return report.status.Healthy && report.configurationProblem == ""
}

func (report overviewReport) problems() []string {
	problems := append([]string(nil), report.status.Problems...)
	if report.configurationProblem != "" {
		problems = append(problems, report.configurationProblem)
	}
	return problems
}

func configurationEndpoint(configuration Configuration) string {
	port := strings.TrimSpace(configuration.Values["NODE_PORT"])
	if port == "" {
		return "unknown"
	}
	host := strings.TrimSpace(configuration.Values["NODE_BIND_ADDR"])
	if host == "" {
		host = "*"
	} else if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, port)
}

func renderOverview(report overviewReport, color bool) string {
	status := report.status
	var output strings.Builder
	output.WriteString("Remnanode Lite\n")
	writeStatusField(&output, "State", valueOr(status.Deployment, "unknown"))

	health, healthColor := "unhealthy", ansiRed
	if status.Deployment == "absent" {
		health, healthColor = "not installed", ansiYellow
	} else if report.healthy() {
		health, healthColor = "healthy", ansiGreen
	}
	writeStatusField(&output, "Health", styled(health, healthColor, color))
	if status.Version != "" {
		writeStatusField(&output, "Version", status.Version)
	}
	if status.Generation != "" {
		writeStatusField(&output, "Generation", status.Generation)
	}
	if status.Previous != "" {
		writeStatusField(&output, "Previous", status.Previous)
	}
	if status.Service.Manager != "" {
		writeStatusField(&output, "Service", renderServiceStatus(status.Service))
	}
	if report.endpoint != "" {
		writeStatusField(&output, "Endpoint", report.endpoint)
	}
	if status.Pending != nil {
		pending := status.Pending.Operation
		if status.Pending.Phase != "" {
			pending += " / " + status.Pending.Phase
		}
		writeStatusField(&output, "Pending", pending)
	}
	if problems := report.problems(); len(problems) > 0 {
		output.WriteString("Problems:\n")
		for _, problem := range problems {
			fmt.Fprintf(&output, "  - %s\n", problem)
		}
	}

	output.WriteByte('\n')
	output.WriteString(renderCommandSection("Commands", overviewAdvice(report)))
	return output.String()
}

func overviewAdvice(report overviewReport) []string {
	status := report.status
	switch {
	case status.Deployment == "absent":
		return []string{"rnlctl install --help"}
	case status.Pending != nil:
		return []string{"sudo rnlctl repair"}
	case !report.healthy() || len(status.Problems) > 0:
		return []string{"sudo rnlctl doctor"}
	case status.Prepared || status.Deployment == "prepared":
		return []string{"sudo rnlctl activate --help"}
	case status.Service.Manager == "":
		return []string{"sudo rnlctl doctor"}
	case !status.Service.Active:
		return []string{"sudo rnlctl start"}
	default:
		return []string{
			"sudo rnlctl doctor",
			"sudo rnlctl logs node --lines 100",
			"sudo rnlctl upgrade --help",
		}
	}
}

func lifecycleSuccessAdvice(command string, result Result) []string {
	switch command {
	case "install", "activate", "upgrade", "rollback", "repair":
	default:
		return nil
	}
	if result.PreparedOnly {
		return []string{"sudo rnlctl activate --help", "sudo rnlctl overview"}
	}
	if command == "repair" && result.Generation == "" {
		return []string{"sudo rnlctl overview", "rnlctl install --help"}
	}
	return []string{"sudo rnlctl overview", "sudo rnlctl doctor"}
}

func lifecycleFailureAdvice(command string) []string {
	switch command {
	case "install", "activate", "upgrade", "rollback", "repair", "uninstall", "start", "stop", "restart":
	default:
		return nil
	}
	commands := []string{"sudo rnlctl status", "sudo rnlctl doctor"}
	if command == "start" || command == "restart" {
		commands = append(commands, "sudo rnlctl logs node --lines 100")
	}
	return commands
}

func renderLifecycleResult(result Result, advice []string) string {
	verb := "unchanged"
	if result.Changed {
		verb = "completed"
	}
	line := fmt.Sprintf("%s %s", result.Operation, verb)
	if result.Version != "" {
		line += ": " + result.Version
	}
	if len(advice) == 0 {
		return line + "\n"
	}
	return line + "\n" + renderCommandSection("Next", advice)
}

func renderCommandSection(heading string, commands []string) string {
	var output strings.Builder
	output.WriteString(heading)
	output.WriteString(":\n")
	for _, command := range commands {
		fmt.Fprintf(&output, "  %s\n", command)
	}
	return output.String()
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
