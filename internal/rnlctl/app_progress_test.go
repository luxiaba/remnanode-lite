package rnlctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type progressTestLifecycle struct {
	*fakeLifecycle
	restartEvents   []progressEvent
	preflightEvents []progressEvent
	secretEvents    []progressEvent
	statusEvents    []progressEvent
	doctorEvents    []progressEvent
}

func (lifecycle *progressTestLifecycle) Restart(ctx context.Context) (Result, error) {
	lifecycle.called = append(lifecycle.called, "restart")
	emitProgressTestEvents(ctx, lifecycle.restartEvents)
	return lifecycle.result, lifecycle.err
}

func (lifecycle *progressTestLifecycle) PreflightUpgrade(ctx context.Context, request UpgradeRequest) (UpgradePlan, error) {
	lifecycle.called = append(lifecycle.called, "upgrade-preflight")
	lifecycle.preflightRequest = &request
	emitProgressTestEvents(ctx, lifecycle.preflightEvents)
	return lifecycle.upgradePlan, lifecycle.err
}

func (lifecycle *progressTestLifecycle) SetSecret(ctx context.Context, request SecretUpdateRequest) (Result, error) {
	lifecycle.called = append(lifecycle.called, "secret-set")
	lifecycle.secretRequest = &request
	emitProgressTestEvents(ctx, lifecycle.secretEvents)
	return lifecycle.result, lifecycle.err
}

func (lifecycle *progressTestLifecycle) Status(ctx context.Context) (Status, error) {
	lifecycle.called = append(lifecycle.called, "status")
	emitProgressTestEvents(ctx, lifecycle.statusEvents)
	return lifecycle.status, lifecycle.err
}

func (lifecycle *progressTestLifecycle) Doctor(ctx context.Context) (DoctorReport, error) {
	lifecycle.called = append(lifecycle.called, "doctor")
	emitProgressTestEvents(ctx, lifecycle.doctorEvents)
	return lifecycle.doctor, lifecycle.err
}

func emitProgressTestEvents(ctx context.Context, events []progressEvent) {
	for _, event := range events {
		emitProgressEvent(ctx, event)
	}
}

func TestAppProgressPolicyUsesStderrTerminalIndependently(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		terminal   bool
		wantStdout string
		wantStderr string
		wantTTY    bool
	}{
		{name: "auto terminal", args: []string{"restart"}, terminal: true, wantStdout: "restart completed\n", wantStderr: "OK", wantTTY: true},
		{name: "auto redirected", args: []string{"restart"}, wantStdout: "restart completed\n", wantStderr: "rnlctl: restart: Restart managed service"},
		{name: "forced plain", args: []string{"restart", "--progress=plain"}, terminal: true, wantStdout: "restart completed\n", wantStderr: "rnlctl: restart: Restart managed service"},
		{name: "never", args: []string{"--progress", "never", "restart"}, terminal: true, wantStdout: "restart completed\n"},
		{name: "quiet overrides explicit progress", args: []string{"restart", "--progress=plain", "--quiet"}, terminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &fakeLifecycle{result: Result{Operation: "restart", Changed: true}}
			lifecycle := &progressTestLifecycle{
				fakeLifecycle: base,
				restartEvents: []progressEvent{{Kind: progressPhaseStarted, Phase: phaseRestartService}},
			}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
				IsTerminal: func(writer io.Writer) bool {
					return test.terminal && writer == &stderr
				},
				TerminalWidth: func(io.Writer) int { return 64 },
				LookupEnv:     func(string) (string, bool) { return "", false },
				Now:           newProgressTestClock().Now,
			})

			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if stdout.String() != test.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.wantStdout)
			}
			if test.wantStderr == "" {
				if stderr.Len() != 0 {
					t.Fatalf("stderr = %q, want empty", stderr.String())
				}
				return
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.wantStderr)
			}
			if strings.Contains(stderr.String(), "\r\x1b[2K") != test.wantTTY {
				t.Fatalf("terminal controls in stderr = %q, want %t", stderr.String(), test.wantTTY)
			}
		})
	}
}

func TestAppProgressColorAndDumbTerminalPolicy(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		env       map[string]string
		wantColor bool
		wantPlain bool
	}{
		{name: "default color", wantColor: true},
		{name: "no-color flag", args: []string{"--no-color"}},
		{name: "NO_COLOR", env: map[string]string{"NO_COLOR": "1"}},
		{name: "empty NO_COLOR", env: map[string]string{"NO_COLOR": ""}, wantColor: true},
		{name: "dumb terminal", env: map[string]string{"TERM": "dumb"}, wantPlain: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &fakeLifecycle{result: Result{Operation: "restart", Changed: true}}
			lifecycle := &progressTestLifecycle{
				fakeLifecycle: base,
				restartEvents: []progressEvent{
					{Kind: progressPhaseStarted, Phase: phaseWaitHealthy},
					{Kind: progressPhaseHeartbeat, Phase: phaseWaitHealthy},
				},
			}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
				IsTerminal:    func(io.Writer) bool { return true },
				TerminalWidth: func(io.Writer) int { return 64 },
				LookupEnv: func(key string) (string, bool) {
					value, ok := test.env[key]
					return value, ok
				},
				Now: newProgressTestClock().Now,
			})
			args := append([]string{"restart"}, test.args...)
			if code := application.Run(context.Background(), args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", args, code, stderr.String())
			}

			got := stderr.String()
			if strings.Contains(got, ansiGreen) != test.wantColor {
				t.Fatalf("color in stderr = %q, want %t", got, test.wantColor)
			}
			if test.wantPlain {
				if strings.ContainsAny(got, "\r\x1b") || !strings.Contains(got, "rnlctl: restart: Wait for runtime health") {
					t.Fatalf("TERM=dumb stderr = %q, want stable plain output", got)
				}
			} else if !strings.Contains(got, "\r\x1b[2K") {
				t.Fatalf("interactive stderr = %q, want terminal redraw", got)
			}
		})
	}
}

func TestAppJSONDisablesProgressAndKeepsStdoutMachineReadable(t *testing.T) {
	for _, jsonArgument := range []string{"--json", "--json=true", "--json=1"} {
		t.Run(jsonArgument, func(t *testing.T) {
			plan := UpgradePlan{
				SchemaVersion: 1, ChangeRequired: true,
				CurrentVersion: "2.8.0", CurrentGeneration: "generation-a",
				TargetVersion: "2.8.0-rnl.1", TargetGeneration: "generation-b",
			}
			lifecycle := &progressTestLifecycle{
				fakeLifecycle: &fakeLifecycle{upgradePlan: plan},
				preflightEvents: []progressEvent{
					{Kind: progressPhaseStarted, Phase: phaseResolveRelease},
					{Kind: progressTransferUpdated, Phase: phaseDownloadBundle, Current: 32 << 20, Total: 64 << 20},
				},
			}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
				IsTerminal:    func(io.Writer) bool { return true },
				TerminalWidth: func(io.Writer) int { return 64 },
				Now:           newProgressTestClock().Now,
			})
			args := []string{"upgrade", "--to", "2.8.0-rnl.1", "--dry-run", jsonArgument}
			if code := application.Run(context.Background(), args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", args, code, stderr.String())
			}

			var decoded UpgradePlan
			decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatalf("decode stdout %q: %v", stdout.String(), err)
			}
			if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout contains more than one JSON value: %q, error = %v", stdout.String(), err)
			}
			if decoded != plan {
				t.Fatalf("decoded plan = %#v, want %#v", decoded, plan)
			}
			if strings.Contains(stdout.String(), "rnlctl:") || strings.ContainsAny(stdout.String(), "\r\x1b") {
				t.Fatalf("JSON stdout is polluted by progress: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("JSON progress stderr = %q, want disabled", stderr.String())
			}
		})
	}
}

func TestAppReadOnlyHealthHeartbeatsNeverStartProgress(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "status human", args: []string{"status"}},
		{name: "status JSON", args: []string{"status", "--json"}},
		{name: "overview", args: []string{"overview"}},
		{name: "doctor human", args: []string{"doctor"}},
		{name: "doctor JSON", args: []string{"doctor", "--json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &fakeLifecycle{
				status:        Status{SchemaVersion: 1, Deployment: "installed", Healthy: true},
				doctor:        DoctorReport{SchemaVersion: 1, Healthy: true},
				configuration: Configuration{Values: map[string]string{"NODE_PORT": "2222"}},
			}
			lifecycle := &progressTestLifecycle{
				fakeLifecycle: base,
				statusEvents: []progressEvent{
					{Kind: progressPhaseStarted, Phase: phaseWaitHealthy},
					{Kind: progressPhaseHeartbeat, Phase: phaseWaitHealthy},
				},
				doctorEvents: []progressEvent{
					{Kind: progressPhaseStarted, Phase: phaseWaitHealthy},
					{Kind: progressPhaseHeartbeat, Phase: phaseWaitHealthy},
				},
			}
			var stdout, stderr bytes.Buffer
			application := New(Options{
				Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
				IsTerminal: func(io.Writer) bool { return true },
			})

			if code := application.Run(context.Background(), test.args); code != exitOK {
				t.Fatalf("Run(%q) = %d, stderr = %q", test.args, code, stderr.String())
			}
			if stdout.Len() == 0 {
				t.Fatalf("Run(%q) produced no result", test.args)
			}
			if stderr.Len() != 0 {
				t.Fatalf("Run(%q) progress stderr = %q, want empty", test.args, stderr.String())
			}
		})
	}
}

func TestProgressOperationAllowsOnlyCommandsWithVisibleWork(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"upgrade", "--to", "2.8.0-rnl.2"}, want: "upgrade"},
		{args: []string{"config", "apply"}, want: "config apply"},
		{args: []string{"secret", "set"}, want: "secret set"},
		{args: []string{"status"}},
		{args: []string{"overview"}},
		{args: []string{"doctor", "--json"}},
		{args: []string{"config", "show"}},
		{args: []string{"logs", "node"}},
	}
	for _, test := range tests {
		if got := progressOperation(test.args); got != test.want {
			t.Fatalf("progressOperation(%q) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestAppQuietSuppressesProgressButNotRuntimeErrors(t *testing.T) {
	lifecycle := &progressTestLifecycle{
		fakeLifecycle: &fakeLifecycle{err: errors.New("service unavailable")},
		restartEvents: []progressEvent{{Kind: progressPhaseHeartbeat, Phase: phaseWaitHealthy}},
	}
	var stdout, stderr bytes.Buffer
	application := New(Options{
		Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
		IsTerminal: func(io.Writer) bool { return true },
	})
	if code := application.Run(context.Background(), []string{"restart", "--quiet"}); code != exitFailure {
		t.Fatalf("quiet restart = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("quiet error stdout = %q, want empty", stdout.String())
	}
	if stderr.String() != "rnlctl: restart: service unavailable\n" {
		t.Fatalf("quiet error stderr = %q", stderr.String())
	}
}

func TestAppProgressDoesNotExposeSecretFileNameOrContents(t *testing.T) {
	const sentinel = "secret-sentinel-must-not-appear"
	secretFile := filepath.Join(t.TempDir(), sentinel+".key")
	if err := os.WriteFile(secretFile, []byte(sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := &progressTestLifecycle{
		fakeLifecycle: &fakeLifecycle{result: Result{Operation: "secret set", Changed: true}},
		secretEvents: []progressEvent{
			{Kind: progressPhaseStarted, Phase: phaseWriteConfiguration},
			{Kind: progressPhaseHeartbeat, Phase: phaseRestartService},
		},
	}
	var stdout, stderr bytes.Buffer
	application := New(Options{
		Lifecycle: lifecycle, Stdout: &stdout, Stderr: &stderr,
		IsTerminal:    func(io.Writer) bool { return true },
		TerminalWidth: func(io.Writer) int { return 64 },
		Now:           newProgressTestClock().Now,
	})
	args := []string{"secret", "set", "--file", secretFile, "--apply"}
	if code := application.Run(context.Background(), args); code != exitOK {
		t.Fatalf("Run(%q) = %d, stderr = %q", args, code, stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, sentinel) || strings.Contains(combined, secretFile) {
		t.Fatalf("command output exposed Secret material or source path: %q", combined)
	}
}
