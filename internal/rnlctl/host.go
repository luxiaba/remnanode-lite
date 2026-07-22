package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxHostCommandOutput = 64 << 10
	managedAccountName   = "remnanode-lite"
)

type CommandExecutor interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type OSCommandExecutor struct{}

func (OSCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var output limitedWriter
	output.remaining = maxHostCommandOutput
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if err != nil {
		return output.Bytes(), fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return output.Bytes(), nil
}

type limitedWriter struct {
	bytes.Buffer
	remaining int
}

func (writer *limitedWriter) Write(data []byte) (int, error) {
	original := len(data)
	if writer.remaining > 0 {
		keep := len(data)
		if keep > writer.remaining {
			keep = writer.remaining
		}
		_, _ = writer.Buffer.Write(data[:keep])
		writer.remaining -= keep
	}
	return original, nil
}

type LinuxHostOptions struct {
	Executor   CommandExecutor
	LookPath   LookPathFunc
	PathExists PathExistsFunc
}

type LinuxHost struct {
	executor   CommandExecutor
	lookPath   LookPathFunc
	pathExists PathExistsFunc
}

func NewLinuxHost(options LinuxHostOptions) *LinuxHost {
	if options.Executor == nil {
		options.Executor = OSCommandExecutor{}
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.PathExists == nil {
		options.PathExists = pathExists
	}
	return &LinuxHost{executor: options.Executor, lookPath: options.LookPath, pathExists: options.PathExists}
}

// Preflight is deliberately read-only. The lifecycle engine invokes it before
// acquiring its on-disk lock so a missing host dependency leaves no managed
// files behind.
func (host *LinuxHost) Preflight(_ context.Context, activating bool, paths Paths) error {
	manager, err := host.manager()
	if err != nil {
		return err
	}
	if manager.kind == serviceManagerOpenRC {
		for _, name := range []string{"rc-service", "supervise-daemon", "checkpath"} {
			if _, err := host.requireExecutable(name); err != nil {
				return fmt.Errorf("OpenRC experimental support: %w", err)
			}
		}
		if activating && !host.pathExists("/sys/fs/cgroup/cgroup.controllers") && !host.pathExists("/sys/fs/cgroup/unified/cgroup.controllers") {
			return fmt.Errorf("OpenRC experimental support requires cgroup v2")
		}
	}
	if account, err := user.Lookup(managedAccountName); err == nil {
		if _, identityErr := validateManagedAccount(account, paths.ApplicationState, false, false); identityErr != nil {
			return identityErr
		}
	} else {
		if _, unknown := err.(user.UnknownUserError); !unknown {
			return fmt.Errorf("look up %s account: %w", managedAccountName, err)
		}
		commands := []string{"useradd", "userdel"}
		if _, groupErr := user.LookupGroup(managedAccountName); groupErr != nil {
			if _, unknownGroup := groupErr.(user.UnknownGroupError); !unknownGroup {
				return fmt.Errorf("look up %s group: %w", managedAccountName, groupErr)
			}
			commands = append(commands, "groupadd", "groupdel")
		}
		for _, name := range commands {
			if _, findErr := host.requireExecutable(name); findErr != nil {
				return findErr
			}
		}
	}
	if activating {
		var missing []string
		for _, name := range []string{"nft", "ss"} {
			if host.findExecutable(name) == "" {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("missing runtime commands %s; install them first (Rocky: dnf install nftables iproute; Debian: apt install nftables iproute2)", strings.Join(missing, ", "))
		}
	}
	return nil
}

func (host *LinuxHost) Prepare(ctx context.Context, generationRoot string, paths Paths) (ManagedAccount, error) {
	for _, directory := range []string{paths.ApplicationState, paths.LogDirectory, paths.RuntimeDirectory} {
		if err := ensureDirectory(directory, 0o750); err != nil {
			return ManagedAccount{}, fmt.Errorf("prepare runtime directory %s: %w", directory, err)
		}
	}
	account, err := host.ensureAccount(ctx, paths.ApplicationState)
	if err != nil {
		return ManagedAccount{}, err
	}
	manager, err := host.manager()
	if err != nil {
		return account, err
	}
	support := filepath.Join(generationRoot, "support", "deploy")
	switch manager.kind {
	case serviceManagerSystemd:
		if err := atomicCopyFile(filepath.Join(support, "remnanode-lite.service"), paths.SystemdUnit, 0o644); err != nil {
			return account, fmt.Errorf("install systemd unit: %w", err)
		}
		if host.supportsModernSystemd(ctx, manager.executable) {
			if err := atomicCopyFile(filepath.Join(support, "remnanode-lite-hardening.conf"), paths.SystemdDropIn, 0o644); err != nil {
				return account, fmt.Errorf("install systemd hardening: %w", err)
			}
		} else if err := removeAndSync(paths.SystemdDropIn); err != nil && !errors.Is(err, os.ErrNotExist) {
			return account, fmt.Errorf("remove unsupported systemd hardening: %w", err)
		}
		if _, err := host.executor.Run(ctx, manager.executable, "daemon-reload"); err != nil {
			return account, fmt.Errorf("reload systemd: %w", err)
		}
	case serviceManagerOpenRC:
		if err := atomicCopyFile(filepath.Join(support, "remnanode-lite.openrc"), paths.OpenRCUnit, 0o755); err != nil {
			return account, fmt.Errorf("install OpenRC service: %w", err)
		}
	}
	if err := host.ApplyOwnership(ctx, paths); err != nil {
		return account, err
	}
	return account, nil
}

func (host *LinuxHost) RemoveService(ctx context.Context, paths Paths) error {
	manager, detectErr := host.manager()
	var errs []error
	for _, target := range []string{paths.SystemdUnit, paths.SystemdDropIn, paths.OpenRCUnit} {
		if err := removeAndSync(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if detectErr == nil && manager.kind == serviceManagerSystemd {
		if _, err := host.executor.Run(ctx, manager.executable, "daemon-reload"); err != nil {
			errs = append(errs, fmt.Errorf("reload systemd: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (host *LinuxHost) RemoveAccount(ctx context.Context, expected ManagedAccount) error {
	if !expected.UserCreated && !expected.GroupCreated {
		return nil
	}
	account, err := user.Lookup(managedAccountName)
	if err != nil {
		if _, unknown := err.(user.UnknownUserError); unknown {
			return nil
		}
		return err
	}
	actual, err := validateManagedAccount(account, expected.Home, expected.UserCreated, expected.GroupCreated)
	if err != nil {
		return err
	}
	if actual.UID != expected.UID || actual.GID != expected.GID || actual.Shell != expected.Shell {
		return fmt.Errorf("refusing to remove %s account because its identity changed", managedAccountName)
	}
	userdel, err := host.requireExecutable("userdel")
	if err != nil {
		return err
	}
	if expected.UserCreated {
		if _, err := host.executor.Run(ctx, userdel, managedAccountName); err != nil {
			return fmt.Errorf("remove %s account: %w", managedAccountName, err)
		}
	}
	group, err := user.LookupGroup(managedAccountName)
	if expected.GroupCreated && err == nil && group.Gid == expected.GID {
		groupdel, findErr := host.requireExecutable("groupdel")
		if findErr != nil {
			return findErr
		}
		if _, deleteErr := host.executor.Run(ctx, groupdel, managedAccountName); deleteErr != nil {
			return fmt.Errorf("remove %s group: %w", managedAccountName, deleteErr)
		}
	}
	return nil
}

func (host *LinuxHost) PreflightRemoveAccount(_ context.Context, expected ManagedAccount) error {
	if !expected.UserCreated && !expected.GroupCreated {
		return nil
	}
	account, err := user.Lookup(managedAccountName)
	if err != nil {
		if _, unknown := err.(user.UnknownUserError); unknown {
			return nil
		}
		return err
	}
	actual, err := validateManagedAccount(account, expected.Home, expected.UserCreated, expected.GroupCreated)
	if err != nil {
		return err
	}
	if actual.UID != expected.UID || actual.GID != expected.GID || actual.Shell != expected.Shell {
		return fmt.Errorf("refusing to remove %s account because its identity changed", managedAccountName)
	}
	var commands []string
	if expected.UserCreated {
		commands = append(commands, "userdel")
	}
	if expected.GroupCreated {
		commands = append(commands, "groupdel")
	}
	for _, command := range commands {
		if _, err := host.requireExecutable(command); err != nil {
			return err
		}
	}
	return nil
}

func (host *LinuxHost) ServiceStatus(ctx context.Context) (ServiceStatus, error) {
	manager, err := host.manager()
	if err != nil {
		return ServiceStatus{}, err
	}
	status := ServiceStatus{Manager: managerName(manager.kind)}
	switch manager.kind {
	case serviceManagerSystemd:
		status.Enabled, err = host.commandSuccess(ctx, manager.executable, "is-enabled", "--quiet", systemdService)
		if err != nil {
			return ServiceStatus{}, err
		}
		status.Active, err = host.commandSuccess(ctx, manager.executable, "is-active", "--quiet", systemdService)
	case serviceManagerOpenRC:
		var output []byte
		output, err = host.executor.Run(ctx, manager.executable, "-q", "show", "default")
		if err == nil {
			for _, field := range strings.Fields(string(output)) {
				if field == openRCService {
					status.Enabled = true
					break
				}
			}
		}
		if err != nil {
			return ServiceStatus{}, fmt.Errorf("query OpenRC enabled state: %w", err)
		}
		rcService, findErr := host.requireExecutable("rc-service")
		if findErr != nil {
			return ServiceStatus{}, findErr
		}
		status.Active, err = host.commandSuccess(ctx, rcService, openRCService, "status")
	}
	return status, err
}

func (host *LinuxHost) SetEnabled(ctx context.Context, enabled bool) error {
	manager, err := host.manager()
	if err != nil {
		return err
	}
	switch manager.kind {
	case serviceManagerSystemd:
		action := "disable"
		if enabled {
			action = "enable"
		}
		_, err = host.executor.Run(ctx, manager.executable, action, systemdService)
	case serviceManagerOpenRC:
		action := "del"
		if enabled {
			action = "add"
		}
		_, err = host.executor.Run(ctx, manager.executable, action, openRCService, "default")
	}
	if err != nil {
		return fmt.Errorf("set service enabled=%t: %w", enabled, err)
	}
	return nil
}

func (host *LinuxHost) SetActive(ctx context.Context, active bool) error {
	manager, err := host.manager()
	if err != nil {
		return err
	}
	action := "stop"
	if active {
		action = "start"
	}
	name := manager.executable
	args := []string{action, systemdService}
	if manager.kind == serviceManagerSystemd && active {
		if _, err := host.executor.Run(ctx, manager.executable, "reset-failed", systemdService); err != nil {
			return fmt.Errorf("reset service start-rate state: %w", err)
		}
	}
	if manager.kind == serviceManagerOpenRC {
		name, err = host.requireExecutable("rc-service")
		if err != nil {
			return err
		}
		args = []string{openRCService, action}
	}
	if _, err := host.executor.Run(ctx, name, args...); err != nil {
		return fmt.Errorf("set service active=%t: %w", active, err)
	}
	return nil
}

func (host *LinuxHost) Restart(ctx context.Context) error {
	manager, err := host.manager()
	if err != nil {
		return err
	}
	name := manager.executable
	args := []string{"restart", systemdService}
	if manager.kind == serviceManagerSystemd {
		if _, err := host.executor.Run(ctx, manager.executable, "reset-failed", systemdService); err != nil {
			return fmt.Errorf("reset service start-rate state: %w", err)
		}
	}
	if manager.kind == serviceManagerOpenRC {
		name, err = host.requireExecutable("rc-service")
		if err != nil {
			return err
		}
		args = []string{openRCService, "restart"}
	}
	_, err = host.executor.Run(ctx, name, args...)
	return err
}

func (host *LinuxHost) ValidateBinary(ctx context.Context, binary, version, contractVersion string) error {
	output, err := host.executor.Run(ctx, binary, "version")
	if err != nil {
		return fmt.Errorf("validate node binary: %w", err)
	}
	want := fmt.Sprintf("remnanode-lite %s (contract %s)", version, contractVersion)
	line := strings.TrimSpace(string(output))
	if line != want {
		return fmt.Errorf("node binary reports %q, want %q", line, want)
	}
	return nil
}

func (host *LinuxHost) WaitHealthy(ctx context.Context, binary, socketPath string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	stableSuccesses := 0
	for {
		status, err := host.ServiceStatus(waitCtx)
		if err != nil {
			lastErr = err
			stableSuccesses = 0
		} else if !status.Active {
			lastErr = fmt.Errorf("service is not active")
			stableSuccesses = 0
		} else if _, err := host.executor.Run(waitCtx, binary, "healthcheck", "--socket", socketPath); err != nil {
			lastErr = err
			stableSuccesses = 0
		} else {
			stableSuccesses++
			if stableSuccesses >= 2 {
				return nil
			}
			lastErr = fmt.Errorf("healthcheck has not been stable for two consecutive probes")
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("service did not become healthy within %s: %w", timeout, lastErr)
		case <-ticker.C:
		}
	}
}

func (host *LinuxHost) ApplyOwnership(_ context.Context, paths Paths) error {
	account, err := user.Lookup(managedAccountName)
	if err != nil {
		return fmt.Errorf("look up %s account: %w", managedAccountName, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return fmt.Errorf("parse %s uid: %w", managedAccountName, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return fmt.Errorf("parse %s gid: %w", managedAccountName, err)
	}
	for _, directory := range []string{paths.ApplicationState, paths.LogDirectory, paths.RuntimeDirectory} {
		if err := os.Chown(directory, uid, gid); err != nil {
			return fmt.Errorf("set ownership on %s: %w", directory, err)
		}
	}
	for _, file := range []string{paths.ConfigDirectory, paths.EnvironmentFile, paths.SecretFile} {
		if _, statErr := os.Lstat(file); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return statErr
		}
		if err := os.Chown(file, 0, gid); err != nil {
			return fmt.Errorf("set ownership on %s: %w", file, err)
		}
	}
	if err := os.Chmod(paths.ConfigDirectory, 0o750); err != nil {
		return fmt.Errorf("set configuration directory mode: %w", err)
	}
	for _, file := range []string{paths.EnvironmentFile, paths.SecretFile} {
		if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Chmod(file, 0o640); err != nil {
			return fmt.Errorf("set permissions on %s: %w", file, err)
		}
	}
	if err := os.Chown(paths.ControlBinary, 0, 0); err != nil {
		return fmt.Errorf("set rnlctl ownership: %w", err)
	}
	if err := os.Chmod(paths.ControlBinary, 0o755); err != nil {
		return fmt.Errorf("set rnlctl permissions: %w", err)
	}
	return nil
}

func (host *LinuxHost) ensureAccount(ctx context.Context, home string) (ManagedAccount, error) {
	if account, err := user.Lookup(managedAccountName); err == nil {
		return validateManagedAccount(account, home, false, false)
	} else if _, unknown := err.(user.UnknownUserError); !unknown {
		return ManagedAccount{}, fmt.Errorf("look up %s account: %w", managedAccountName, err)
	}
	groupadd, err := host.requireExecutable("groupadd")
	if err != nil {
		return ManagedAccount{}, err
	}
	groupCreated := false
	if _, lookupErr := user.LookupGroup(managedAccountName); lookupErr != nil {
		if _, unknown := lookupErr.(user.UnknownGroupError); !unknown {
			return ManagedAccount{}, fmt.Errorf("look up %s group: %w", managedAccountName, lookupErr)
		}
		if _, err := host.executor.Run(ctx, groupadd, "--system", managedAccountName); err != nil {
			return ManagedAccount{}, fmt.Errorf("create %s group: %w", managedAccountName, err)
		}
		groupCreated = true
	}
	useradd, err := host.requireExecutable("useradd")
	if err != nil {
		return ManagedAccount{}, err
	}
	shell := "/usr/sbin/nologin"
	if !host.pathExists(shell) && host.pathExists("/sbin/nologin") {
		shell = "/sbin/nologin"
	}
	if _, err := host.executor.Run(ctx, useradd, "--system", "--gid", managedAccountName, "--home-dir", home, "--no-create-home", "--shell", shell, managedAccountName); err != nil {
		return ManagedAccount{}, errors.Join(
			fmt.Errorf("create %s account: %w", managedAccountName, err),
			host.cleanupCreatedAccount(ctx, false, groupCreated),
		)
	}
	account, err := user.Lookup(managedAccountName)
	if err != nil {
		return ManagedAccount{}, errors.Join(
			fmt.Errorf("look up newly created %s account: %w", managedAccountName, err),
			host.cleanupCreatedAccount(ctx, true, groupCreated),
		)
	}
	managed, err := validateManagedAccount(account, home, true, groupCreated)
	if err != nil {
		return ManagedAccount{}, errors.Join(err, host.cleanupCreatedAccount(ctx, true, groupCreated))
	}
	return managed, nil
}

func (host *LinuxHost) cleanupCreatedAccount(ctx context.Context, userCreated, groupCreated bool) error {
	var errs []error
	if userCreated {
		userdel, err := host.requireExecutable("userdel")
		if err != nil {
			errs = append(errs, err)
		} else if _, err := host.executor.Run(ctx, userdel, managedAccountName); err != nil {
			errs = append(errs, fmt.Errorf("roll back %s account: %w", managedAccountName, err))
		}
	}
	if groupCreated {
		groupdel, err := host.requireExecutable("groupdel")
		if err != nil {
			errs = append(errs, err)
		} else if _, err := host.executor.Run(ctx, groupdel, managedAccountName); err != nil {
			errs = append(errs, fmt.Errorf("roll back %s group: %w", managedAccountName, err))
		}
	}
	return errors.Join(errs...)
}

func validateManagedAccount(account *user.User, expectedHome string, userCreated, groupCreated bool) (ManagedAccount, error) {
	if account.Uid == "0" || account.Uid == "" || account.Gid == "" {
		return ManagedAccount{}, fmt.Errorf("%s must be a non-root system account", managedAccountName)
	}
	if account.HomeDir != expectedHome {
		return ManagedAccount{}, fmt.Errorf("%s home is %q, want %q", managedAccountName, account.HomeDir, expectedHome)
	}
	if filepath.Base(account.Username) != managedAccountName {
		return ManagedAccount{}, fmt.Errorf("unexpected %s account name %q", managedAccountName, account.Username)
	}
	group, err := user.LookupGroup(managedAccountName)
	if err != nil {
		return ManagedAccount{}, fmt.Errorf("look up %s group: %w", managedAccountName, err)
	}
	if group.Gid != account.Gid {
		return ManagedAccount{}, fmt.Errorf("%s primary gid %s does not match %s group gid %s", managedAccountName, account.Gid, managedAccountName, group.Gid)
	}
	shell := account.HomeDir
	// os/user does not expose the login shell on every platform. Read the
	// authoritative passwd entry on Linux and retain a conservative marker.
	if raw, err := os.ReadFile("/etc/passwd"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) == 7 && fields[0] == managedAccountName {
				shell = fields[6]
				break
			}
		}
	}
	if filepath.Base(shell) != "nologin" {
		return ManagedAccount{}, fmt.Errorf("%s login shell %q is not nologin", managedAccountName, shell)
	}
	return ManagedAccount{UserCreated: userCreated, GroupCreated: groupCreated, UID: account.Uid, GID: account.Gid, Home: account.HomeDir, Shell: shell}, nil
}

func (host *LinuxHost) manager() (serviceManager, error) {
	systemctl := host.findExecutable("systemctl")
	rcUpdate := host.findExecutable("rc-update")
	if systemctl != "" && (host.pathExists(systemdRuntime) || rcUpdate == "") {
		return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, nil
	}
	if rcUpdate != "" {
		return serviceManager{kind: serviceManagerOpenRC, executable: rcUpdate}, nil
	}
	if systemctl != "" {
		return serviceManager{kind: serviceManagerSystemd, executable: systemctl}, nil
	}
	return serviceManager{}, fmt.Errorf("neither systemd nor OpenRC is available")
}

func (host *LinuxHost) findExecutable(name string) string {
	value, err := host.lookPath(name)
	if err != nil {
		return ""
	}
	return value
}

func (host *LinuxHost) requireExecutable(name string) (string, error) {
	value := host.findExecutable(name)
	if value == "" {
		return "", fmt.Errorf("required command %q is unavailable", name)
	}
	return value, nil
}

func (host *LinuxHost) commandSuccess(ctx context.Context, name string, args ...string) (bool, error) {
	_, err := host.executor.Run(ctx, name, args...)
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() > 0 {
		return false, nil
	}
	return false, err
}

var systemdVersionPattern = regexp.MustCompile(`(?m)^systemd\s+([0-9]+)\b`)

func (host *LinuxHost) supportsModernSystemd(ctx context.Context, executable string) bool {
	output, err := host.executor.Run(ctx, executable, "--version")
	if err != nil {
		return false
	}
	match := systemdVersionPattern.FindSubmatch(output)
	if len(match) != 2 {
		return false
	}
	version, err := strconv.Atoi(string(match[1]))
	// ProtectProc=, the newest directive in the managed drop-in, requires 247.
	return err == nil && version >= 247
}

func managerName(kind serviceManagerKind) string {
	if kind == serviceManagerSystemd {
		return "systemd"
	}
	return "openrc"
}

var _ io.Writer = (*limitedWriter)(nil)
