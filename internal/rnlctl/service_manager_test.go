package rnlctl

import "testing"

func TestChooseServiceManager(t *testing.T) {
	tests := []struct {
		name           string
		systemctl      string
		openRC         string
		systemdRuntime bool
		want           serviceManager
		wantAvailable  bool
	}{
		{
			name: "systemd only", systemctl: "/usr/bin/systemctl",
			want: serviceManager{kind: serviceManagerSystemd, executable: "/usr/bin/systemctl"}, wantAvailable: true,
		},
		{
			name: "openrc only", openRC: "/sbin/rc-service",
			want: serviceManager{kind: serviceManagerOpenRC, executable: "/sbin/rc-service"}, wantAvailable: true,
		},
		{
			name: "systemd runtime wins", systemctl: "/usr/bin/systemctl", openRC: "/sbin/rc-service", systemdRuntime: true,
			want: serviceManager{kind: serviceManagerSystemd, executable: "/usr/bin/systemctl"}, wantAvailable: true,
		},
		{
			name: "openrc wins without systemd runtime", systemctl: "/usr/bin/systemctl", openRC: "/sbin/rc-service",
			want: serviceManager{kind: serviceManagerOpenRC, executable: "/sbin/rc-service"}, wantAvailable: true,
		},
		{name: "neither manager", want: serviceManager{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, available := chooseServiceManager(test.systemctl, test.openRC, test.systemdRuntime)
			if manager != test.want || available != test.wantAvailable {
				t.Fatalf("chooseServiceManager() = (%#v, %t), want (%#v, %t)", manager, available, test.want, test.wantAvailable)
			}
		})
	}
}

func TestAppDetectServiceManagerUsesRuntimePolicy(t *testing.T) {
	tests := []struct {
		name   string
		exists PathExistsFunc
		want   serviceManager
	}{
		{
			name:   "systemd runtime",
			exists: func(path string) bool { return path == systemdRuntime },
			want:   serviceManager{kind: serviceManagerSystemd, executable: "/usr/bin/systemctl"},
		},
		{
			name:   "no runtime marker",
			exists: func(string) bool { return false },
			want:   serviceManager{kind: serviceManagerOpenRC, executable: "/sbin/rc-service"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := New(Options{
				LookPath: executableFinder(map[string]string{
					"systemctl":  "/usr/bin/systemctl",
					"rc-service": "/sbin/rc-service",
				}),
				PathExists: test.exists,
			})
			manager, err := app.detectServiceManager()
			if err != nil {
				t.Fatal(err)
			}
			if manager != test.want {
				t.Fatalf("manager = %#v, want %#v", manager, test.want)
			}
		})
	}
}
