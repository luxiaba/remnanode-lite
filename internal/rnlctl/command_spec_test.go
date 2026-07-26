package rnlctl

import (
	"strings"
	"testing"
)

func TestValidateCommandSpecRejectsMalformedMetadata(t *testing.T) {
	validChild := func() commandSpec {
		return commandSpec{
			Name:        "status",
			Description: "Show status",
			Help:        commandHelpSpec{Synopsis: "rnlctl status"},
		}
	}
	validRoot := func() commandSpec {
		return commandSpec{
			Name:        "rnlctl",
			Description: "Administration CLI",
			Help:        commandHelpSpec{Synopsis: "rnlctl <command>"},
			Commands:    []commandSpec{validChild()},
		}
	}

	for _, test := range []struct {
		name   string
		mutate func(*commandSpec)
		want   string
	}{
		{
			name: "duplicate command",
			mutate: func(root *commandSpec) {
				root.Commands = append(root.Commands, validChild())
			},
			want: "repeats subcommand",
		},
		{
			name: "duplicate option",
			mutate: func(root *commandSpec) {
				root.Commands[0].Options = []commandOptionSpec{
					{Long: "json", Description: "JSON output"},
					{Long: "json", Description: "JSON output"},
				}
			},
			want: "repeats option",
		},
		{
			name: "invalid help block",
			mutate: func(root *commandSpec) {
				root.Commands[0].Help.Blocks = []commandHelpBlock{{Heading: "Options"}}
			},
			want: "invalid help block",
		},
		{
			name: "help description without label",
			mutate: func(root *commandSpec) {
				root.Commands[0].Options = []commandOptionSpec{{
					Long: "json", Description: "JSON output", HelpDescription: "Emit JSON",
				}}
			},
			want: "help text without a label",
		},
		{
			name: "missing synopsis",
			mutate: func(root *commandSpec) {
				root.Commands[0].Help.Synopsis = ""
			},
			want: "invalid help synopsis",
		},
		{
			name: "finite values require word option",
			mutate: func(root *commandSpec) {
				root.Commands[0].Options = []commandOptionSpec{{
					Long: "progress", Description: "Progress mode",
					ValueCandidates: []commandArgumentSpec{{Value: "auto", Description: "Automatic"}},
				}}
			},
			want: "finite values without a word value",
		},
		{
			name: "invalid finite value",
			mutate: func(root *commandSpec) {
				root.Commands[0].Options = []commandOptionSpec{{
					Long: "progress", Description: "Progress mode", Value: commandValueWord,
					ValueCandidates: []commandArgumentSpec{{Value: "not valid", Description: "Invalid"}},
				}}
			},
			want: "invalid finite value",
		},
		{
			name: "duplicate finite value",
			mutate: func(root *commandSpec) {
				root.Commands[0].Options = []commandOptionSpec{{
					Long: "progress", Description: "Progress mode", Value: commandValueWord,
					ValueCandidates: []commandArgumentSpec{
						{Value: "auto", Description: "Automatic"},
						{Value: "auto", Description: "Automatic"},
					},
				}}
			},
			want: "repeats finite value",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := validRoot()
			test.mutate(&spec)
			err := validateCommandSpec(spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateCommandSpec() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
