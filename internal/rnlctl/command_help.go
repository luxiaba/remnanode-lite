package rnlctl

import (
	"fmt"
	"strings"
)

type commandHelpSpec struct {
	Synopsis string
	Blocks   []commandHelpBlock
}

type commandHelpBlock struct {
	Heading           string
	DescriptionColumn int
	Rows              []commandHelpRow
	Text              string
}

type commandHelpRow struct {
	Label       string
	Description string
}

func usageForCommand(path ...string) string {
	command, ok := findCommandSpec(path...)
	if !ok {
		panic("rnlctl command specification is missing " + strings.Join(path, " "))
	}
	return renderCommandHelp(command.Help)
}

func renderCommandHelp(help commandHelpSpec) string {
	var output strings.Builder
	output.WriteString("Usage: ")
	output.WriteString(help.Synopsis)
	output.WriteByte('\n')
	for _, block := range help.Blocks {
		output.WriteByte('\n')
		if block.Heading != "" {
			output.WriteString(block.Heading)
			output.WriteString(":\n")
			for _, row := range block.Rows {
				fmt.Fprintf(&output, "  %-*s%s\n", block.DescriptionColumn, row.Label, row.Description)
			}
			continue
		}
		output.WriteString(block.Text)
		if !strings.HasSuffix(block.Text, "\n") {
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func commandHelpRows(commands []commandSpec) []commandHelpRow {
	rows := make([]commandHelpRow, 0, len(commands))
	for _, command := range commands {
		if command.HideFromParentHelp {
			continue
		}
		label := command.HelpListing
		if label == "" {
			label = command.Name
		}
		description := command.HelpDescription
		if description == "" {
			description = command.Description
		}
		rows = append(rows, commandHelpRow{Label: label, Description: description})
	}
	return rows
}

func optionHelpRows(options []commandOptionSpec) []commandHelpRow {
	rows := make([]commandHelpRow, 0, len(options))
	for _, option := range options {
		if option.HelpLabel == "" {
			continue
		}
		description := option.HelpDescription
		if description == "" {
			description = option.Description
		}
		rows = append(rows, commandHelpRow{Label: option.HelpLabel, Description: description})
	}
	return rows
}

func argumentHelpRows(arguments []commandArgumentSpec) []commandHelpRow {
	rows := make([]commandHelpRow, 0, len(arguments))
	for _, argument := range arguments {
		rows = append(rows, commandHelpRow{Label: argument.Value, Description: argument.Description})
	}
	return rows
}

func validateCommandHelp(path string, help commandHelpSpec) error {
	if help.Synopsis == "" || strings.ContainsAny(help.Synopsis, "\r\n") {
		return fmt.Errorf("command %q has an invalid help synopsis", path)
	}
	for _, block := range help.Blocks {
		hasRows := block.Heading != "" || len(block.Rows) != 0 || block.DescriptionColumn != 0
		hasText := block.Text != ""
		if hasRows == hasText || hasRows && (block.Heading == "" || len(block.Rows) == 0 || block.DescriptionColumn < 1) {
			return fmt.Errorf("command %q has an invalid help block", path)
		}
		if hasText && strings.HasSuffix(block.Text, "\n\n") {
			return fmt.Errorf("command %q help paragraph has excess trailing space", path)
		}
		for _, row := range block.Rows {
			if row.Label == "" || row.Description == "" || len(row.Label) >= block.DescriptionColumn {
				return fmt.Errorf("command %q has an invalid help row", path)
			}
		}
	}
	return nil
}
