package rnlctl

import "fmt"

const (
	systemdService = "remnanode-lite.service"
	openRCService  = "remnanode-lite"
	systemdRuntime = "/run/systemd/system"
)

type serviceManagerKind uint8

const (
	serviceManagerSystemd serviceManagerKind = iota + 1
	serviceManagerOpenRC
)

type serviceManager struct {
	kind       serviceManagerKind
	executable string
}

func (a *App) detectServiceManager() (serviceManager, error) {
	systemctl := a.findExecutable("systemctl")
	rcService := a.findExecutable("rc-service")
	if manager, ok := chooseServiceManager(systemctl, rcService, a.pathExists(systemdRuntime)); ok {
		return manager, nil
	}
	return serviceManager{}, fmt.Errorf("neither systemctl nor rc-service is available")
}

func chooseServiceManager(systemctl, openRCExecutable string, systemdRuntimePresent bool) (serviceManager, bool) {
	switch {
	case systemctl != "" && systemdRuntimePresent:
		return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, true
	case openRCExecutable != "":
		return serviceManager{kind: serviceManagerOpenRC, executable: openRCExecutable}, true
	case systemctl != "":
		return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, true
	default:
		return serviceManager{}, false
	}
}
