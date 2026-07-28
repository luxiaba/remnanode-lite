package rnlctl

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

const (
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiReset  = "\x1b[0m"
)

func isTerminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func terminalWidth(writer io.Writer) int {
	file, ok := writer.(*os.File)
	if !ok {
		return defaultTerminalWidth
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width < 1 {
		return defaultTerminalWidth
	}
	return width
}

func (a *App) colorEnabled() bool {
	return a.colorEnabledFor(a.stdout)
}

func (a *App) colorEnabledFor(writer io.Writer) bool {
	if a.noColor || a.quiet || a.isTerminal == nil || !a.isTerminal(writer) {
		return false
	}
	if value, ok := a.lookupEnv("NO_COLOR"); ok && value != "" {
		return false
	}
	if value, ok := a.lookupEnv("TERM"); ok && value == "dumb" {
		return false
	}
	return true
}

func renderStatus(status Status, color bool) string {
	var output strings.Builder
	output.WriteString("Remnanode Lite\n")
	writeStatusField(&output, "State", valueOr(status.Deployment, "unknown"))
	health := "unhealthy"
	healthColor := ansiRed
	if status.Deployment == "absent" {
		health = "not installed"
		healthColor = ansiYellow
	} else if status.Healthy {
		health = "healthy"
		healthColor = ansiGreen
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
	if status.RepairCapability != "" {
		writeStatusField(&output, "Repair", status.RepairCapability)
	}
	if status.Pending != nil {
		pending := status.Pending.Operation
		if status.Pending.Phase != "" {
			pending += " / " + status.Pending.Phase
		}
		writeStatusField(&output, "Pending", pending)
	}
	if len(status.Problems) > 0 {
		output.WriteString("Problems:\n")
		for _, problem := range status.Problems {
			fmt.Fprintf(&output, "  - %s\n", problem)
		}
	}
	if status.Deployment == "recovery-required" && status.Pending != nil {
		output.WriteString("Next:        sudo rnlctl repair\n")
	} else if len(status.Problems) > 0 {
		output.WriteString("Next:        sudo rnlctl doctor\n")
	}
	return output.String()
}

func writeStatusField(output *strings.Builder, label, value string) {
	fmt.Fprintf(output, "%-13s%s\n", label+":", value)
}

func renderServiceStatus(status ServiceStatus) string {
	enabled := "disabled"
	if status.Enabled {
		enabled = "enabled"
	}
	active := "inactive"
	if status.Active {
		active = "active"
	}
	return fmt.Sprintf("%s (%s, %s)", status.Manager, enabled, active)
}

func renderUpgradePlan(plan UpgradePlan) string {
	var output strings.Builder
	output.WriteString("Upgrade preflight\n")
	result := "ready"
	if !plan.ChangeRequired {
		result = "already current"
	}
	writeStatusField(&output, "Result", result)
	writeStatusField(&output, "Current", plan.CurrentVersion+" ("+plan.CurrentGeneration+")")
	writeStatusField(&output, "Target", plan.TargetVersion+" ("+plan.TargetGeneration+")")
	if plan.Service.Manager != "" {
		writeStatusField(&output, "Service", renderServiceStatus(plan.Service))
	}
	mode := "installed"
	if plan.Prepared {
		mode = "prepared"
	}
	writeStatusField(&output, "Mode", mode)
	output.WriteString("Known preconditions passed. No installation or service changes were made.\n")
	return output.String()
}

func renderDoctor(report DoctorReport, color bool) string {
	var output strings.Builder
	errorsCount := 0
	warningsCount := 0
	for _, check := range report.Checks {
		label, padding, colorCode := "[ERROR]", " ", ansiRed
		switch check.Status {
		case "ok":
			label, padding, colorCode = "[OK]", "    ", ansiGreen
		case "warning":
			label, padding, colorCode = "[WARN]", "  ", ansiYellow
			warningsCount++
		default:
			errorsCount++
		}
		output.WriteString(styled(label, colorCode, color))
		output.WriteString(padding)
		output.WriteString(check.Name)
		if check.Detail != "" {
			output.WriteString(" - ")
			output.WriteString(check.Detail)
		}
		output.WriteByte('\n')
	}

	result := "healthy"
	resultColor := ansiGreen
	if !report.Healthy {
		result = "unhealthy"
		resultColor = ansiRed
	} else if warningsCount > 0 {
		result = "healthy with warnings"
		resultColor = ansiYellow
	}
	fmt.Fprintf(&output, "\nResult: %s (%d checks, %d errors, %d warnings)\n",
		styled(result, resultColor, color), len(report.Checks), errorsCount, warningsCount)

	advice := doctorAdvice(report.Checks)
	if len(advice) > 0 {
		output.WriteString("Next:\n")
		for _, command := range advice {
			fmt.Fprintf(&output, "  - %s\n", command)
		}
	}
	return output.String()
}

func doctorAdvice(checks []Check) []string {
	commands := make(map[string]struct{})
	for _, check := range checks {
		if check.Status != "error" {
			continue
		}
		var command string
		switch {
		case check.Name == "installation-state":
			command = "rnlctl install --help"
		case check.Name == "configuration":
			command = "sudo rnlctl config check"
		case check.Name == "runtime-health":
			command = "sudo rnlctl logs node --lines 100"
		case check.Name == "service":
			command = "sudo rnlctl status"
		case check.Name == "transaction-journal" && check.Detail == pendingJournalRepairDetail,
			check.Name == "generation-links",
			check.Name == "managed-permissions",
			strings.HasPrefix(check.Name, "generation:"),
			strings.HasPrefix(check.Name, "repair-cache:"):
			command = "sudo rnlctl repair"
		}
		if command != "" {
			commands[command] = struct{}{}
		}
	}
	result := make([]string, 0, len(commands))
	for command := range commands {
		result = append(result, command)
	}
	sort.Strings(result)
	return result
}

func styled(value, colorCode string, enabled bool) string {
	if !enabled {
		return value
	}
	return colorCode + value + ansiReset
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
