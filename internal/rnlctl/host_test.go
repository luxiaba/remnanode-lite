package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type executorCall struct {
	name string
	args []string
}

type recordingExecutor struct {
	calls   []executorCall
	handler func(string, []string) ([]byte, error)
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
		want   serviceManagerKind
	}{
		{
			name:  "systemd runtime wins",
			paths: map[string]string{"systemctl": "/usr/bin/systemctl", "rc-update": "/sbin/rc-update"},
			exists: func(path string) bool {
				return path == systemdRuntime
			},
			want: serviceManagerSystemd,
		},
		{
			name:   "openrc wins without systemd runtime",
			paths:  map[string]string{"systemctl": "/usr/bin/systemctl", "rc-update": "/sbin/rc-update"},
			exists: func(string) bool { return false },
			want:   serviceManagerOpenRC,
		},
		{
			name:   "standalone systemctl remains supported",
			paths:  map[string]string{"systemctl": "/usr/bin/systemctl"},
			exists: func(string) bool { return false },
			want:   serviceManagerSystemd,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := NewLinuxHost(LinuxHostOptions{LookPath: executableFinder(test.paths), PathExists: test.exists})
			manager, err := host.manager()
			if err != nil {
				t.Fatal(err)
			}
			if manager.kind != test.want {
				t.Fatalf("manager = %#v, want kind %v", manager, test.want)
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
