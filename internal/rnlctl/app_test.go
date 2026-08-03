package rnlctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordingRunner struct {
	commands []Command
	exitCode int
}

type fakeLifecycle struct {
	installRequest   *InstallRequest
	activateRequest  *ActivateRequest
	upgradeRequest   *UpgradeRequest
	preflightRequest *UpgradeRequest
	rollbackRequest  *RollbackRequest
	repairRequest    *RepairRequest
	uninstallRequest *UninstallRequest
	configRequest    *ConfigurationUpdateRequest
	secretRequest    *SecretUpdateRequest
	called           []string
	result           Result
	status           Status
	doctor           DoctorReport
	upgradePlan      UpgradePlan
	configuration    Configuration
	configurationErr error
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

func (l *fakeLifecycle) PreflightUpgrade(_ context.Context, request UpgradeRequest) (UpgradePlan, error) {
	l.called = append(l.called, "upgrade-preflight")
	l.preflightRequest = &request
	return l.upgradePlan, l.err
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

func (l *fakeLifecycle) ReadConfiguration(context.Context) (Configuration, error) {
	l.called = append(l.called, "config-read")
	if l.configurationErr != nil {
		return Configuration{}, l.configurationErr
	}
	return l.configuration, l.err
}

func (l *fakeLifecycle) UpdateConfiguration(_ context.Context, request ConfigurationUpdateRequest) (Result, error) {
	l.called = append(l.called, "config-update")
	l.configRequest = &request
	return l.result, l.err
}

func (l *fakeLifecycle) CheckConfiguration(context.Context) error {
	l.called = append(l.called, "config-check")
	return l.err
}

func (l *fakeLifecycle) ApplyConfiguration(context.Context) (Result, error) {
	l.called = append(l.called, "config-apply")
	return l.result, l.err
}

func (l *fakeLifecycle) SetSecret(_ context.Context, request SecretUpdateRequest) (Result, error) {
	l.called = append(l.called, "secret-set")
	l.secretRequest = &request
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
		{name: "config help", args: []string{"config", "--help"}, want: "Usage: rnlctl config"},
		{name: "secret help", args: []string{"secret", "--help"}, want: "Usage: rnlctl secret set"},
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
		{"config"},
		{"config", "show", "extra"},
		{"config", "get", "SECRET_KEY"},
		{"config", "set"},
		{"config", "set", "INTERNAL_REST_TOKEN=value"},
		{"config", "set", "NODE_PORT=12345", "NODE_PORT=2222"},
		{"config", "set", "NODE_PORT=12345", "--apply", "--apply"},
		{"config", "unset"},
		{"config", "unset", "XRAY_BIN"},
		{"config", "check", "extra"},
		{"config", "apply", "extra"},
		{"secret"},
		{"secret", "set"},
		{"secret", "set", "--file", "/one", "--file", "/two"},
		{"overview", "extra"},
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
		{"logs", "node", "--follow", "-f"},
		{"logs", "node", "--lines", "10", "-n", "20"},
		{"logs", "node", "--since"},
		{"logs", "node", "--since", "0s"},
		{"logs", "node", "--since", "15m", "--since=1h"},
		{"logs", "core", "--since", "15m"},
		{"--quiet", "-q", "status"},
		{"--no-color", "status", "--no-color"},
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

func TestAppRendersHumanStatusFromLifecycle(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		wantExit int
		want     []string
		notWant  []string
	}{
		{
			name: "healthy installed",
			status: Status{
				SchemaVersion: 1, Deployment: "installed", Installed: true, Healthy: true,
				Version: "2.8.0", Generation: "generation-a", Previous: "generation-b",
				Service:          ServiceStatus{Manager: "systemd", Enabled: true, Active: true},
				RepairCapability: "verified-archive",
			},
			want: []string{"Remnanode Lite", "State:       installed", "Health:      healthy", "Version:     2.8.0", "Service:     systemd (enabled, active)"},
		},
		{
			name:     "unreadable recovery state",
			status:   Status{SchemaVersion: 1, Deployment: "recovery-required", Installed: true, Problems: []string{"interrupted upgrade"}},
			wantExit: 1,
			want:     []string{"Health:      unhealthy", "Problems:", "interrupted upgrade", "Next:        sudo rnlctl doctor"},
			notWant:  []string{"Next:        sudo rnlctl repair"},
		},
		{
			name: "pending transaction",
			status: Status{
				SchemaVersion: 1, Deployment: "recovery-required", Installed: true,
				Pending: &PendingOperation{Operation: "upgrade", Phase: "planned"},
			},
			wantExit: 1,
			want:     []string{"Pending:     upgrade / planned", "Next:        sudo rnlctl repair"},
		},
		{
			name:   "absent",
			status: Status{SchemaVersion: 1, Deployment: "absent", Healthy: true},
			want:   []string{"State:       absent", "Health:      not installed"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			lifecycle := &fakeLifecycle{status: test.status}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Runner: runner, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), []string{"status"}); code != test.wantExit {
				t.Fatalf("Run(status) = %d, want %d; stderr = %q", code, test.wantExit, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
			}
			for _, notWant := range test.notWant {
				if strings.Contains(stdout.String(), notWant) {
					t.Fatalf("stdout = %q, do not want %q", stdout.String(), notWant)
				}
			}
			if len(runner.commands) != 0 || !reflect.DeepEqual(lifecycle.called, []string{"status"}) {
				t.Fatalf("runner commands = %#v, lifecycle calls = %q", runner.commands, lifecycle.called)
			}
		})
	}
}

func TestAppRendersOperatorOverview(t *testing.T) {
	tests := []struct {
		name          string
		status        Status
		configuration Configuration
		configErr     error
		wantExit      int
		wantCalls     []string
		want          []string
		notWant       []string
	}{
		{
			name: "healthy active",
			status: Status{
				Deployment: "installed", Installed: true, Healthy: true,
				Version: "2.8.0-rnl.2", Generation: "generation-b", Previous: "generation-a",
				Service: ServiceStatus{Manager: "systemd", Enabled: true, Active: true},
			},
			configuration: Configuration{Values: map[string]string{"NODE_PORT": "2222", "NODE_BIND_ADDR": "::"}},
			wantCalls:     []string{"status", "config-read"},
			want: []string{
				"Remnanode Lite", "State:       installed", "Health:      healthy",
				"Version:     2.8.0-rnl.2", "Endpoint:    [::]:2222", "Commands:",
				"sudo rnlctl doctor", "sudo rnlctl logs node --lines 100", "sudo rnlctl upgrade --help",
			},
		},
		{
			name:      "absent",
			status:    Status{Deployment: "absent", Healthy: true},
			wantCalls: []string{"status"},
			want:      []string{"State:       absent", "Health:      not installed", "rnlctl install --help"},
			notWant:   []string{"Endpoint:"},
		},
		{
			name: "prepared",
			status: Status{
				Deployment: "prepared", Installed: true, Prepared: true, Healthy: true,
				Service: ServiceStatus{Manager: "openrc"},
			},
			configuration: Configuration{Values: map[string]string{"NODE_PORT": "2222"}},
			wantCalls:     []string{"status", "config-read"},
			want:          []string{"State:       prepared", "Endpoint:    *:2222", "sudo rnlctl activate --help"},
			notWant:       []string{"sudo rnlctl start"},
		},
		{
			name: "pending repair",
			status: Status{
				Deployment: "recovery-required", Installed: true,
				Pending: &PendingOperation{Operation: "upgrade", Phase: "committed"},
			},
			configuration: Configuration{Values: map[string]string{"NODE_PORT": "8443", "NODE_BIND_ADDR": "127.0.0.1"}},
			wantExit:      exitFailure,
			wantCalls:     []string{"status", "config-read"},
			want:          []string{"Pending:     upgrade / committed", "sudo rnlctl repair"},
			notWant:       []string{"sudo rnlctl doctor"},
		},
		{
			name: "healthy inactive",
			status: Status{
				Deployment: "installed", Installed: true, Healthy: true,
				Service: ServiceStatus{Manager: "systemd", Enabled: true},
			},
			configuration: Configuration{Values: map[string]string{"NODE_PORT": "2222"}},
			wantCalls:     []string{"status", "config-read"},
			want:          []string{"Service:     systemd (enabled, inactive)", "sudo rnlctl start"},
		},
		{
			name: "configuration unavailable",
			status: Status{
				Deployment: "installed", Installed: true, Healthy: true,
				Service: ServiceStatus{Manager: "systemd", Enabled: true, Active: true},
			},
			configErr: errors.New("node.env is unreadable"),
			wantExit:  exitFailure,
			wantCalls: []string{"status", "config-read"},
			want:      []string{"Health:      unhealthy", "Problems:", "read configuration: node.env is unreadable", "sudo rnlctl doctor"},
			notWant:   []string{"Endpoint:"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{
				status: test.status, configuration: test.configuration, configurationErr: test.configErr,
			}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), []string{"overview"}); code != test.wantExit {
				t.Fatalf("Run(overview) = %d, want %d; stdout = %q, stderr = %q", code, test.wantExit, stdout.String(), stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
			}
			for _, notWant := range test.notWant {
				if strings.Contains(stdout.String(), notWant) {
					t.Fatalf("stdout = %q, do not want %q", stdout.String(), notWant)
				}
			}
			if !reflect.DeepEqual(lifecycle.called, test.wantCalls) {
				t.Fatalf("lifecycle calls = %q, want %q", lifecycle.called, test.wantCalls)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestAppQuietOverviewPreservesChecksAndExitCode(t *testing.T) {
	lifecycle := &fakeLifecycle{
		status:           Status{Deployment: "installed", Installed: true, Healthy: true},
		configurationErr: errors.New("node.env is unreadable"),
	}
	var stdout, stderr bytes.Buffer
	application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
	if code := application.Run(context.Background(), []string{"--quiet", "overview"}); code != exitFailure {
		t.Fatalf("quiet overview = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("quiet output: stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	if !reflect.DeepEqual(lifecycle.called, []string{"status", "config-read"}) {
		t.Fatalf("lifecycle calls = %q", lifecycle.called)
	}
}

func TestAppAddsGuidanceToLifecycleResults(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		result  Result
		want    []string
		notWant []string
	}{
		{
			name: "prepared install", args: []string{"install"},
			result:  Result{Operation: "install", Changed: true, Version: "2.8.0-rnl.2", PreparedOnly: true},
			want:    []string{"install completed: 2.8.0-rnl.2", "Next:", "sudo rnlctl activate --help", "sudo rnlctl overview"},
			notWant: []string{"sudo rnlctl doctor"},
		},
		{
			name: "upgrade", args: []string{"upgrade"},
			result: Result{Operation: "upgrade", Changed: true, Version: "2.8.0-rnl.2"},
			want:   []string{"upgrade completed: 2.8.0-rnl.2", "sudo rnlctl overview", "sudo rnlctl doctor"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{result: test.result}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
			}
			for _, notWant := range test.notWant {
				if strings.Contains(stdout.String(), notWant) {
					t.Fatalf("stdout = %q, do not want %q", stdout.String(), notWant)
				}
			}
		})
	}

	lifecycle := &fakeLifecycle{result: Result{Operation: "repair", Changed: true}}
	var stdout, stderr bytes.Buffer
	application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
	if code := application.Run(context.Background(), []string{"--quiet", "repair"}); code != exitOK {
		t.Fatalf("quiet repair = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("quiet output: stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestAppAddsSafeGuidanceToLifecycleFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		err     error
		want    []string
		notWant []string
	}{
		{
			name: "install", args: []string{"install"}, err: errors.New("bundle rejected"),
			want: []string{"rnlctl: install: bundle rejected", "Next:", "sudo rnlctl status", "sudo rnlctl doctor"},
		},
		{
			name: "restart", args: []string{"restart"}, err: errors.New("service unavailable"),
			want: []string{"sudo rnlctl status", "sudo rnlctl doctor", "sudo rnlctl logs node --lines 100"},
		},
		{
			name: "canceled", args: []string{"repair"}, err: context.Canceled,
			want: []string{"rnlctl: repair: context canceled"}, notWant: []string{"Next:", "sudo rnlctl"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{err: test.err}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != exitFailure {
				t.Fatalf("Run(%q) = %d, stdout = %q, stderr = %q", test.args, code, stdout.String(), stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			for _, notWant := range test.notWant {
				if strings.Contains(stderr.String(), notWant) {
					t.Fatalf("stderr = %q, do not want %q", stderr.String(), notWant)
				}
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
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
		want []string
	}{
		{name: "text", args: []string{"doctor"}, want: []string{"[OK]    lifecycle-state - generation-a", "[WARN]  configuration", "Result: healthy with warnings (2 checks, 0 errors, 1 warnings)"}},
		{name: "json", args: []string{"doctor", "--json"}, want: []string{`"schemaVersion":1`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{doctor: report}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout = %q, want %q", stdout.String(), want)
				}
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
			args: []string{"install", "--bundle-root", "/bundle", "--expected-version", "2.8.0-rnl.1", "--port", "12345", "--secret-file", "/secret", "--prepare-only"},
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := InstallRequest{Bundle: BundleInput{Root: "/bundle", ExpectedVersion: "2.8.0-rnl.1"}, Port: 12345, SecretFile: "/secret", PrepareOnly: true}
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

func TestAppRendersUpgradePreflight(t *testing.T) {
	plan := UpgradePlan{
		SchemaVersion: 1, ChangeRequired: true,
		CurrentVersion: "2.8.0", CurrentGeneration: "generation-a",
		TargetVersion: "2.8.0-rnl.1", TargetGeneration: "generation-b",
		Service: ServiceStatus{Manager: "systemd", Enabled: true, Active: true},
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "human", args: []string{"upgrade", "--to", "2.8.0-rnl.1", "--dry-run"}, want: "Known preconditions passed"},
		{name: "json", args: []string{"upgrade", "--dry-run", "--json", "--to", "2.8.0-rnl.1"}, want: `"changeRequired":true`},
		{name: "quiet remains explicit", args: []string{"--quiet", "upgrade", "--to", "2.8.0-rnl.1", "--dry-run"}, want: "Upgrade preflight"},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{upgradePlan: plan}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.want)
			}
			wantRequest := UpgradeRequest{To: "2.8.0-rnl.1"}
			if lifecycle.preflightRequest == nil || !reflect.DeepEqual(*lifecycle.preflightRequest, wantRequest) {
				t.Fatalf("preflight request = %#v, want %#v", lifecycle.preflightRequest, wantRequest)
			}
			if !reflect.DeepEqual(lifecycle.called, []string{"upgrade-preflight"}) {
				t.Fatalf("lifecycle calls = %q", lifecycle.called)
			}
		})
	}

	lifecycle := &fakeLifecycle{}
	var stdout, stderr bytes.Buffer
	application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
	if code := application.Run(context.Background(), []string{"upgrade", "--to", "2.8.0-rnl.1", "--json"}); code != exitUsage {
		t.Fatalf("upgrade --json = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if len(lifecycle.called) != 0 || !strings.Contains(stderr.String(), "--json requires --dry-run") {
		t.Fatalf("lifecycle calls = %q, stderr = %q", lifecycle.called, stderr.String())
	}
}

func TestAppConfigurationAndSecretCommands(t *testing.T) {
	values := map[string]string{
		"NODE_PORT":                "2222",
		"NODE_BIND_ADDR":           "127.0.0.1",
		"LOW_MEMORY":               "1",
		"BODY_LIMIT_MB":            "",
		"GOMEMLIMIT":               "180MiB",
		"DISABLE_HASHED_SET_CHECK": "false",
	}

	t.Run("show and get", func(t *testing.T) {
		for _, test := range []struct {
			args       []string
			wantOutput string
		}{
			{args: []string{"config", "show"}, wantOutput: "NODE_PORT=2222\nNODE_BIND_ADDR=127.0.0.1\n"},
			{args: []string{"config", "show", "--json"}, wantOutput: `"NODE_PORT":"2222"`},
			{args: []string{"config", "get", "GOMEMLIMIT"}, wantOutput: "180MiB\n"},
		} {
			lifecycle := &fakeLifecycle{configuration: Configuration{SchemaVersion: 1, Path: "/etc/remnanode-lite/node.env", Values: values}}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantOutput) || strings.Contains(stdout.String(), "SECRET") {
				t.Fatalf("stdout = %q, want %q without Secret data", stdout.String(), test.wantOutput)
			}
			if !reflect.DeepEqual(lifecycle.called, []string{"config-read"}) {
				t.Fatalf("lifecycle calls = %q", lifecycle.called)
			}
		}
	})

	t.Run("show JSON schema", func(t *testing.T) {
		lifecycle := &fakeLifecycle{configuration: Configuration{
			SchemaVersion: 1,
			Path:          "/etc/remnanode-lite/node.env",
			Values:        values,
		}}
		var stdout, stderr bytes.Buffer
		application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
		if code := application.Run(context.Background(), []string{"config", "show", "--json"}); code != 0 {
			t.Fatalf("Run(config show --json) = %d, stderr = %q", code, stderr.String())
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatalf("decode config JSON: %v", err)
		}
		for _, key := range []string{"schemaVersion", "path", "values"} {
			if _, exists := envelope[key]; !exists {
				t.Fatalf("config JSON is missing %q: %s", key, stdout.String())
			}
		}
		var outputValues map[string]string
		if err := json.Unmarshal(envelope["values"], &outputValues); err != nil {
			t.Fatalf("decode config values: %v", err)
		}
		if len(envelope) != 3 || len(outputValues) != len(editableConfigurationKeys) {
			t.Fatalf("config JSON schema = %s", stdout.String())
		}
		for _, forbidden := range []string{"SECRET_KEY", "SECRET_KEY_FILE", "INTERNAL_REST_TOKEN", "XRAY_BIN"} {
			if bytes.Contains(stdout.Bytes(), []byte(forbidden)) {
				t.Fatalf("config JSON exposed %s: %s", forbidden, stdout.String())
			}
		}
	})

	tests := []struct {
		name   string
		args   []string
		called string
		assert func(*testing.T, *fakeLifecycle)
	}{
		{
			name: "set", args: []string{"config", "set", "NODE_PORT=12345", "LOW_MEMORY=1", "--apply"}, called: "config-update",
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := ConfigurationUpdateRequest{Set: map[string]string{"NODE_PORT": "12345", "LOW_MEMORY": "1"}, Apply: true}
				if lifecycle.configRequest == nil || !reflect.DeepEqual(*lifecycle.configRequest, want) {
					t.Fatalf("config request = %#v, want %#v", lifecycle.configRequest, want)
				}
			},
		},
		{
			name: "unset", args: []string{"config", "unset", "BODY_LIMIT_MB", "GOMEMLIMIT"}, called: "config-update",
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := ConfigurationUpdateRequest{Unset: []string{"BODY_LIMIT_MB", "GOMEMLIMIT"}}
				if lifecycle.configRequest == nil || !reflect.DeepEqual(*lifecycle.configRequest, want) {
					t.Fatalf("config request = %#v, want %#v", lifecycle.configRequest, want)
				}
			},
		},
		{name: "check", args: []string{"config", "check"}, called: "config-check"},
		{name: "apply", args: []string{"config", "apply"}, called: "config-apply"},
		{
			name: "secret", args: []string{"secret", "set", "--file", "/root/new-secret.key", "--apply"}, called: "secret-set",
			assert: func(t *testing.T, lifecycle *fakeLifecycle) {
				want := SecretUpdateRequest{File: "/root/new-secret.key", Apply: true}
				if lifecycle.secretRequest == nil || *lifecycle.secretRequest != want {
					t.Fatalf("secret request = %#v, want %#v", lifecycle.secretRequest, want)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{result: Result{Operation: test.called, Changed: true}}
			var stdout, stderr bytes.Buffer
			application := New(Options{Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != 0 {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !reflect.DeepEqual(lifecycle.called, []string{test.called}) {
				t.Fatalf("lifecycle calls = %q, want %q", lifecycle.called, test.called)
			}
			if test.assert != nil {
				test.assert(t, lifecycle)
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
	if !strings.Contains(stderr.String(), "rnlctl: restart: lifecycle unavailable") {
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

func TestAppDispatchesSystemdNodeLogsSince(t *testing.T) {
	runner := &recordingRunner{}
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	application := New(Options{
		Runner: runner,
		LookPath: executableFinder(map[string]string{
			"systemctl":  "/usr/bin/systemctl",
			"journalctl": "/usr/bin/journalctl",
		}),
		Now:    func() time.Time { return now },
		Stdout: io.Discard,
		Stderr: io.Discard,
	})

	args := []string{"logs", "node", "--since=15m", "--lines", "20"}
	if code := application.Run(context.Background(), args); code != exitOK {
		t.Fatalf("Run(%q) = %d", args, code)
	}
	assertSingleCommand(t, runner, "/usr/bin/journalctl", []string{
		"--no-pager",
		"--unit", "remnanode-lite.service",
		"--lines", "20",
		"--since=@" + strconv.FormatInt(now.Add(-15*time.Minute).Unix(), 10),
	})
}

func TestAppRejectsLogsSinceOnOpenRC(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	application := New(Options{
		Runner:   runner,
		LookPath: executableFinder(map[string]string{"rc-service": "/sbin/rc-service", "tail": "/usr/bin/tail"}),
		Stdout:   io.Discard,
		Stderr:   &stderr,
	})
	if code := application.Run(context.Background(), []string{"logs", "node", "--since", "15m"}); code != exitFailure {
		t.Fatalf("Run(logs node --since) = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "systemd") || len(runner.commands) != 0 {
		t.Fatalf("stderr = %q, commands = %#v", stderr.String(), runner.commands)
	}
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

func TestAppReportsHumanAndJSONWriteFailures(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		lifecycle *fakeLifecycle
	}{
		{name: "human status", args: []string{"status"}, lifecycle: &fakeLifecycle{status: Status{Deployment: "installed", Healthy: true}}},
		{name: "JSON status", args: []string{"status", "--json"}, lifecycle: &fakeLifecycle{status: Status{Deployment: "installed", Healthy: true}}},
		{
			name: "overview", args: []string{"overview"},
			lifecycle: &fakeLifecycle{
				status:        Status{Deployment: "installed", Installed: true, Healthy: true},
				configuration: Configuration{Values: map[string]string{"NODE_PORT": "2222"}},
			},
		},
		{name: "human doctor", args: []string{"doctor"}, lifecycle: &fakeLifecycle{doctor: DoctorReport{Healthy: true}}},
		{name: "JSON doctor", args: []string{"doctor", "--json"}, lifecycle: &fakeLifecycle{doctor: DoctorReport{Healthy: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			application := New(Options{Lifecycle: test.lifecycle, Stdout: failingWriter{}, Stderr: &stderr})
			if code := application.Run(context.Background(), test.args); code != exitFailure {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "write") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
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
