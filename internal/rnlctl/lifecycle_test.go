package rnlctl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeHostController struct {
	status                  ServiceStatus
	account                 ManagedAccount
	calls                   []string
	failures                map[string][]error
	paths                   Paths
	removeRuntimeOnStop     bool
	requireRuntimeOnPrepare bool
	activePayload           string
	missingPayloadOnStop    string
}

type fakeBundleResolver struct {
	archive string
	err     error
	calls   []string
}

func (resolver *fakeBundleResolver) Resolve(_ context.Context, version, architecture, destination string) (string, error) {
	resolver.calls = append(resolver.calls, version+":"+architecture+":"+filepath.Base(destination))
	return resolver.archive, resolver.err
}

func newFakeHostController() *fakeHostController {
	return &fakeHostController{
		status:  ServiceStatus{Manager: "test"},
		account: ManagedAccount{UID: "1001", GID: "1001", Home: "/var/lib/remnanode-lite", Shell: "/usr/sbin/nologin"},
	}
}

func (host *fakeHostController) fail(method string, values ...error) {
	if host.failures == nil {
		host.failures = make(map[string][]error)
	}
	host.failures[method] = append(host.failures[method], values...)
}

func (host *fakeHostController) nextFailure(method string) error {
	values := host.failures[method]
	if len(values) == 0 {
		return nil
	}
	host.failures[method] = values[1:]
	return values[0]
}

func (host *fakeHostController) Preflight(_ context.Context, activating bool, paths Paths) error {
	host.calls = append(host.calls, fmt.Sprintf("preflight:%t", activating))
	host.paths = paths
	return host.nextFailure("preflight")
}

func (host *fakeHostController) Prepare(_ context.Context, generationRoot string, paths Paths) (ManagedAccount, error) {
	host.calls = append(host.calls, "prepare:"+filepath.Base(generationRoot))
	host.paths = paths
	if host.requireRuntimeOnPrepare {
		for _, directory := range []string{paths.ApplicationState, paths.LogDirectory, paths.RuntimeDirectory} {
			info, err := os.Lstat(directory)
			if err != nil || !info.IsDir() {
				return ManagedAccount{}, fmt.Errorf("runtime directory %s is unavailable before Prepare: %w", directory, err)
			}
		}
	}
	return host.account, host.nextFailure("prepare")
}

func (host *fakeHostController) RemoveService(context.Context, Paths) error {
	host.calls = append(host.calls, "remove-service")
	return host.nextFailure("remove-service")
}

func (host *fakeHostController) PreflightRemoveAccount(context.Context, ManagedAccount) error {
	host.calls = append(host.calls, "preflight-remove-account")
	return host.nextFailure("preflight-remove-account")
}

func (host *fakeHostController) RemoveAccount(_ context.Context, account ManagedAccount) error {
	host.calls = append(host.calls, fmt.Sprintf("remove-account:user=%t:group=%t", account.UserCreated, account.GroupCreated))
	return host.nextFailure("remove-account")
}

func (host *fakeHostController) ServiceStatus(context.Context) (ServiceStatus, error) {
	host.calls = append(host.calls, "service-status")
	return host.status, host.nextFailure("service-status")
}

func (host *fakeHostController) SetEnabled(_ context.Context, enabled bool) error {
	host.calls = append(host.calls, fmt.Sprintf("enabled:%t", enabled))
	if err := host.nextFailure("set-enabled"); err != nil {
		return err
	}
	host.status.Enabled = enabled
	return nil
}

func (host *fakeHostController) SetActive(_ context.Context, active bool) error {
	host.calls = append(host.calls, fmt.Sprintf("active:%t", active))
	if err := host.nextFailure("set-active"); err != nil {
		return err
	}
	if !active && host.activePayload != "" {
		if _, err := os.Lstat(host.activePayload); errors.Is(err, os.ErrNotExist) {
			host.missingPayloadOnStop = host.activePayload
		}
	}
	host.status.Active = active
	if !active && host.removeRuntimeOnStop && host.paths.RuntimeDirectory != "" {
		if err := os.RemoveAll(host.paths.RuntimeDirectory); err != nil {
			return err
		}
	}
	return nil
}

func (host *fakeHostController) Restart(context.Context) error {
	host.calls = append(host.calls, "restart")
	return host.nextFailure("restart")
}

func (host *fakeHostController) ApplyOwnership(context.Context, Paths) error {
	host.calls = append(host.calls, "apply-ownership")
	return host.nextFailure("apply-ownership")
}

func (host *fakeHostController) ValidateBinary(_ context.Context, binary, version, contractVersion string) error {
	host.calls = append(host.calls, "validate-binary:"+filepath.Base(binary)+":"+version+":"+contractVersion)
	return host.nextFailure("validate-binary")
}

func (host *fakeHostController) WaitHealthy(_ context.Context, binary, _ string, _ time.Duration) error {
	host.calls = append(host.calls, "wait-healthy:"+filepath.Base(binary))
	return host.nextFailure("wait-healthy")
}

type lifecycleHarness struct {
	engine *Engine
	host   *fakeHostController
	paths  Paths
	bundle string
	secret string
}

func newLifecycleHarness(t *testing.T, version string) lifecycleHarness {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	paths := PathsAt(filepath.Join(root, "host"))
	host := newFakeHostController()
	return lifecycleHarness{
		engine: NewEngine(EngineOptions{
			Paths: paths, Host: host, Architecture: "amd64",
			RequireRoot: func() bool { return true },
		}),
		host:   host,
		paths:  paths,
		bundle: writeTestBundle(t, filepath.Join(root, "bundle-"+version), version),
		secret: writeTestSecret(t, filepath.Join(root, "secret.key")),
	}
}

func (h lifecycleHarness) install(t *testing.T, prepareOnly bool) Result {
	t.Helper()
	request := InstallRequest{Bundle: BundleInput{Root: h.bundle, ExpectedVersion: testBundleVersion(h.bundle)}, PrepareOnly: prepareOnly}
	if !prepareOnly {
		request.SecretFile = h.secret
	}
	result, err := h.engine.Install(context.Background(), request)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	return result
}

func TestEngineInstallStatusAndDoctor(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	result := harness.install(t, false)
	if !result.Changed || result.Operation != "install" || result.Version != "2.8.0-rnl.1" || result.PreparedOnly {
		t.Fatalf("Install() = %#v", result)
	}
	if harness.host.status != (ServiceStatus{Manager: "test", Enabled: true, Active: true}) {
		t.Fatalf("service status = %#v", harness.host.status)
	}

	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	if state.Current != result.Generation || state.Previous != "" || state.Prepared || !state.Desired.Enabled || !state.Desired.Active {
		t.Fatalf("installed state = %#v", state)
	}
	assertSymlinkTarget(t, harness.paths.CurrentLink, filepath.Join(harness.paths.Generations, result.Generation))
	assertSymlinkTarget(t, harness.paths.NodeBinaryLink, filepath.Join(harness.paths.CurrentLink, "bin", "remnanode-lite"))
	assertMode(t, harness.paths.EnvironmentFile, 0o640)
	assertMode(t, harness.paths.SecretFile, 0o640)
	assertMode(t, harness.paths.ControlBinary, 0o755)

	status, err := harness.engine.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Healthy || status.Deployment != "installed" || status.Version != result.Version || status.RepairCapability != "root-snapshot-limited" {
		t.Fatalf("Status() = %#v", status)
	}
	report, err := harness.engine.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if !report.Healthy || !hasCheck(report.Checks, "runtime-health", "ok") || !hasCheck(report.Checks, "repair-cache:"+result.Generation, "warning") {
		t.Fatalf("Doctor() = %#v", report)
	}
}

func TestEngineNoOpOperationsRespectPendingJournalAndLifecycleLock(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	current := state.Generations[state.Current]
	for _, operation := range []string{"install", "upgrade"} {
		journal := transactionJournal{
			SchemaVersion: journalSchemaVersion, Operation: operation,
			Phase: "state-committed",
			From:  state.Current, Target: current, Desired: state.Desired,
			Account: state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := saveJournal(harness.paths, journal); err != nil {
			t.Fatal(err)
		}
		var operationErr error
		if operation == "install" {
			_, operationErr = harness.engine.Install(context.Background(), InstallRequest{
				Bundle:     BundleInput{Root: harness.bundle, ExpectedVersion: current.Version},
				SecretFile: harness.secret,
			})
		} else {
			_, operationErr = harness.engine.Upgrade(context.Background(), UpgradeRequest{
				Bundle: BundleInput{Root: harness.bundle, ExpectedVersion: current.Version},
			})
		}
		if operationErr == nil || !strings.Contains(operationErr.Error(), "requires rnlctl repair") {
			t.Fatalf("%s with pending journal error = %v, want repair requirement", operation, operationErr)
		}
		if err := clearJournal(harness.paths); err != nil {
			t.Fatal(err)
		}
	}
	rollbackJournal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "rollback", Phase: "planned",
		From: state.Current, Target: current, Desired: state.Desired,
		Account: state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(harness.paths, rollbackJournal); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.engine.Rollback(context.Background(), RollbackRequest{GenerationID: current.ID}); err == nil || !strings.Contains(err.Error(), "requires rnlctl repair") {
		t.Fatalf("rollback with pending journal error = %v, want repair requirement", err)
	}
	if err := clearJournal(harness.paths); err != nil {
		t.Fatal(err)
	}

	for _, operation := range []string{"install", "upgrade", "rollback"} {
		lock, err := acquireOperationLock(harness.paths)
		if err != nil {
			t.Fatal(err)
		}
		var operationErr error
		switch operation {
		case "install":
			_, operationErr = harness.engine.Install(context.Background(), InstallRequest{Bundle: BundleInput{Root: harness.bundle}})
		case "upgrade":
			_, operationErr = harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: harness.bundle}})
		case "rollback":
			_, operationErr = harness.engine.Rollback(context.Background(), RollbackRequest{GenerationID: current.ID})
		}
		if closeErr := lock.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if !errors.Is(operationErr, ErrConcurrentOperation) {
			t.Fatalf("concurrent identical %s error = %v, want ErrConcurrentOperation", operation, operationErr)
		}
	}
}

func TestEngineValidatesBinaryBeforeActiveHealthChecks(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	assertCallOrder(t, harness.host.calls,
		"validate-binary:remnanode-lite:2.8.0-rnl.1:2.8.0",
		"wait-healthy:remnanode-lite",
	)

	harness.host.calls = nil
	secondBundle := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-v2"), "2.8.0-rnl.2")
	if _, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: secondBundle}}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	assertCallOrder(t, harness.host.calls,
		"validate-binary:remnanode-lite:2.8.0-rnl.2:2.8.0",
		"wait-healthy:remnanode-lite",
	)
}

func TestEnginePreparedInstallRequiresActivation(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	result := harness.install(t, true)
	if !result.Changed || !result.PreparedOnly {
		t.Fatalf("Install(prepare-only) = %#v", result)
	}
	if harness.host.status.Enabled || harness.host.status.Active {
		t.Fatalf("prepared service status = %#v", harness.host.status)
	}
	if _, err := os.Lstat(harness.paths.SecretFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared secret file error = %v, want not exist", err)
	}
	if _, err := harness.engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "rnlctl activate") {
		t.Fatalf("Start() error = %v, want activation guidance", err)
	}
	if _, err := harness.engine.Restart(context.Background()); err == nil || !strings.Contains(err.Error(), "rnlctl activate") {
		t.Fatalf("Restart() error = %v, want activation guidance", err)
	}

	activated, err := harness.engine.Activate(context.Background(), ActivateRequest{SecretFile: harness.secret})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if !activated.Changed || harness.host.status != (ServiceStatus{Manager: "test", Enabled: true, Active: true}) {
		t.Fatalf("Activate() = %#v, service = %#v", activated, harness.host.status)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil || state.Prepared || !state.Desired.Active || !state.Desired.Enabled {
		t.Fatalf("activated state = %#v, %v", state, err)
	}
}

func TestEngineActivateRollsBackSecretAndServiceAfterStateCommitFailure(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	oldSecret, err := os.ReadFile(harness.paths.SecretFile)
	if err != nil {
		t.Fatal(err)
	}
	newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "changed")
	stateBefore, err := loadState(harness.paths)
	if err != nil || stateBefore == nil {
		t.Fatalf("loadState() = %#v, %v", stateBefore, err)
	}
	// A malformed account returned by Prepare makes the final saveState fail
	// after the new secret has been written and the service restarted.
	harness.host.account = ManagedAccount{}
	if _, err := harness.engine.Activate(context.Background(), ActivateRequest{SecretFile: newSecret}); err == nil || !strings.Contains(err.Error(), "managed account identity is incomplete") {
		t.Fatalf("Activate() error = %v", err)
	}
	gotSecret, err := os.ReadFile(harness.paths.SecretFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotSecret, oldSecret) {
		t.Fatalf("secret after failed activation = %q, want original %q", gotSecret, oldSecret)
	}
	stateAfter, err := loadState(harness.paths)
	if err != nil || stateAfter == nil || !reflect.DeepEqual(*stateAfter, *stateBefore) {
		t.Fatalf("state after failed activation = %#v, %v; want %#v", stateAfter, err, stateBefore)
	}
	if harness.host.status != (ServiceStatus{Manager: "test", Enabled: true, Active: true}) {
		t.Fatalf("service after failed activation = %#v", harness.host.status)
	}
	if countCall(harness.host.calls, "restart") != 2 {
		t.Fatalf("restart calls = %q, want activation and rollback restart", harness.host.calls)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after activation rollback = %#v, %v", journal, err)
	}
}

func TestEngineRepairRetriesInterruptedActivateRestartAndKeepsSecretIntent(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "repair")
	secretData, err := os.ReadFile(newSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(harness.paths.SecretFile, secretData, 0o640); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	current := state.Generations[state.Current]
	if err := saveJournal(harness.paths, transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "activate", Phase: "planned",
		From: state.Current, Target: current, Desired: desiredServiceState{Enabled: true, Active: true},
		RestartRequired: true, Account: state.Account, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	harness.host.fail("restart", errors.New("restart temporarily unavailable"), nil)

	if _, err := harness.engine.Repair(context.Background(), RepairRequest{}); err == nil || !strings.Contains(err.Error(), "restart temporarily unavailable") {
		t.Fatalf("first Repair() error = %v", err)
	}
	journal, err := loadJournal(harness.paths)
	if err != nil || journal == nil || journal.Operation != "activate" || !journal.RestartRequired {
		t.Fatalf("journal after failed activate repair = %#v, %v", journal, err)
	}

	if _, err := harness.engine.Repair(context.Background(), RepairRequest{}); err != nil {
		t.Fatalf("second Repair() error = %v", err)
	}
	if countCall(harness.host.calls, "restart") != 2 {
		t.Fatalf("restart calls = %q, want retry", harness.host.calls)
	}
	gotSecret, err := os.ReadFile(harness.paths.SecretFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotSecret, secretData) {
		t.Fatalf("secret after repaired activation = %q, want new secret %q", gotSecret, secretData)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after repaired activation = %#v, %v", journal, err)
	}
}

func TestEngineActivateEnablesAnActiveButDisabledService(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	harness.host.status.Enabled = false
	harness.host.calls = nil
	newSecret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "disabled")
	result, err := harness.engine.Activate(context.Background(), ActivateRequest{SecretFile: newSecret})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if !result.Changed || harness.host.status != (ServiceStatus{Manager: "test", Enabled: true, Active: true}) {
		t.Fatalf("Activate() = %#v, service = %#v", result, harness.host.status)
	}
	if !containsCall(harness.host.calls, "enabled:true") || !containsCall(harness.host.calls, "restart") {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineServiceControlPersistsDesiredState(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)

	stopped, err := harness.engine.Stop(context.Background())
	if err != nil || !stopped.Changed || harness.host.status.Active {
		t.Fatalf("Stop() = %#v, %v; service = %#v", stopped, err, harness.host.status)
	}
	state, _ := loadState(harness.paths)
	if state == nil || state.Desired.Active {
		t.Fatalf("state after stop = %#v", state)
	}
	unchanged, err := harness.engine.Stop(context.Background())
	if err != nil || unchanged.Changed {
		t.Fatalf("second Stop() = %#v, %v", unchanged, err)
	}

	started, err := harness.engine.Start(context.Background())
	if err != nil || !started.Changed || !harness.host.status.Active {
		t.Fatalf("Start() = %#v, %v; service = %#v", started, err, harness.host.status)
	}
	state, _ = loadState(harness.paths)
	if state == nil || !state.Desired.Active {
		t.Fatalf("state after start = %#v", state)
	}
	unchanged, err = harness.engine.Start(context.Background())
	if err != nil || unchanged.Changed {
		t.Fatalf("second Start() = %#v, %v", unchanged, err)
	}

	restarted, err := harness.engine.Restart(context.Background())
	if err != nil || !restarted.Changed {
		t.Fatalf("Restart() = %#v, %v", restarted, err)
	}
	if !containsCall(harness.host.calls, "restart") || !containsCall(harness.host.calls, "wait-healthy:remnanode-lite") {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after successful controls = %#v, %v", journal, err)
	}
}

func TestEngineRejectsConcurrentLifecycleMutation(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	lock, err := acquireOperationLock(harness.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	if _, err := harness.engine.Stop(context.Background()); !errors.Is(err, ErrConcurrentOperation) {
		t.Fatalf("Stop() error = %v, want ErrConcurrentOperation", err)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil || !state.Desired.Active {
		t.Fatalf("state after rejected concurrent stop = %#v, %v", state, err)
	}
}

func TestEngineInterruptedControlIsRepairable(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if _, err := harness.engine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	harness.host.fail("set-active", errors.New("service start failed"))
	if _, err := harness.engine.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "service start failed") {
		t.Fatalf("Start() error = %v", err)
	}
	status, err := harness.engine.Status(context.Background())
	if err != nil || status.Deployment != "recovery-required" || status.Pending == nil || status.Pending.Operation != "start" {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	repaired, err := harness.engine.Repair(context.Background(), RepairRequest{})
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !repaired.Changed || harness.host.status.Active {
		t.Fatalf("Repair() = %#v, service = %#v", repaired, harness.host.status)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after repair = %#v, %v", journal, err)
	}
}

func TestEngineInterruptedInstallJournalSurvivesFailedRepair(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, true)
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	current := state.Generations[state.Current]
	if err := removeAndSync(harness.paths.StateFile); err != nil {
		t.Fatal(err)
	}
	interrupted := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "install", Phase: "service-prepared",
		Target: current, Desired: state.Desired, Prepared: state.Prepared, Account: state.Account,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(harness.paths, interrupted); err != nil {
		t.Fatal(err)
	}
	harness.host.fail("service-status", errors.New("temporary service query failure"))

	if _, err := harness.engine.Repair(context.Background(), RepairRequest{}); err == nil || !strings.Contains(err.Error(), "temporary service query failure") {
		t.Fatalf("first Repair() error = %v", err)
	}
	journal, err := loadJournal(harness.paths)
	if err != nil || journal == nil || journal.Operation != "install" || journal.Target.ID != installed.Generation {
		t.Fatalf("journal after failed repair = %#v, %v", journal, err)
	}

	repaired, err := harness.engine.Repair(context.Background(), RepairRequest{})
	if err != nil {
		t.Fatalf("second Repair() error = %v", err)
	}
	if !repaired.Changed || repaired.Generation != installed.Generation {
		t.Fatalf("second Repair() = %#v", repaired)
	}
	state, err = loadState(harness.paths)
	if err != nil || state == nil || state.Current != installed.Generation {
		t.Fatalf("reconstructed state = %#v, %v", state, err)
	}
	if journal, err = loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after successful repair = %#v, %v", journal, err)
	}
}

func TestEngineUpgradeAndRollbackGenerations(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, false)
	harness.host.removeRuntimeOnStop = true
	harness.host.requireRuntimeOnPrepare = true
	secondBundle := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-v2"), "2.8.0-rnl.2")

	upgraded, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: secondBundle, ExpectedVersion: "2.8.0-rnl.2"}})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if !upgraded.Changed || upgraded.Version != "2.8.0-rnl.2" {
		t.Fatalf("Upgrade() = %#v", upgraded)
	}
	state, _ := loadState(harness.paths)
	if state == nil || state.Current != upgraded.Generation || state.Previous != installed.Generation || len(state.Generations) != 2 {
		t.Fatalf("state after upgrade = %#v", state)
	}
	assertSymlinkTarget(t, harness.paths.PreviousLink, filepath.Join(harness.paths.Generations, installed.Generation))

	rolledBack, err := harness.engine.Rollback(context.Background(), RollbackRequest{})
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if !rolledBack.Changed || rolledBack.Generation != installed.Generation {
		t.Fatalf("Rollback() = %#v", rolledBack)
	}
	state, _ = loadState(harness.paths)
	if state == nil || state.Current != installed.Generation || state.Previous != upgraded.Generation {
		t.Fatalf("state after rollback = %#v", state)
	}
	assertSymlinkTarget(t, harness.paths.PreviousLink, filepath.Join(harness.paths.Generations, upgraded.Generation))
}

func TestEngineUpgradeToBindsExactResolvedVersion(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	secondRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-v2"), "2.8.0-rnl.2")
	resolver := &fakeBundleResolver{archive: writeTestBundleArchive(t, secondRoot)}
	harness.engine.resolver = resolver

	result, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"})
	if err != nil {
		t.Fatalf("Upgrade(--to) error = %v", err)
	}
	if !result.Changed || result.Version != "2.8.0-rnl.2" {
		t.Fatalf("Upgrade(--to) = %#v", result)
	}
	if len(resolver.calls) != 1 || !strings.HasPrefix(resolver.calls[0], "2.8.0-rnl.2:amd64:") {
		t.Fatalf("resolver calls = %q", resolver.calls)
	}
}

func TestEngineUpgradeToRejectsResolvedVersionMismatch(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, false)
	wrongRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-wrong"), "2.8.0-rnl.3")
	harness.engine.resolver = &fakeBundleResolver{archive: writeTestBundleArchive(t, wrongRoot)}

	if _, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"}); err == nil || !strings.Contains(err.Error(), "does not match expected version") {
		t.Fatalf("Upgrade(mismatched --to) error = %v", err)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil || state.Current != installed.Generation || state.Previous != "" {
		t.Fatalf("state after rejected mismatch = %#v, %v", state, err)
	}
}

func TestEngineUpgradeToRejectsMovingChannels(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	resolver := &fakeBundleResolver{}
	harness.engine.resolver = resolver
	for _, version := range []string{"latest", "preview", "v2.8.0", "2.8"} {
		t.Run(version, func(t *testing.T) {
			if _, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{To: version}); err == nil || !strings.Contains(err.Error(), "exact version") {
				t.Fatalf("Upgrade(--to=%s) error = %v", version, err)
			}
		})
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("resolver called for moving channel: %q", resolver.calls)
	}
}

func TestEngineFailedRollbackRestoresCommittedGeneration(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	secondBundle := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-v2"), "2.8.0-rnl.2")
	upgraded, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: secondBundle}})
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, _ := loadState(harness.paths)
	harness.host.removeRuntimeOnStop = true
	harness.host.requireRuntimeOnPrepare = true
	harness.host.fail("prepare", errors.New("target service rejected"), nil)

	if _, err := harness.engine.Rollback(context.Background(), RollbackRequest{}); err == nil || !strings.Contains(err.Error(), "target service rejected") {
		t.Fatalf("Rollback() error = %v", err)
	}
	stateAfter, err := loadState(harness.paths)
	if err != nil || stateAfter == nil || !reflect.DeepEqual(*stateAfter, *stateBefore) {
		t.Fatalf("state after failed rollback = %#v, %v; want %#v", stateAfter, err, stateBefore)
	}
	assertSymlinkTarget(t, harness.paths.CurrentLink, filepath.Join(harness.paths.Generations, upgraded.Generation))
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after restored rollback = %#v, %v", journal, err)
	}
}

func TestEngineRepairRestoresDamagedGenerationFromCache(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, true)
	brokenBinary := filepath.Join(harness.paths.Generations, installed.Generation, "bin", "remnanode-lite")
	if err := os.WriteFile(brokenBinary, []byte("tampered\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := harness.engine.Doctor(context.Background())
	if err != nil || report.Healthy || !hasCheck(report.Checks, "generation:"+installed.Generation, "error") {
		t.Fatalf("Doctor() before repair = %#v, %v", report, err)
	}

	repaired, err := harness.engine.Repair(context.Background(), RepairRequest{})
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !repaired.Changed || repaired.Generation != installed.Generation {
		t.Fatalf("Repair() = %#v", repaired)
	}
	state, _ := loadState(harness.paths)
	if state == nil {
		t.Fatal("state missing after repair")
	}
	if err := harness.engine.verifyGeneration(state.Generations[state.Current]); err != nil {
		t.Fatalf("verify repaired generation: %v", err)
	}
	status, err := harness.engine.Status(context.Background())
	if err != nil || !status.Healthy || status.Deployment != "prepared" {
		t.Fatalf("Status() after repair = %#v, %v", status, err)
	}
}

func TestEngineRepairRecreatesRuntimeDirectoriesBeforePrepare(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	harness.host.requireRuntimeOnPrepare = true
	for _, directory := range []string{harness.paths.ApplicationState, harness.paths.LogDirectory, harness.paths.RuntimeDirectory} {
		if err := os.RemoveAll(directory); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := harness.engine.Repair(context.Background(), RepairRequest{}); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	for _, directory := range []string{harness.paths.ApplicationState, harness.paths.LogDirectory, harness.paths.RuntimeDirectory} {
		assertMode(t, directory, 0o750)
	}
}

func TestEngineRepairRemovesUncommittedUpgradeGeneration(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	secondRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "bundle-2"), "2.8.0-rnl.2")
	bundle, err := openBundle(BundleInput{Root: secondRoot}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	cache, _, err := cacheBundle(bundle, harness.paths.BundleCache)
	if err != nil {
		bundle.Close()
		t.Fatal(err)
	}
	record := generationFromBundle(bundle, cache)
	targetRoot, created, err := copyBundleToGeneration(bundle, harness.paths.Generations)
	bundle.Close()
	if err != nil || !created {
		t.Fatalf("copyBundleToGeneration() = %s, %t, %v", targetRoot, created, err)
	}
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion, Operation: "upgrade", Phase: "payload-ready",
		From: state.Current, Target: record, Desired: state.Desired, Account: state.Account,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(harness.paths, journal); err != nil {
		t.Fatal(err)
	}
	harness.host.status.Active = true
	harness.host.activePayload = targetRoot
	if _, err := harness.engine.Repair(context.Background(), RepairRequest{}); err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if harness.host.missingPayloadOnStop != "" {
		t.Fatalf("repair removed active generation before stopping service: %s", harness.host.missingPayloadOnStop)
	}
	if _, err := os.Lstat(targetRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted generation remains: %v", err)
	}
	if _, err := os.Lstat(cache.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted cache remains: %v", err)
	}
	repairedState, err := loadState(harness.paths)
	if err != nil || repairedState == nil || repairedState.Current != state.Current || len(repairedState.Generations) != 1 {
		t.Fatalf("state after cleanup = %#v, %v", repairedState, err)
	}
}

func TestEngineFailedInstallRollsBackOwnedAccountAndFiles(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.host.fail("wait-healthy", errors.New("healthcheck failed"))

	request := InstallRequest{Bundle: BundleInput{Root: harness.bundle}, SecretFile: harness.secret}
	if _, err := harness.engine.Install(context.Background(), request); err == nil || !strings.Contains(err.Error(), "healthcheck failed") {
		t.Fatalf("Install() error = %v", err)
	}
	for _, path := range []string{harness.paths.StateFile, harness.paths.JournalFile, harness.paths.CurrentLink, harness.paths.NodeBinaryLink, harness.paths.ControlBinary} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s remains after rollback: %v", path, err)
		}
	}
	if !containsCall(harness.host.calls, "remove-account:user=true:group=false") {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineRepairCompletesInterruptedPurgeUninstall(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.host.account.GroupCreated = true
	harness.install(t, true)
	harness.host.fail("remove-account", errors.New("temporary account database failure"))

	if _, err := harness.engine.Uninstall(context.Background(), UninstallRequest{Purge: true, Yes: true}); err == nil || !strings.Contains(err.Error(), "temporary account database failure") {
		t.Fatalf("Uninstall() error = %v", err)
	}
	journal, err := loadJournal(harness.paths)
	if err != nil || journal == nil || journal.Operation != "uninstall" || !journal.Purge {
		t.Fatalf("interrupted uninstall journal = %#v, %v", journal, err)
	}
	if _, err := os.Lstat(harness.paths.ConfigDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purged config directory error = %v, want not exist", err)
	}

	repaired, err := harness.engine.Repair(context.Background(), RepairRequest{})
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !repaired.Changed {
		t.Fatalf("Repair() = %#v", repaired)
	}
	if countCall(harness.host.calls, "remove-account:user=true:group=true") != 2 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	for _, path := range []string{harness.paths.StateFile, harness.paths.JournalFile, harness.paths.InstallerState, harness.paths.ConfigDirectory, harness.paths.LibraryRoot} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s remains after repaired purge: %v", path, err)
		}
	}
	status, err := harness.engine.Status(context.Background())
	if err != nil || status.Deployment != "absent" || !status.Healthy {
		t.Fatalf("Status() after repaired purge = %#v, %v", status, err)
	}
}

func TestEngineRetainsAccountOwnershipAcrossNonPurgeReinstall(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.host.account.GroupCreated = false
	harness.install(t, true)

	uninstalled, err := harness.engine.Uninstall(context.Background(), UninstallRequest{})
	if err != nil || !uninstalled.Changed {
		t.Fatalf("Uninstall() = %#v, %v", uninstalled, err)
	}
	retained, err := loadRetained(harness.paths)
	if err != nil || retained == nil || !retained.Account.UserCreated || retained.Account.GroupCreated {
		t.Fatalf("retained account = %#v, %v", retained, err)
	}

	harness.host.account.UserCreated = false
	harness.host.account.GroupCreated = false
	reinstalled := harness.install(t, true)
	state, err := loadState(harness.paths)
	if err != nil || state == nil || !state.Account.UserCreated || state.Account.GroupCreated {
		t.Fatalf("state after reinstall = %#v, %v", state, err)
	}
	if _, err := os.Lstat(harness.paths.RetainedFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retained metadata after reinstall = %v", err)
	}
	if reinstalled.Version != "2.8.0-rnl.1" {
		t.Fatalf("reinstall result = %#v", reinstalled)
	}

	if _, err := harness.engine.Uninstall(context.Background(), UninstallRequest{Purge: true, Yes: true}); err != nil {
		t.Fatalf("purge after reinstall: %v", err)
	}
	if !containsCall(harness.host.calls, "remove-account:user=true:group=false") {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEngineFailedReinstallDoesNotRemoveRetainedAccount(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.install(t, true)
	if _, err := harness.engine.Uninstall(context.Background(), UninstallRequest{}); err != nil {
		t.Fatalf("Uninstall() = %v", err)
	}
	harness.host.calls = nil
	harness.host.account.UserCreated = false
	harness.host.fail("wait-healthy", errors.New("healthcheck failed"))
	if _, err := harness.engine.Install(context.Background(), InstallRequest{
		Bundle: BundleInput{Root: harness.bundle}, SecretFile: harness.secret,
	}); err == nil || !strings.Contains(err.Error(), "healthcheck failed") {
		t.Fatalf("failed reinstall error = %v", err)
	}
	if containsCall(harness.host.calls, "remove-account:user=true:group=false") {
		t.Fatalf("failed reinstall attempted to remove retained account: %q", harness.host.calls)
	}
	if _, err := os.Lstat(harness.paths.RetainedFile); err != nil {
		t.Fatalf("retained account metadata was lost: %v", err)
	}
}

func TestEngineInstallRollbackRemovesAccountCreatedBeforePrepareFailure(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.host.account.GroupCreated = true
	harness.host.fail("prepare", errors.New("service preparation failed"))

	if _, err := harness.engine.Install(context.Background(), InstallRequest{
		Bundle: BundleInput{Root: harness.bundle}, SecretFile: harness.secret,
	}); err == nil || !strings.Contains(err.Error(), "service preparation failed") {
		t.Fatalf("Install() error = %v", err)
	}
	if !containsCall(harness.host.calls, "remove-account:user=true:group=true") {
		t.Fatalf("rollback did not remove account created by failed Prepare: %q", harness.host.calls)
	}
	if state, err := loadState(harness.paths); err != nil || state != nil {
		t.Fatalf("state after failed install = %#v, %v; want absent", state, err)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after failed install = %#v, %v; want absent", journal, err)
	}
}

func TestEngineRejectsNonRootBeforeCreatingManagedState(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.engine.requireRoot = func() bool { return false }
	_, err := harness.engine.Install(context.Background(), InstallRequest{Bundle: BundleInput{Root: harness.bundle}, SecretFile: harness.secret})
	if err == nil || !strings.Contains(err.Error(), "must run as root") {
		t.Fatalf("Install() error = %v", err)
	}
	for _, path := range []string{harness.paths.InstallerState, harness.paths.LibraryRoot, harness.paths.ConfigDirectory} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("managed path %s exists after privilege rejection: %v", path, statErr)
		}
	}
}

func TestEngineStatusDetectsUnsafeManagedPermissions(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if err := os.Chmod(harness.paths.SecretFile, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := harness.engine.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Healthy || status.Deployment != "degraded" || !containsSubstring(status.Problems, "must be a regular 0640 file") {
		t.Fatalf("Status() = %#v", status)
	}
}

func TestEngineDoctorReportsUnsafeManagedPermissions(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if err := os.Chmod(harness.paths.EnvironmentFile, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := harness.engine.Doctor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy || !hasCheckDetail(report.Checks, "managed-permissions", "error", "0640") {
		t.Fatalf("Doctor() = %#v", report)
	}
}

func TestMergeAccountOwnershipPreservesOnlyMatchingIdentity(t *testing.T) {
	old := ManagedAccount{UserCreated: true, GroupCreated: true, UID: "1001", GID: "1001", Home: "/state", Shell: "/sbin/nologin"}
	current := ManagedAccount{UID: "1001", GID: "1001", Home: "/state", Shell: "/sbin/nologin"}
	merged := mergeAccountOwnership(old, current)
	if !merged.UserCreated || !merged.GroupCreated {
		t.Fatalf("mergeAccountOwnership() = %#v", merged)
	}
	current.UID = "2001"
	if got := mergeAccountOwnership(old, current); got.UserCreated || got.GroupCreated {
		t.Fatalf("changed identity inherited ownership flags: %#v", got)
	}
}

func TestGenerationRecordRejectsNonCanonicalVersionID(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	record := state.Generations[state.Current]
	record.ID = "02.8.0-rnl.1-" + strings.Repeat("a", 16)
	if err := validateGenerationRecord(record, harness.paths); err == nil || !strings.Contains(err.Error(), "invalid identity metadata") {
		t.Fatalf("validateGenerationRecord() error = %v, want canonical ID rejection", err)
	}
}

func writeTestBundle(t *testing.T, root, version string) string {
	t.Helper()
	files := map[string][]byte{
		"LICENSE":                               []byte("test license\n"),
		"SOURCE-OFFER.md":                       []byte("source offer\n"),
		"THIRD_PARTY_NOTICES.md":                []byte("third party notices\n"),
		"SBOM.spdx.json":                        []byte("{}\n"),
		"install.sh":                            []byte("#!/bin/sh\nexit 0\n"),
		"bin/remnanode-lite":                    []byte("node " + version + "\n"),
		"bin/rnlctl":                            []byte("rnlctl " + version + "\n"),
		"lib/rw-core":                           []byte("core " + version + "\n"),
		"share/asn/asn-prefixes.bin":            []byte("asn\n"),
		"share/xray/geoip.dat":                  []byte("geoip\n"),
		"share/xray/geosite.dat":                []byte("geosite\n"),
		"support/deploy/remnanode-lite.service": []byte("[Service]\nExecStart=/usr/local/bin/remnanode-lite\n"),
		"support/deploy/remnanode-lite-hardening.conf": []byte("[Service]\nNoNewPrivileges=true\n"),
		"support/deploy/remnanode-lite.openrc":         []byte("#!/sbin/openrc-run\n"),
		"support/deploy/node.env.example":              []byte("LOW_MEMORY=1\n"),
		"runtime-assets.lock.json":                     []byte("{\"schemaVersion\":2}\n"),
		"licenses/MPL-2.0.txt":                         []byte("MPL\n"),
		"licenses/GPL-3.0-only.txt":                    []byte("GPL\n"),
		"licenses/CC-BY-SA-4.0.txt":                    []byte("CC-BY-SA\n"),
		"licenses/CC0-1.0.txt":                         []byte("CC0\n"),
	}
	executable := map[string]bool{
		"install.sh": true, "bin/remnanode-lite": true, "bin/rnlctl": true,
		"lib/rw-core": true, "support/deploy/remnanode-lite.openrc": true,
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	manifestFiles := make([]manifestFile, 0, len(paths))
	for _, relative := range paths {
		mode := os.FileMode(0o644)
		modeText := "0644"
		if executable[relative] {
			mode = 0o755
			modeText = "0755"
		}
		target := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, files[relative], mode); err != nil {
			t.Fatal(err)
		}
		manifestFiles = append(manifestFiles, manifestFile{
			Path: relative, Mode: modeText, Size: int64(len(files[relative])),
			SHA256: digestBytes(files[relative]), Role: "test", License: "MIT",
		})
	}
	digest := strings.Repeat("a", 64)
	revision := strings.Repeat("b", 40)
	artifact := manifestArtifact{URL: "https://example.invalid/artifact", SHA256: digest, Size: 1}
	payload := manifestRuntimePayload{SHA256: digest, Size: 1, License: "MIT"}
	manifest := releaseManifest{
		SchemaVersion: manifestSchema, Name: bundleTopDirectory, Version: version,
		ContractVersion: "2.8.0", OS: "linux", Architecture: "amd64",
		SourceRevision: revision, SourceDateEpoch: 1_700_000_000,
		RuntimeAssetLockSHA256: digestBytes(files["runtime-assets.lock.json"]),
		RuntimeAssets: manifestRuntimeAssets{
			Xray:    manifestXrayRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/xray", Archive: artifact, Core: payload},
			GeoIP:   manifestGeoRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/geoip", SourceArtifact: artifact, Artifact: artifact, License: "MIT"},
			GeoSite: manifestGeoRuntime{Version: "test", Commit: revision, SourceURL: "https://example.invalid/geosite", SourceArtifact: artifact, Artifact: artifact, License: "MIT"},
			ASN:     manifestASNRuntime{Commit: revision, Source: artifact, Output: payload},
		},
		Files: manifestFiles,
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(root, "release-manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeTestSecret(t *testing.T, path string) string {
	return writeTestSecretValue(t, path, "default")
}

func writeTestSecretValue(t *testing.T, path, marker string) string {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"caCertPem":"ca-%s","jwtPublicKey":"jwt-%s","nodeCertPem":"cert-%s","nodeKeyPem":"key-%s"}`, marker, marker, marker, marker))
	encoded := base64.StdEncoding.EncodeToString(raw) + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestBundleArchive(t *testing.T, root string) string {
	t.Helper()
	bundle, err := openBundle(BundleInput{Root: root}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	archive := filepath.Join(t.TempDir(), "remnanode-lite.tar.gz")
	if err := writeBundleCacheArchive(bundle, archive); err != nil {
		t.Fatal(err)
	}
	return archive
}

func testBundleVersion(root string) string {
	raw, _ := os.ReadFile(filepath.Join(root, "release-manifest.json"))
	var manifest releaseManifest
	_ = json.Unmarshal(raw, &manifest)
	return manifest.Version
}

func assertSymlinkTarget(t *testing.T, link, want string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink(%s): %v", link, err)
	}
	if got != want {
		t.Fatalf("Readlink(%s) = %q, want %q", link, got, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, got, want)
	}
}

func hasCheck(checks []Check, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func countCall(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
}

func assertCallOrder(t *testing.T, calls []string, before, after string) {
	t.Helper()
	beforeIndex, afterIndex := -1, -1
	for index, call := range calls {
		if call == before && beforeIndex < 0 {
			beforeIndex = index
		}
		if call == after && afterIndex < 0 {
			afterIndex = index
		}
	}
	if beforeIndex < 0 || afterIndex < 0 || beforeIndex >= afterIndex {
		t.Fatalf("call order = %q, want %q before %q", calls, before, after)
	}
}

func hasCheckDetail(checks []Check, name, status, detail string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status && strings.Contains(check.Detail, detail) {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
