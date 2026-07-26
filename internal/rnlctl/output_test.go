package rnlctl

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseGlobalOptions(t *testing.T) {
	args, quiet, noColor, err := parseGlobalOptions([]string{
		"upgrade", "--to", "2.8.0-rnl.1", "--quiet", "GOMEMLIMIT=--quiet", "--no-color",
	})
	if err != nil {
		t.Fatalf("parseGlobalOptions() error = %v", err)
	}
	want := []string{"upgrade", "--to", "2.8.0-rnl.1", "GOMEMLIMIT=--quiet"}
	if !reflect.DeepEqual(args, want) || !quiet || !noColor {
		t.Fatalf("parseGlobalOptions() = %q, %t, %t; want %q, true, true", args, quiet, noColor, want)
	}
	for _, input := range [][]string{{"--quiet", "-q", "status"}, {"--no-color", "status", "--no-color"}} {
		if _, _, _, err := parseGlobalOptions(input); err == nil {
			t.Fatalf("parseGlobalOptions(%q) accepted a duplicate global option", input)
		}
	}
}

func TestAppQuietIsScopedToOneRun(t *testing.T) {
	lifecycle := &fakeLifecycle{result: Result{Operation: "restart", Changed: true}}
	var stdout, stderr bytes.Buffer
	application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})

	if code := application.Run(context.Background(), []string{"restart", "--quiet"}); code != exitOK {
		t.Fatalf("quiet restart = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("quiet stdout = %q", stdout.String())
	}
	if code := application.Run(context.Background(), []string{"restart"}); code != exitOK {
		t.Fatalf("regular restart = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "restart completed\n" {
		t.Fatalf("regular stdout = %q", stdout.String())
	}
}

func TestAppQuietStatusAndExplicitJSON(t *testing.T) {
	lifecycle := &fakeLifecycle{status: Status{SchemaVersion: 1, Deployment: "installed", Installed: true, Healthy: true}}
	for _, test := range []struct {
		name       string
		args       []string
		wantOutput bool
	}{
		{name: "human", args: []string{"--quiet", "status"}},
		{name: "json", args: []string{"status", "--json", "--quiet"}, wantOutput: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if (stdout.Len() > 0) != test.wantOutput {
				t.Fatalf("stdout = %q, wantOutput = %t", stdout.String(), test.wantOutput)
			}
		})
	}
}

func TestAppColorPolicy(t *testing.T) {
	status := Status{SchemaVersion: 1, Deployment: "installed", Installed: true, Healthy: true}
	tests := []struct {
		name      string
		args      []string
		terminal  bool
		env       map[string]string
		wantColor bool
	}{
		{name: "interactive terminal", terminal: true, wantColor: true},
		{name: "redirected", terminal: false},
		{name: "NO_COLOR", terminal: true, env: map[string]string{"NO_COLOR": "1"}},
		{name: "empty NO_COLOR", terminal: true, env: map[string]string{"NO_COLOR": ""}, wantColor: true},
		{name: "dumb terminal", terminal: true, env: map[string]string{"TERM": "dumb"}},
		{name: "flag", args: []string{"--no-color"}, terminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle:  &fakeLifecycle{status: status},
				Stdout:     &stdout,
				Stderr:     &stderr,
				IsTerminal: func(io.Writer) bool { return test.terminal },
				LookupEnv: func(key string) (string, bool) {
					value, ok := test.env[key]
					return value, ok
				},
			})
			args := append([]string{"status"}, test.args...)
			if code := application.Run(context.Background(), args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", args, code, stderr.String())
			}
			if strings.Contains(stdout.String(), "\x1b[") != test.wantColor {
				t.Fatalf("stdout color = %q, wantColor = %t", stdout.String(), test.wantColor)
			}
		})
	}
}

func TestRenderDoctorAddsOnlyActionableAdvice(t *testing.T) {
	report := DoctorReport{Checks: []Check{
		{Name: "transaction-journal", Status: "error", Detail: "interrupted upgrade"},
		{Name: "runtime-health", Status: "error", Detail: "socket unavailable"},
		{Name: "repair-cache:generation-a", Status: "warning", Detail: "root snapshot"},
	}}
	output := renderDoctor(report, false)
	for _, want := range []string{"sudo rnlctl repair", "sudo rnlctl logs node --lines 100"} {
		if !strings.Contains(output, want) {
			t.Fatalf("renderDoctor() = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("non-color doctor output contains ANSI: %q", output)
	}
}
