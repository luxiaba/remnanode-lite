package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const hostExecutorHelperEnv = "GO_WANT_RNLCTL_HOST_EXECUTOR_HELPER"

func TestOSCommandExecutorHelper(_ *testing.T) {
	switch os.Getenv(hostExecutorHelperEnv) {
	case "failure":
		_, _ = fmt.Fprintln(os.Stdout, "OpenRC failed to start remnanode-lite")
		_, _ = fmt.Fprintln(os.Stderr, "missing cgroup controller memory.max")
		os.Exit(19)
	case "large-failure":
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("x", maxHostCommandOutput+1024))
		os.Exit(23)
	}
}

type executorCall struct {
	name string
	args []string
}

type recordingExecutor struct {
	calls   []executorCall
	handler func(string, []string) ([]byte, error)
}

func TestOSCommandExecutorIncludesBoundedFailureDiagnostics(t *testing.T) {
	t.Setenv(hostExecutorHelperEnv, "failure")
	output, err := (OSCommandExecutor{}).Run(
		context.Background(), os.Args[0], "-test.run=^TestOSCommandExecutorHelper$",
	)
	if err == nil {
		t.Fatal("Run() accepted a failing command")
	}
	for _, want := range []string{filepath.Base(os.Args[0]), "exit status 19", "OpenRC failed", "missing cgroup controller memory.max"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Run() error does not contain %q: %v", want, err)
		}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 19 {
		t.Fatalf("Run() error does not preserve ExitError: %T, %v", err, err)
	}
	if len(output) == 0 || len(output) > maxHostCommandOutput {
		t.Fatalf("Run() output length = %d", len(output))
	}

	t.Setenv(hostExecutorHelperEnv, "large-failure")
	output, err = (OSCommandExecutor{}).Run(
		context.Background(), os.Args[0], "-test.run=^TestOSCommandExecutorHelper$",
	)
	if err == nil {
		t.Fatal("Run() accepted a failing command with large output")
	}
	if len(output) != maxHostCommandOutput {
		t.Fatalf("bounded Run() output length = %d, want %d", len(output), maxHostCommandOutput)
	}
}

func (executor *recordingExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	copied := append([]string(nil), args...)
	executor.calls = append(executor.calls, executorCall{name: name, args: copied})
	if executor.handler != nil {
		return executor.handler(name, copied)
	}
	return nil, nil
}

func TestLinuxHostSystemdServiceMutations(t *testing.T) {
	executor := &recordingExecutor{}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{"systemctl": "/usr/bin/systemctl"}),
		PathExists: func(path string) bool {
			return path == systemdRuntime
		},
	})

	if err := host.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := host.SetActive(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := host.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.SetActive(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := host.SetEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	want := []executorCall{
		{name: "/usr/bin/systemctl", args: []string{"enable", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"reset-failed", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"start", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"reset-failed", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"restart", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"stop", "remnanode-lite.service"}},
		{name: "/usr/bin/systemctl", args: []string{"disable", "remnanode-lite.service"}},
	}
	if !reflect.DeepEqual(executor.calls, want) {
		t.Fatalf("commands = %#v, want %#v", executor.calls, want)
	}
}

func TestLinuxHostOpenRCServiceMutations(t *testing.T) {
	executor := &recordingExecutor{}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{
			"rc-update":  "/sbin/rc-update",
			"rc-service": "/sbin/rc-service",
		}),
	})

	if err := host.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := host.SetActive(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := host.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.SetActive(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := host.SetEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	want := []executorCall{
		{name: "/sbin/rc-update", args: []string{"add", "remnanode-lite", "default"}},
		{name: "/sbin/rc-service", args: []string{"remnanode-lite", "start"}},
		{name: "/sbin/rc-service", args: []string{"remnanode-lite", "restart"}},
		{name: "/sbin/rc-service", args: []string{"remnanode-lite", "stop"}},
		{name: "/sbin/rc-update", args: []string{"del", "remnanode-lite", "default"}},
	}
	if !reflect.DeepEqual(executor.calls, want) {
		t.Fatalf("commands = %#v, want %#v", executor.calls, want)
	}
}

func TestLinuxHostOpenRCPreflightDoesNotRequireOpenRCRunShellHelpers(t *testing.T) {
	host := NewLinuxHost(LinuxHostOptions{
		LookPath: executableFinder(map[string]string{
			"rc-update":        "/sbin/rc-update",
			"rc-service":       "/sbin/rc-service",
			"supervise-daemon": "/sbin/supervise-daemon",
			"useradd":          "/usr/sbin/useradd",
			"userdel":          "/usr/sbin/userdel",
			"groupadd":         "/usr/sbin/groupadd",
			"groupdel":         "/usr/sbin/groupdel",
		}),
	})

	// Alpine exposes its internal checkpath helper only while openrc-run
	// evaluates a service. It is intentionally absent from the normal PATH.
	if err := host.Preflight(context.Background(), false, PathsAt(t.TempDir())); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestLinuxHostOpenRCPreflightNamesAlpineRuntimePackages(t *testing.T) {
	host := NewLinuxHost(LinuxHostOptions{
		LookPath: executableFinder(map[string]string{
			"rc-update":        "/sbin/rc-update",
			"rc-service":       "/sbin/rc-service",
			"supervise-daemon": "/sbin/supervise-daemon",
			"useradd":          "/usr/sbin/useradd",
			"userdel":          "/usr/sbin/userdel",
			"groupadd":         "/usr/sbin/groupadd",
			"groupdel":         "/usr/sbin/groupdel",
		}),
		PathExists: func(path string) bool {
			return path == "/sys/fs/cgroup/cgroup.controllers"
		},
	})

	err := host.Preflight(context.Background(), true, PathsAt(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "Alpine: apk add nftables iproute2") {
		t.Fatalf("Preflight() error = %v, want Alpine runtime package guidance", err)
	}
}

func TestLinuxHostRemoveServiceRemovesOnlyManagedSystemdFiles(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dropInDirectory string)
		verify  func(t *testing.T, dropInDirectory string)
	}{
		{
			name: "empty managed drop-in directory is removed",
			prepare: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				if err := os.MkdirAll(dropInDirectory, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				if _, err := os.Lstat(dropInDirectory); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("empty managed drop-in directory remains: %v", err)
				}
			},
		},
		{
			name: "local drop-in is retained",
			prepare: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				if err := os.MkdirAll(dropInDirectory, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dropInDirectory, "90-local.conf"), []byte("[Service]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dropInDirectory, "90-local.conf"))
				if err != nil || string(data) != "[Service]\n" {
					t.Fatalf("local drop-in = %q, %v", data, err)
				}
			},
		},
		{
			name: "symlinked drop-in parent is retained without touching target",
			prepare: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(dropInDirectory), "external-drop-ins")
				if err := os.MkdirAll(target, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, content := range map[string]string{
					"20-remnanode-lite-hardening.conf": "managed-looking\n",
					"sentinel.conf":                    "leave me\n",
				} {
					if err := os.WriteFile(filepath.Join(target, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, dropInDirectory); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				info, err := os.Lstat(dropInDirectory)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("drop-in parent = %#v, %v", info, err)
				}
				target, err := os.Readlink(dropInDirectory)
				if err != nil {
					t.Fatal(err)
				}
				for name, want := range map[string]string{
					"20-remnanode-lite-hardening.conf": "managed-looking\n",
					"sentinel.conf":                    "leave me\n",
				} {
					data, readErr := os.ReadFile(filepath.Join(target, name))
					if readErr != nil || string(data) != want {
						t.Fatalf("symlink target %s = %q, %v", name, data, readErr)
					}
				}
			},
		},
		{
			name: "regular-file drop-in parent is retained",
			prepare: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				if err := os.WriteFile(dropInDirectory, []byte("administrator-owned\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, dropInDirectory string) {
				t.Helper()
				data, err := os.ReadFile(dropInDirectory)
				if err != nil || string(data) != "administrator-owned\n" {
					t.Fatalf("drop-in parent = %q, %v", data, err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := PathsAt(t.TempDir())
			if err := os.MkdirAll(filepath.Dir(paths.SystemdUnit), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.SystemdUnit, []byte("managed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			dropInDirectory := filepath.Dir(paths.SystemdDropIn)
			if err := os.MkdirAll(filepath.Dir(dropInDirectory), 0o755); err != nil {
				t.Fatal(err)
			}
			test.prepare(t, dropInDirectory)
			if info, err := os.Lstat(dropInDirectory); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				if err := os.WriteFile(paths.SystemdDropIn, []byte("managed\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			executor := &recordingExecutor{}
			host := NewLinuxHost(LinuxHostOptions{
				Executor: executor,
				LookPath: executableFinder(map[string]string{"systemctl": "/usr/bin/systemctl"}),
				PathExists: func(path string) bool {
					return path == systemdRuntime
				},
			})
			for attempt := 0; attempt < 2; attempt++ {
				if err := host.RemoveService(context.Background(), paths); err != nil {
					t.Fatal(err)
				}
			}

			for _, path := range []string{paths.SystemdUnit} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("managed path %s remains: %v", path, err)
				}
			}
			test.verify(t, dropInDirectory)

			want := []executorCall{
				{name: "/usr/bin/systemctl", args: []string{"daemon-reload"}},
				{name: "/usr/bin/systemctl", args: []string{"daemon-reload"}},
			}
			if !reflect.DeepEqual(executor.calls, want) {
				t.Fatalf("commands = %#v, want %#v", executor.calls, want)
			}
		})
	}
}

func TestLinuxHostQueriesServiceStatus(t *testing.T) {
	tests := []struct {
		name     string
		paths    map[string]string
		exists   PathExistsFunc
		handler  func(string, []string) ([]byte, error)
		want     ServiceStatus
		commands []executorCall
	}{
		{
			name:  "systemd",
			paths: map[string]string{"systemctl": "/usr/bin/systemctl"},
			exists: func(path string) bool {
				return path == systemdRuntime
			},
			want: ServiceStatus{Manager: "systemd", Enabled: true, Active: true},
			commands: []executorCall{
				{name: "/usr/bin/systemctl", args: []string{"is-enabled", "--quiet", "remnanode-lite.service"}},
				{name: "/usr/bin/systemctl", args: []string{"is-active", "--quiet", "remnanode-lite.service"}},
			},
		},
		{
			name:  "openrc",
			paths: map[string]string{"rc-update": "/sbin/rc-update", "rc-service": "/sbin/rc-service"},
			handler: func(name string, args []string) ([]byte, error) {
				if name == "/sbin/rc-update" && reflect.DeepEqual(args, []string{"-q", "show", "default"}) {
					return []byte("networking remnanode-lite\n"), nil
				}
				return nil, nil
			},
			want: ServiceStatus{Manager: "openrc", Enabled: true, Active: true},
			commands: []executorCall{
				{name: "/sbin/rc-update", args: []string{"-q", "show", "default"}},
				{name: "/sbin/rc-service", args: []string{"remnanode-lite", "status"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingExecutor{handler: test.handler}
			host := NewLinuxHost(LinuxHostOptions{Executor: executor, LookPath: executableFinder(test.paths), PathExists: test.exists})
			got, err := host.ServiceStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ServiceStatus() = %#v, want %#v", got, test.want)
			}
			if !reflect.DeepEqual(executor.calls, test.commands) {
				t.Fatalf("commands = %#v, want %#v", executor.calls, test.commands)
			}
		})
	}
}

func TestLinuxHostChoosesRunningServiceManager(t *testing.T) {
	tests := []struct {
		name   string
		paths  map[string]string
		exists PathExistsFunc
		want   serviceManager
	}{
		{
			name:  "systemd runtime wins",
			paths: map[string]string{"systemctl": "/usr/bin/systemctl", "rc-update": "/sbin/rc-update"},
			exists: func(path string) bool {
				return path == systemdRuntime
			},
			want: serviceManager{kind: serviceManagerSystemd, executable: "/usr/bin/systemctl"},
		},
		{
			name:   "openrc wins without systemd runtime",
			paths:  map[string]string{"systemctl": "/usr/bin/systemctl", "rc-update": "/sbin/rc-update"},
			exists: func(string) bool { return false },
			want:   serviceManager{kind: serviceManagerOpenRC, executable: "/sbin/rc-update"},
		},
		{
			name:   "standalone systemctl remains supported",
			paths:  map[string]string{"systemctl": "/usr/bin/systemctl"},
			exists: func(string) bool { return false },
			want:   serviceManager{kind: serviceManagerSystemd, executable: "/usr/bin/systemctl"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := NewLinuxHost(LinuxHostOptions{LookPath: executableFinder(test.paths), PathExists: test.exists})
			manager, err := host.manager()
			if err != nil {
				t.Fatal(err)
			}
			if manager != test.want {
				t.Fatalf("manager = %#v, want %#v", manager, test.want)
			}
		})
	}
}

func TestLinuxHostResetFailureStopsActivation(t *testing.T) {
	executor := &recordingExecutor{handler: func(_ string, args []string) ([]byte, error) {
		if len(args) > 0 && args[0] == "reset-failed" {
			return nil, errors.New("reset rejected")
		}
		return nil, nil
	}}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{"systemctl": "/usr/bin/systemctl"}),
		PathExists: func(path string) bool {
			return path == systemdRuntime
		},
	})

	if err := host.SetActive(context.Background(), true); err == nil || !strings.Contains(err.Error(), "reset service start-rate state") {
		t.Fatalf("SetActive(true) error = %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].args[0] != "reset-failed" {
		t.Fatalf("commands = %#v", executor.calls)
	}
}

func TestLinuxHostCleansUpPartiallyCreatedAccount(t *testing.T) {
	tests := []struct {
		name         string
		userCreated  bool
		groupCreated bool
		want         []executorCall
	}{
		{
			name: "group created before useradd failure", groupCreated: true,
			want: []executorCall{{name: "/usr/sbin/groupdel", args: []string{managedAccountName}}},
		},
		{
			name: "user created before lookup failure", userCreated: true, groupCreated: true,
			want: []executorCall{
				{name: "/usr/sbin/userdel", args: []string{managedAccountName}},
				{name: "/usr/sbin/groupdel", args: []string{managedAccountName}},
			},
		},
		{name: "preexisting account surface"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingExecutor{}
			host := NewLinuxHost(LinuxHostOptions{
				Executor: executor,
				LookPath: executableFinder(map[string]string{
					"userdel": "/usr/sbin/userdel", "groupdel": "/usr/sbin/groupdel",
				}),
			})
			if err := host.cleanupCreatedAccount(context.Background(), test.userCreated, test.groupCreated); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(executor.calls, test.want) {
				t.Fatalf("cleanup commands = %#v, want %#v", executor.calls, test.want)
			}
		})
	}
}

func TestLinuxHostReportsAccountCleanupFailures(t *testing.T) {
	executor := &recordingExecutor{handler: func(_ string, args []string) ([]byte, error) {
		return nil, fmt.Errorf("%s rejected", args[0])
	}}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{
			"userdel": "/usr/sbin/userdel", "groupdel": "/usr/sbin/groupdel",
		}),
	})
	err := host.cleanupCreatedAccount(context.Background(), true, true)
	if err == nil || !strings.Contains(err.Error(), "roll back "+managedAccountName+" account") || !strings.Contains(err.Error(), "roll back "+managedAccountName+" group") {
		t.Fatalf("cleanupCreatedAccount() error = %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("cleanup commands = %#v", executor.calls)
	}
}

func TestNativeServiceTemplatesUseManagedAccount(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		required []string
	}{
		{
			name: "systemd",
			file: "remnanode-lite.service",
			required: []string{
				"User=" + managedAccountName,
				"Group=" + managedAccountName,
				"USER=" + managedAccountName,
				"LOGNAME=" + managedAccountName,
			},
		},
		{
			name: "openrc",
			file: "remnanode-lite.openrc",
			required: []string{
				"need cgroups net",
				"command_user=\"" + managedAccountName + ":" + managedAccountName + "\"",
				"USER=" + managedAccountName,
				"LOGNAME=" + managedAccountName,
				"checkpath -d -o " + managedAccountName + ":" + managedAccountName,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join("..", "..", "deploy", test.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range test.required {
				if !strings.Contains(string(contents), required) {
					t.Fatalf("%s does not contain %q", test.file, required)
				}
			}
			for _, stale := range []string{
				"User=remnanode\n",
				"Group=remnanode\n",
				"USER=remnanode ",
				"LOGNAME=remnanode ",
				`command_user="remnanode:remnanode"`,
				"checkpath -d -o remnanode:remnanode",
			} {
				if strings.Contains(string(contents), stale) {
					t.Fatalf("%s still contains stale account field %q", test.file, stale)
				}
			}
		})
	}
}

func TestLinuxHostValidatesReportedNodeVersion(t *testing.T) {
	tests := []struct {
		output  string
		wantErr bool
	}{
		{output: "remnanode-lite 2.8.0-rnl.1 (contract 2.8.0)\n"},
		{output: "remnanode-lite 2.8.0-rnl.1\n", wantErr: true},
		{output: "remnanode-lite 2.8.0\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(strings.TrimSpace(test.output), func(t *testing.T) {
			executor := &recordingExecutor{handler: func(string, []string) ([]byte, error) {
				return []byte(test.output), nil
			}}
			host := NewLinuxHost(LinuxHostOptions{Executor: executor})
			err := host.ValidateBinary(context.Background(), "/generation/bin/remnanode-lite", "2.8.0-rnl.1", "2.8.0")
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBinary() error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestLinuxHostWaitHealthyUsesExplicitSocketAndStableProbes(t *testing.T) {
	executor := &recordingExecutor{}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{"systemctl": "/usr/bin/systemctl"}),
		PathExists: func(path string) bool {
			return path == systemdRuntime
		},
	})
	if err := host.WaitHealthy(context.Background(), "/usr/local/bin/remnanode-lite", "/run/remnanode-lite/internal.sock", 3*time.Second); err != nil {
		t.Fatalf("WaitHealthy() error = %v", err)
	}
	var probes []executorCall
	for _, call := range executor.calls {
		if call.name == "/usr/local/bin/remnanode-lite" {
			probes = append(probes, call)
		}
	}
	if len(probes) != 2 {
		t.Fatalf("healthcheck probes = %#v, want two stable probes", probes)
	}
	wantArgs := []string{"healthcheck", "--socket", "/run/remnanode-lite/internal.sock"}
	for _, probe := range probes {
		if !reflect.DeepEqual(probe.args, wantArgs) {
			t.Fatalf("healthcheck args = %#v, want %#v", probe.args, wantArgs)
		}
	}
}

func TestLinuxHostWaitHealthyRequiresConsecutiveSuccesses(t *testing.T) {
	healthChecks := 0
	executor := &recordingExecutor{handler: func(name string, args []string) ([]byte, error) {
		if name == "/usr/local/bin/remnanode-lite" && reflect.DeepEqual(args,
			[]string{"healthcheck", "--socket", "/run/remnanode-lite/internal.sock"}) {
			healthChecks++
			if healthChecks == 2 {
				return nil, errors.New("transient readiness failure")
			}
		}
		return nil, nil
	}}
	host := NewLinuxHost(LinuxHostOptions{
		Executor: executor,
		LookPath: executableFinder(map[string]string{"systemctl": "/usr/bin/systemctl"}),
		PathExists: func(path string) bool {
			return path == systemdRuntime
		},
	})
	if err := host.WaitHealthy(context.Background(), "/usr/local/bin/remnanode-lite", "/run/remnanode-lite/internal.sock", 4*time.Second); err != nil {
		t.Fatalf("WaitHealthy() error = %v", err)
	}
	if healthChecks != 4 {
		t.Fatalf("healthcheck probes = %d, want success, failure, success, success", healthChecks)
	}
}

func TestLinuxHostSystemdHardeningVersionGate(t *testing.T) {
	for _, test := range []struct {
		output string
		want   bool
	}{
		{output: "systemd 239 (239-82.el8)\n", want: false},
		{output: "systemd 247 (247.3-7)\n", want: true},
		{output: "systemd 256 (256.9)\n", want: true},
		{output: "not-systemd\n", want: false},
	} {
		t.Run(fmt.Sprintf("want_%t_%s", test.want, strings.Fields(test.output)[0]), func(t *testing.T) {
			executor := &recordingExecutor{handler: func(string, []string) ([]byte, error) {
				return []byte(test.output), nil
			}}
			host := NewLinuxHost(LinuxHostOptions{Executor: executor})
			if got := host.supportsModernSystemd(context.Background(), "/usr/bin/systemctl"); got != test.want {
				t.Fatalf("supportsModernSystemd(%q) = %t, want %t", test.output, got, test.want)
			}
		})
	}
}
