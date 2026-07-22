package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type recordingRunner struct {
	commands []Command
	exitCode int
}

type fakeLifecycle struct {
	installRequest   *InstallRequest
	activateRequest  *ActivateRequest
	upgradeRequest   *UpgradeRequest
	rollbackRequest  *RollbackRequest
	repairRequest    *RepairRequest
	uninstallRequest *UninstallRequest
	called           []string
	result           Result
	status           Status
	doctor           DoctorReport
	err              error
}

func (l *fakeLifecycle) Install(_ context.Context, request InstallRequest) (Result, error) {
	l.called = append(l.called, "install")
	l.installRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Activate(_ context.Context, request ActivateRequest) (Result, error) {
	l.called = append(l.called, "activate")
	l.activateRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Upgrade(_ context.Context, request UpgradeRequest) (Result, error) {
	l.called = append(l.called, "upgrade")
	l.upgradeRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Rollback(_ context.Context, request RollbackRequest) (Result, error) {
	l.called = append(l.called, "rollback")
	l.rollbackRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Repair(_ context.Context, request RepairRequest) (Result, error) {
	l.called = append(l.called, "repair")
	l.repairRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Uninstall(_ context.Context, request UninstallRequest) (Result, error) {
	l.called = append(l.called, "uninstall")
	l.uninstallRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) Status(context.Context) (Status, error) {
	l.called = append(l.called, "status")
	return l.status, l.err
}

func (l *fakeLifecycle) Doctor(context.Context) (DoctorReport, error) {
	l.called = append(l.called, "doctor")
	return l.doctor, l.err
}

func (l *fakeLifecycle) Start(context.Context) (Result, error) {
	l.called = append(l.called, "start")
	return l.result, l.err
}

func (l *fakeLifecycle) Stop(context.Context) (Result, error) {
	l.called = append(l.called, "stop")
	return l.result, l.err
}

func (l *fakeLifecycle) Restart(context.Context) (Result, error) {
	l.called = append(l.called, "restart")
	return l.result, l.err
}

func (r *recordingRunner) Run(_ context.Context, command Command) int {
	command.Args = append([]string(nil), command.Args...)
	r.commands = append(r.commands, command)
	return r.exitCode
}

func TestAppHelpAndVersionDoNotRunExternalCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no arguments", want: "Usage: rnlctl"},
		{name: "help", args: []string{"help"}, want: "Usage: rnlctl"},
		{name: "short help", args: []string{"-h"}, want: "Usage: rnlctl"},
		{name: "long help", args: []string{"--help"}, want: "Usage: rnlctl"},
		{name: "version", args: []string{"version"}, want: "rnlctl test-version\n"},
		{name: "version flag", args: []string{"--version"}, want: "rnlctl test-version\n"},
		{name: "command help", args: []string{"status", "--help"}, want: "Usage: rnlctl status [--json]\n"},
		{name: "logs help", args: []string{"logs", "core", "--help"}, want: "Usage: rnlctl logs"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Runner:        runner,
				LookPath:      missingExecutable,
				Stdout:        &stdout,
				Stderr:        &stderr,
				VersionString: "rnlctl test-version",
			})

			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if len(runner.commands) != 0 {
				t.Fatalf("external commands = %#v", runner.commands)
			}
		})
	}
}

func TestAppRejectsMalformedCommands(t *testing.T) {
	tests := [][]string{
		{"help", "extra"},
		{"version", "extra"},
		{"install", "--bundle-root", "/bundle", "--bundle-root", "/other"},
		{"install", "--unknown"},
		{"activate", "extra"},
		{"upgrade", "--to"},
		{"rollback", "--to", "one", "extra"},
		{"repair", "--bundle", "/bundle.tar.gz", "--sha256"},
		{"uninstall", "--purge", "extra"},
		{"status", "extra"},
		{"doctor", "extra"},
		{"start", "extra"},
		{"stop", "extra"},
		{"restart", "extra"},
		{"logs"},
		{"logs", "unknown"},
		{"logs", "node", "core"},
		{"logs", "node", "--unknown"},
		{"logs", "node", "--lines"},
		{"logs", "node", "--lines", "0"},
		{"logs", "node", "--lines", "100001"},
		{"logs", "node", "--lines", "not-a-number"},
		{"unknown"},
	}

	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			runner := &recordingRunner{}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Runner:   runner,
				LookPath: executableFinder(map[string]string{"systemctl": "/bin/systemctl"}),
				Stdout:   &stdout,
				Stderr:   &stderr,
			})

			if code := application.Run(context.Background(), args); code != 2 {
				t.Fatalf("Run(%q) = %d, stdout = %q, stderr = %q", args, code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("stderr = %q, want usage", stderr.String())
			}
			if len(runner.commands) != 0 {
				t.Fatalf("external commands = %#v", runner.commands)
			}
		})
	}
}

func TestAppDispatchesHumanStatusToSystemdAndPreservesExitCode(t *testing.T) {
	runner := &recordingRunner{exitCode: 23}
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"systemctl":  "/usr/bin/systemctl",
			"rc-service": "/sbin/rc-service",
		}),
		PathExists: func(string) bool { return false },
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})

	if code := application.Run(context.Background(), []string{"status"}); code != 23 {
		t.Fatalf("Run(status) = %d, want child exit 23", code)
	}
	assertSingleCommand(t, runner, "/usr/bin/systemctl", []string{"--no-pager", "--full", "status", "remnanode-lite.service"})
}

func TestAppDispatchesHumanStatusToOpenRC(t *testing.T) {
	runner := &recordingRunner{exitCode: 17}
	application := New(Options{
		Runner:   runner,
		LookPath: executableFinder(map[string]string{"rc-service": "/sbin/rc-service"}),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})

	if code := application.Run(context.Background(), []string{"status"}); code != 17 {
		t.Fatalf("Run(status) = %d, want child exit 17", code)
	}
	assertSingleCommand(t, runner, "/sbin/rc-service", []string{"remnanode-lite", "status"})
}

func TestAppUsesActiveOpenRCWhenBothServiceClientsExist(t *testing.T) {
	runner := &recordingRunner{}
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"systemctl":  "/usr/bin/systemctl",
			"rc-service": "/sbin/rc-service",
		}),
		PathExists: func(path string) bool {
			return path == openRCRuntime
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	})

	if code := application.Run(context.Background(), []string{"status"}); code != 0 {
		t.Fatalf("Run(status) = %d", code)
	}
	assertSingleCommand(t, runner, "/sbin/rc-service", []string{"remnanode-lite", "status"})
}

func TestAppPrefersSystemdWhenBothRuntimeMarkersExist(t *testing.T) {
	runner := &recordingRunner{}
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"systemctl":  "/usr/bin/systemctl",
			"rc-service": "/sbin/rc-service",
		}),
		PathExists: func(string) bool { return true },
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})

	if code := application.Run(context.Background(), []string{"status"}); code != 0 {
		t.Fatalf("Run(status) = %d", code)
	}
	assertSingleCommand(t, runner, "/usr/bin/systemctl", []string{"--no-pager", "--full", "status", "remnanode-lite.service"})
}

func TestAppReportsMissingServiceManager(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	application := New(Options{
		Runner:   runner,
		LookPath: missingExecutable,
		Stdout:   io.Discard,
		Stderr:   &stderr,
	})

	if code := application.Run(context.Background(), []string{"status"}); code != 1 {
		t.Fatalf("Run(status) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "neither systemctl nor rc-service") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(runner.commands) != 0 {
		t.Fatalf("external commands = %#v", runner.commands)
	}
}

func TestAppDispatchesServiceMutationsToLifecycle(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			lifecycle := &fakeLifecycle{result: Result{Operation: action, Changed: true, Version: "2.8.0-rnl.1"}}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})

			if code := application.Run(context.Background(), []string{action}); code != 0 {
				t.Fatalf("Run(%s) = %d, stderr = %q", action, code, stderr.String())
			}
			if !reflect.DeepEqual(lifecycle.called, []string{action}) {
				t.Fatalf("lifecycle calls = %q, want %q", lifecycle.called, []string{action})
			}
			if got := stdout.String(); got != action+" completed: 2.8.0-rnl.1\n" {
				t.Fatalf("stdout = %q", got)
			}
		})
	}
}

func TestAppRendersDoctorReports(t *testing.T) {
	report := DoctorReport{
		SchemaVersion: 1,
		Healthy:       true,
		Checks: []Check{
			{Name: "lifecycle-state", Status: "ok", Detail: "generation-a"},
			{Name: "configuration", Status: "warning"},
		},
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "text", args: []string{"doctor"}, want: "[OK] lifecycle-state - generation-a\n[WARNING] configuration\n"},
		{name: "json", args: []string{"doctor", "--json"}, want: `"schemaVersion":1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{doctor: report}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.want)
			}
			if !reflect.DeepEqual(lifecycle.called, []string{"doctor"}) {
				t.Fatalf("lifecycle calls = %q", lifecycle.called)
			}
		})
	}
}

func TestAppMapsLifecycleCommandFlags(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, *fakeLifecycle)
	}{
		{
			name: "install",
			args: []string{"install", "--bundle-root", "/bundle", "--expected-version", "2.8.0-rnl.1", "--port", "38329", "--secret-file", "/secret", "--prepare-only"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := InstallRequest{Bundle: BundleInput{Root: "/bundle", ExpectedVersion: "2.8.0-rnl.1"}, Port: 38329, SecretFile: "/secret", PrepareOnly: true}
				if lifecycle.installRequest == nil || !reflect.DeepEqual(*lifecycle.installRequest, want) {
					t.Fatalf("install request = %#v, want %#v", lifecycle.installRequest, want)
				}
			},
		},
		{
			name: "activate",
			args: []string{"activate", "--secret-file=/secret"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := ActivateRequest{SecretFile: "/secret"}
				if lifecycle.activateRequest == nil || *lifecycle.activateRequest != want {
					t.Fatalf("activate request = %#v, want %#v", lifecycle.activateRequest, want)
				}
			},
		},
		{
			name: "upgrade local archive",
			args: []string{"upgrade", "--bundle", "/bundle.tar.gz", "--sha256", "abc", "--expected-version", "2.8.0-rnl.1"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := UpgradeRequest{Bundle: BundleInput{Archive: "/bundle.tar.gz", SHA256: "abc", ExpectedVersion: "2.8.0-rnl.1"}}
				if lifecycle.upgradeRequest == nil || !reflect.DeepEqual(*lifecycle.upgradeRequest, want) {
					t.Fatalf("upgrade request = %#v, want %#v", lifecycle.upgradeRequest, want)
				}
			},
		},
		{
			name: "upgrade exact release",
			args: []string{"upgrade", "--to=2.8.0-rnl.2"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := UpgradeRequest{To: "2.8.0-rnl.2"}
				if lifecycle.upgradeRequest == nil || !reflect.DeepEqual(*lifecycle.upgradeRequest, want) {
					t.Fatalf("upgrade request = %#v, want %#v", lifecycle.upgradeRequest, want)
				}
			},
		},
		{
			name: "rollback",
			args: []string{"rollback", "--to", "2.8.0-rnl.1-0123456789abcdef"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := RollbackRequest{GenerationID: "2.8.0-rnl.1-0123456789abcdef"}
				if lifecycle.rollbackRequest == nil || *lifecycle.rollbackRequest != want {
					t.Fatalf("rollback request = %#v, want %#v", lifecycle.rollbackRequest, want)
				}
			},
		},
		{
			name: "repair",
			args: []string{"repair", "--bundle-root", "/bundle", "--expected-version=2.8.0-rnl.1"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := RepairRequest{Bundle: BundleInput{Root: "/bundle", ExpectedVersion: "2.8.0-rnl.1"}}
				if lifecycle.repairRequest == nil || !reflect.DeepEqual(*lifecycle.repairRequest, want) {
					t.Fatalf("repair request = %#v, want %#v", lifecycle.repairRequest, want)
				}
			},
		},
		{
			name: "uninstall",
			args: []string{"uninstall", "--purge", "--yes"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := UninstallRequest{Purge: true, Yes: true}
				if lifecycle.uninstallRequest == nil || *lifecycle.uninstallRequest != want {
					t.Fatalf("uninstall request = %#v, want %#v", lifecycle.uninstallRequest, want)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{result: Result{Operation: strings.Fields(test.name)[0]}}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Runner: &recordingRunner{}, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if len(lifecycle.called) != 1 || lifecycle.called[0] != test.args[0] {
				t.Fatalf("lifecycle calls = %q, want %q", lifecycle.called, test.args[0])
			}
			test.assert(t, lifecycle)
		})
	}
}

func TestAppStatusJSONUsesLifecycleHealth(t *testing.T) {
	tests := []struct {
		name       string
		status     Status
		wantExit   int
		wantOutput string
	}{
		{name: "healthy installed", status: Status{SchemaVersion: 1, Deployment: "installed", Installed: true, Healthy: true}, wantOutput: `"healthy":true`},
		{name: "degraded", status: Status{SchemaVersion: 1, Deployment: "degraded", Installed: true}, wantExit: 1, wantOutput: `"deployment":"degraded"`},
		{name: "absent", status: Status{SchemaVersion: 1, Deployment: "absent", Healthy: true}, wantOutput: `"deployment":"absent"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{status: test.status}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), []string{"status", "--json"}); code != test.wantExit {
				t.Fatalf("Run(status --json) = %d, want %d; stderr = %q", code, test.wantExit, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantOutput) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.wantOutput)
			}
			if !reflect.DeepEqual(lifecycle.called, []string{"status"}) {
				t.Fatalf("lifecycle calls = %q", lifecycle.called)
			}
		})
	}
}

func TestAppLifecycleErrorsReturnFailureWithoutExternalCommands(t *testing.T) {
	runner := &recordingRunner{}
	lifecycle := &fakeLifecycle{err: errors.New("lifecycle unavailable")}
	var stdout, stderr bytes.Buffer
	application := New(Options{Lifecycle: lifecycle, Runner: runner, Stdout: &stdout, Stderr: &stderr})

	if code := application.Run(context.Background(), []string{"restart"}); code != 1 {
		t.Fatalf("Run(restart) = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "lifecycle unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if len(runner.commands) != 0 {
		t.Fatalf("external commands = %#v", runner.commands)
	}
}

func TestAppDispatchesSystemdNodeLogs(t *testing.T) {
	runner := &recordingRunner{exitCode: 4}
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"systemctl":  "/usr/bin/systemctl",
			"journalctl": "/usr/bin/journalctl",
		}),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})

	args := []string{"logs", "--lines", "125", "node", "--follow"}
	if code := application.Run(context.Background(), args); code != 4 {
		t.Fatalf("Run(%q) = %d, want child exit 4", args, code)
	}
	assertSingleCommand(t, runner, "/usr/bin/journalctl", []string{
		"--no-pager",
		"--unit", "remnanode-lite.service",
		"--lines", "125",
		"--follow",
	})
}

func TestAppDispatchesOpenRCNodeLogs(t *testing.T) {
	runner := &recordingRunner{}
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"rc-service": "/sbin/rc-service",
			"tail":       "/usr/bin/tail",
		}),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})

	if code := application.Run(context.Background(), []string{"logs", "node", "-n=9", "-f"}); code != 0 {
		t.Fatalf("Run(logs node) = %d", code)
	}
	assertSingleCommand(t, runner, "/usr/bin/tail", []string{
		"-n", "9", "-F",
		"/var/log/remnanode-lite/openrc.log",
		"/var/log/remnanode-lite/openrc.err.log",
	})
}

func TestAppDispatchesCoreLogsWithoutServiceManager(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "core defaults",
			args: []string{"logs", "core"},
			want: []string{"-n", "50", "/var/log/remnanode-lite/xray.out.log"},
		},
		{
			name: "core errors follow",
			args: []string{"logs", "core-errors", "--follow", "--lines=7"},
			want: []string{"-n", "7", "-F", "/var/log/remnanode-lite/xray.err.log"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			application := New(Options{
				Runner:   runner,
				LookPath: executableFinder(map[string]string{"tail": "/bin/tail"}),
				Stdout:   io.Discard,
				Stderr:   io.Discard,
			})

			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d", test.args, code)
			}
			assertSingleCommand(t, runner, "/bin/tail", test.want)
		})
	}
}

func TestAppReportsMissingLogReader(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		paths map[string]string
		want  string
	}{
		{
			name: "journalctl",
			args: []string{"logs", "node"},
			paths: map[string]string{
				"systemctl": "/usr/bin/systemctl",
			},
			want: "journalctl",
		},
		{
			name:  "tail",
			args:  []string{"logs", "core"},
			paths: map[string]string{},
			want:  "tail",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			var stderr bytes.Buffer
			application := New(Options{
				Runner:   runner,
				LookPath: executableFinder(test.paths),
				Stdout:   io.Discard,
				Stderr:   &stderr,
			})

			if code := application.Run(context.Background(), test.args); code != 1 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			if len(runner.commands) != 0 {
				t.Fatalf("external commands = %#v", runner.commands)
			}
		})
	}
}

func TestAppReportsOutputWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	application := New(Options{
		Runner:        &recordingRunner{},
		LookPath:      missingExecutable,
		Stdout:        failingWriter{},
		Stderr:        &stderr,
		VersionString: "rnlctl test-version",
	})

	if code := application.Run(context.Background(), []string{"version"}); code != 1 {
		t.Fatalf("Run(version) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "write output") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func assertSingleCommand(t *testing.T, runner *recordingRunner, name string, args []string) {
	t.Helper()
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v, want one", runner.commands)
	}
	command := runner.commands[0]
	if command.Name != name || !reflect.DeepEqual(command.Args, args) {
		t.Fatalf("command = %q %q, want %q %q", command.Name, command.Args, name, args)
	}
	if command.Stdin == nil || command.Stdout == nil || command.Stderr == nil {
		t.Fatalf("command streams were not connected: %#v", command)
	}
}

func executableFinder(paths map[string]string) LookPathFunc {
	return func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", errors.New("executable not found")
	}
}

func missingExecutable(string) (string, error) {
	return "", errors.New("executable not found")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
