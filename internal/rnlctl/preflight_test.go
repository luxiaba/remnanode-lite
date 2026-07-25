package rnlctl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnginePreflightUpgradePlansWithoutPersistentMutation(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	target := openTestBundle(t, targetRoot)
	before := captureUpgradeDurableState(t, harness.paths)
	harness.host.calls = nil

	plan, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{
		Bundle: BundleInput{Root: targetRoot, ExpectedVersion: "2.8.0-rnl.2"},
	})
	if err != nil {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	want := UpgradePlan{
		SchemaVersion:     upgradePlanSchemaVersion,
		ChangeRequired:    true,
		CurrentVersion:    installed.Version,
		CurrentGeneration: installed.Generation,
		TargetVersion:     target.Manifest.Version,
		TargetGeneration:  target.GenerationID,
		Prepared:          false,
		Service:           ServiceStatus{Manager: "test", Enabled: true, Active: true},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("PreflightUpgrade() = %#v, want %#v", plan, want)
	}
	if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, before) {
		t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", before, after)
	}
	if !reflect.DeepEqual(harness.host.calls, []string{"service-status", "preflight:true"}) {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEnginePreflightUpgradeSameIdentityNeedsNoHostMutationPreflight(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, false)
	harness.host.calls = nil

	plan, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{
		Bundle: BundleInput{Root: harness.bundle, ExpectedVersion: installed.Version},
	})
	if err != nil {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	if plan.ChangeRequired || plan.TargetGeneration != installed.Generation {
		t.Fatalf("PreflightUpgrade() = %#v", plan)
	}
	if !reflect.DeepEqual(harness.host.calls, []string{"service-status"}) {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEnginePreflightUpgradePreparedState(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	harness.host.calls = nil

	plan, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}})
	if err != nil {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	if !plan.Prepared || plan.Service.Enabled || plan.Service.Active {
		t.Fatalf("PreflightUpgrade() = %#v", plan)
	}
	if !reflect.DeepEqual(harness.host.calls, []string{"service-status", "preflight:false"}) {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEnginePreflightUpgradeRejectsPreparedServiceDrift(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	harness.host.status.Active = true
	request := UpgradeRequest{Bundle: BundleInput{Root: targetRoot}}
	before := captureUpgradeDurableState(t, harness.paths)

	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{name: "preflight", run: func() error {
			_, err := harness.engine.PreflightUpgrade(context.Background(), request)
			return err
		}},
		{name: "upgrade", run: func() error {
			_, err := harness.engine.Upgrade(context.Background(), request)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			harness.host.calls = nil
			err := operation.run()
			if err == nil || !strings.Contains(err.Error(), "unexpectedly enabled or active") {
				t.Fatalf("operation error = %v", err)
			}
			if !reflect.DeepEqual(harness.host.calls, []string{"service-status"}) {
				t.Fatalf("host calls = %q", harness.host.calls)
			}
			if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, before) {
				t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestEnginePreflightUpgradeRespectsLifecycleLock(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	lock, err := acquireOperationLock(harness.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	harness.host.calls = nil

	_, err = harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}})
	if !errors.Is(err, ErrConcurrentOperation) {
		t.Fatalf("PreflightUpgrade() error = %v, want ErrConcurrentOperation", err)
	}
	if len(harness.host.calls) != 0 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEnginePreflightUpgradeRejectsDamagedCurrentGeneration(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	installed := harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	currentBinary := filepath.Join(harness.paths.Generations, installed.Generation, "bin", "remnanode-lite")
	if err := os.WriteFile(currentBinary, []byte("tampered\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	harness.host.calls = nil

	_, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}})
	if err == nil || !strings.Contains(err.Error(), "verify current generation") || !strings.Contains(err.Error(), "run rnlctl repair") {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	if len(harness.host.calls) != 0 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
}

func TestEnginePreflightUpgradeHostPreflightFailureDoesNotMutate(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	before := captureUpgradeDurableState(t, harness.paths)
	serviceBefore := harness.host.status
	harness.host.calls = nil
	harness.host.fail("preflight", errors.New("host capacity rejected"))

	_, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}})
	if err == nil || !strings.Contains(err.Error(), "host capacity rejected") {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	if !reflect.DeepEqual(harness.host.calls, []string{"service-status", "preflight:true"}) {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	if harness.host.status != serviceBefore {
		t.Fatalf("service state changed: before=%#v after=%#v", serviceBefore, harness.host.status)
	}
	if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, before) {
		t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestUpgradeEntryPointsFailBeforeOnlineResolution(t *testing.T) {
	t.Run("non-root", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		harness.install(t, false)
		harness.engine.requireRoot = func() bool { return false }
		assertUpgradeEntryPointsDoNotResolve(t, harness.engine, "must run as root")
	})

	t.Run("not installed", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		assertUpgradeEntryPointsDoNotResolve(t, harness.engine, ErrNotInstalled.Error())
	})

	t.Run("pending journal", func(t *testing.T) {
		harness := newLifecycleHarness(t, "2.8.0-rnl.1")
		harness.install(t, false)
		state, err := loadState(harness.paths)
		if err != nil || state == nil {
			t.Fatalf("loadState() = %#v, %v", state, err)
		}
		current := state.Generations[state.Current]
		if err := saveJournal(harness.paths, transactionJournal{
			SchemaVersion: journalSchemaVersion,
			Operation:     "upgrade",
			Phase:         "planned",
			From:          state.Current,
			Target:        current,
			Desired:       state.Desired,
			Account:       state.Account,
			StartedAt:     time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
		assertUpgradeEntryPointsDoNotResolve(t, harness.engine, "requires rnlctl repair")
	})
}

func TestUpgradeToRejectsEveryLocalBundleOption(t *testing.T) {
	tests := []struct {
		name   string
		bundle BundleInput
	}{
		{name: "root", bundle: BundleInput{Root: "/bundle"}},
		{name: "archive", bundle: BundleInput{Archive: "/bundle.tar.gz"}},
		{name: "sha256", bundle: BundleInput{SHA256: strings.Repeat("a", 64)}},
		{name: "expected version", bundle: BundleInput{ExpectedVersion: "2.8.0-rnl.2"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeBundleResolver{archive: "/must-not-be-read"}
			engine := NewEngine(EngineOptions{
				Paths: PathsAt(t.TempDir()), Host: newFakeHostController(), Resolver: resolver,
				Architecture: "amd64", RequireRoot: func() bool { return true },
			})
			_, err := engine.PreflightUpgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2", Bundle: test.bundle})
			if err == nil || !strings.Contains(err.Error(), "cannot be combined with local bundle options") {
				t.Fatalf("PreflightUpgrade() error = %v", err)
			}
			if len(resolver.calls) != 0 {
				t.Fatalf("resolver calls = %q", resolver.calls)
			}
		})
	}
}

func TestEnginePreflightUpgradeToCleansTemporaryWorkspaces(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	resolver := &fakeBundleResolver{archive: writeTestBundleArchive(t, targetRoot)}
	harness.engine.resolver = resolver
	temporaryRoot := os.TempDir()
	before := readDirectoryNames(t, temporaryRoot)
	durableBefore := captureUpgradeDurableState(t, harness.paths)

	plan, err := harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"})
	if err != nil {
		t.Fatalf("PreflightUpgrade() error = %v", err)
	}
	if !plan.ChangeRequired || plan.TargetVersion != "2.8.0-rnl.2" {
		t.Fatalf("PreflightUpgrade() = %#v", plan)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("resolver calls = %q", resolver.calls)
	}
	if after := readDirectoryNames(t, temporaryRoot); !reflect.DeepEqual(after, before) {
		t.Fatalf("temporary entries changed: before=%q after=%q", before, after)
	}
	if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, durableBefore) {
		t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", durableBefore, after)
	}
}

func TestEnginePreflightUpgradeToCleansTemporaryWorkspacesOnLockConflict(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	resolver := &fakeBundleResolver{archive: writeTestBundleArchive(t, targetRoot)}
	harness.engine.resolver = resolver
	lock, err := acquireOperationLock(harness.paths)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	temporaryRoot := os.TempDir()
	before := readDirectoryNames(t, temporaryRoot)
	durableBefore := captureUpgradeDurableState(t, harness.paths)
	harness.host.calls = nil

	_, err = harness.engine.PreflightUpgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"})
	if !errors.Is(err, ErrConcurrentOperation) {
		t.Fatalf("PreflightUpgrade() error = %v, want ErrConcurrentOperation", err)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("resolver calls = %q", resolver.calls)
	}
	if len(harness.host.calls) != 0 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	if after := readDirectoryNames(t, temporaryRoot); !reflect.DeepEqual(after, before) {
		t.Fatalf("temporary entries changed: before=%q after=%q", before, after)
	}
	if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, durableBefore) {
		t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", durableBefore, after)
	}
}

func TestEngineUpgradeValidatesStagedBinaryBeforeStoppingService(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	target := openTestBundle(t, targetRoot)
	before := captureUpgradeDurableState(t, harness.paths)
	harness.host.calls = nil
	harness.host.fail("validate-binary", errors.New("candidate version rejected"))

	_, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}})
	if err == nil || !strings.Contains(err.Error(), "candidate version rejected") {
		t.Fatalf("Upgrade() error = %v", err)
	}
	for _, forbidden := range []string{"active:false", "active:true", "enabled:false", "enabled:true", "restart", "wait-healthy:remnanode-lite"} {
		if containsCall(harness.host.calls, forbidden) {
			t.Fatalf("host calls = %q; pre-stop validation invoked %q", harness.host.calls, forbidden)
		}
	}
	if containsSubstring(harness.host.calls, "prepare:") {
		t.Fatalf("host calls = %q; service was prepared", harness.host.calls)
	}
	if countCall(harness.host.calls, "validate-binary:remnanode-lite:2.8.0-rnl.2:2.8.0") != 1 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	if after := captureUpgradeDurableState(t, harness.paths); !reflect.DeepEqual(after, before) {
		t.Fatalf("durable state changed:\nbefore=%#v\nafter=%#v", before, after)
	}
	if _, err := os.Lstat(filepath.Join(harness.paths.Generations, target.GenerationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged generation remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(harness.paths.BundleCache, target.Identity+".tar.gz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged cache remains: %v", err)
	}
}

func TestEngineUpgradeValidatesCandidateBeforeAndAfterTransition(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	targetRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	target := openTestBundle(t, targetRoot)
	harness.host.calls = nil

	if _, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: targetRoot}}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	validation := "validate-binary:remnanode-lite:2.8.0-rnl.2:2.8.0"
	if countCall(harness.host.calls, validation) != 2 {
		t.Fatalf("host calls = %q", harness.host.calls)
	}
	assertCallOrder(t, harness.host.calls, validation, "active:false")
	assertCallOrder(t, harness.host.calls, "prepare:"+target.GenerationID, "wait-healthy:remnanode-lite")
}

func TestEngineUpgradePreservesRetainedCacheWhenStagingFails(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*retainedCacheUpgradeFixture) func()
		wantError string
	}{
		{
			name: "initial journal save",
			configure: func(fixture *retainedCacheUpgradeFixture) func() {
				original := fixture.harness.engine.paths.JournalFile
				fixture.harness.engine.paths.JournalFile = filepath.Join(fixture.stagedCache, "journal.json")
				return func() { fixture.harness.engine.paths.JournalFile = original }
			},
			wantError: "is not a real directory",
		},
		{
			name: "candidate binary validation",
			configure: func(fixture *retainedCacheUpgradeFixture) func() {
				fixture.harness.host.fail("validate-binary", errors.New("candidate version rejected"))
				return func() {}
			},
			wantError: "candidate version rejected",
		},
		{
			name: "service transition",
			configure: func(fixture *retainedCacheUpgradeFixture) func() {
				fixture.harness.host.fail("prepare", errors.New("target service rejected"), nil)
				return func() {}
			},
			wantError: "target service rejected",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetainedCacheUpgradeFixture(t)
			restore := test.configure(fixture)
			_, err := fixture.harness.engine.Upgrade(context.Background(), fixture.request())
			restore()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Upgrade() error = %v, want %q", err, test.wantError)
			}
			fixture.assertOriginalStateAndRepair(t)
		})
	}
}

func TestEngineUpgradeCommitsVerifiedCacheBeforeRemovingRetainedSnapshot(t *testing.T) {
	fixture := newRetainedCacheUpgradeFixture(t)
	result, err := fixture.harness.engine.Upgrade(context.Background(), fixture.request())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if result.Generation != fixture.previous.ID || result.Version != fixture.previous.Version {
		t.Fatalf("Upgrade() = %#v", result)
	}

	state, err := loadState(fixture.harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	current := state.Generations[state.Current]
	if current.ID != fixture.previous.ID || current.CacheFile != fixture.stagedCache ||
		current.ArchiveSHA256 != fixture.archiveSHA256 || current.CacheKind != "verified-archive" {
		t.Fatalf("committed generation = %#v", current)
	}
	if state.Previous != fixture.before.Current {
		t.Fatalf("previous generation = %q, want %q", state.Previous, fixture.before.Current)
	}
	if _, err := os.Lstat(fixture.previous.CacheFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded root snapshot remains after commit: %v", err)
	}
	gotArchive, err := os.ReadFile(fixture.stagedCache)
	if err != nil {
		t.Fatal(err)
	}
	wantArchive, err := os.ReadFile(fixture.archive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotArchive, wantArchive) {
		t.Fatal("committed verified cache differs from the supplied archive")
	}
	status, err := fixture.harness.engine.Status(context.Background())
	if err != nil || !status.Healthy || status.RepairCapability != "verified-archive" {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	fixture.assertGenerationCanBeRepaired(t, current)
}

type retainedCacheUpgradeFixture struct {
	harness       lifecycleHarness
	before        persistentState
	previous      generationRecord
	previousBytes []byte
	archive       string
	archiveSHA256 string
	stagedCache   string
}

func newRetainedCacheUpgradeFixture(t *testing.T) *retainedCacheUpgradeFixture {
	t.Helper()
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	first := harness.install(t, false)
	archive := writeTestBundleArchive(t, harness.bundle)
	rewriteTestGzipTimestamp(t, archive)
	archiveSHA256, _, err := digestFile(archive, maxBundleArchive)
	if err != nil {
		t.Fatal(err)
	}

	secondRoot := writeTestBundle(t, filepath.Join(t.TempDir(), "target"), "2.8.0-rnl.2")
	if _, err := harness.engine.Upgrade(context.Background(), UpgradeRequest{Bundle: BundleInput{Root: secondRoot}}); err != nil {
		t.Fatalf("Upgrade(second generation) error = %v", err)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	previous := state.Generations[state.Previous]
	if previous.ID != first.Generation || previous.CacheKind != "root-snapshot" {
		t.Fatalf("retained generation = %#v", previous)
	}
	previousBytes, err := os.ReadFile(previous.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if archiveSHA256 == previous.ArchiveSHA256 {
		t.Fatal("test archive digest unexpectedly matches the retained root snapshot")
	}
	stagedCache := filepath.Join(harness.paths.BundleCache, archiveSHA256+".tar.gz")
	if stagedCache == previous.CacheFile {
		t.Fatal("verified archive and retained root snapshot use the same cache path")
	}
	return &retainedCacheUpgradeFixture{
		harness: harness, before: *state, previous: previous, previousBytes: previousBytes,
		archive: archive, archiveSHA256: archiveSHA256, stagedCache: stagedCache,
	}
}

func (fixture *retainedCacheUpgradeFixture) request() UpgradeRequest {
	return UpgradeRequest{Bundle: BundleInput{
		Archive: fixture.archive, SHA256: fixture.archiveSHA256,
		ExpectedVersion: fixture.previous.Version,
	}}
}

func (fixture *retainedCacheUpgradeFixture) assertOriginalStateAndRepair(t *testing.T) {
	t.Helper()
	state, err := loadState(fixture.harness.paths)
	if err != nil || state == nil || !reflect.DeepEqual(*state, fixture.before) {
		t.Fatalf("state after failed upgrade = %#v, %v; want %#v", state, err, fixture.before)
	}
	if journal, err := loadJournal(fixture.harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after failed upgrade = %#v, %v", journal, err)
	}
	got, err := os.ReadFile(fixture.previous.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, fixture.previousBytes) {
		t.Fatal("failed upgrade changed the retained cache bytes")
	}
	digest, _, err := digestFile(fixture.previous.CacheFile, maxBundleArchive)
	if err != nil || digest != fixture.previous.ArchiveSHA256 {
		t.Fatalf("retained cache digest = %q, %v; want %q", digest, err, fixture.previous.ArchiveSHA256)
	}
	if _, err := os.Lstat(fixture.stagedCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged verified cache remains after failed upgrade: %v", err)
	}
	fixture.assertGenerationCanBeRepaired(t, fixture.previous)
}

func (fixture *retainedCacheUpgradeFixture) assertGenerationCanBeRepaired(t *testing.T, record generationRecord) {
	t.Helper()
	target := filepath.Join(fixture.harness.paths.Generations, record.ID, "bin", "remnanode-lite")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.harness.engine.Repair(context.Background(), RepairRequest{}); err != nil {
		t.Fatalf("Repair() with retained cache error = %v", err)
	}
	state, err := loadState(fixture.harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() after repair = %#v, %v", state, err)
	}
	if err := fixture.harness.engine.verifyGeneration(state.Generations[record.ID]); err != nil {
		t.Fatalf("verify repaired generation: %v", err)
	}
}

func rewriteTestGzipTimestamp(t *testing.T, archive string) {
	t.Helper()
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 10 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatal("test bundle is not a gzip archive")
	}
	// Gzip MTIME is outside the tar payload, so this preserves the manifest
	// identity while giving the verified outer archive a distinct digest.
	data[4] ^= 0x01
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertUpgradeEntryPointsDoNotResolve(t *testing.T, engine *Engine, wantError string) {
	t.Helper()
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{name: "preflight", run: func() error {
			_, err := engine.PreflightUpgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"})
			return err
		}},
		{name: "upgrade", run: func() error {
			_, err := engine.Upgrade(context.Background(), UpgradeRequest{To: "2.8.0-rnl.2"})
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			resolver := &fakeBundleResolver{archive: "/must-not-be-read"}
			engine.resolver = resolver
			err := operation.run()
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("operation error = %v, want %q", err, wantError)
			}
			if len(resolver.calls) != 0 {
				t.Fatalf("resolver calls = %q", resolver.calls)
			}
		})
	}
}

type upgradeDurableState struct {
	state          string
	journal        string
	currentLink    string
	previousLink   string
	nodeBinaryLink string
	controlBinary  upgradeDurableFile
	environment    upgradeDurableFile
	secret         upgradeDurableFile
	systemdUnit    upgradeDurableFile
	systemdDropIn  upgradeDurableFile
	openRCUnit     upgradeDurableFile
	generations    []string
	bundleCache    []string
}

type upgradeDurableFile struct {
	mode    os.FileMode
	content string
}

func captureUpgradeDurableState(t *testing.T, paths Paths) upgradeDurableState {
	t.Helper()
	return upgradeDurableState{
		state:          readOptionalTestFile(t, paths.StateFile),
		journal:        readOptionalTestFile(t, paths.JournalFile),
		currentLink:    readOptionalTestLink(t, paths.CurrentLink),
		previousLink:   readOptionalTestLink(t, paths.PreviousLink),
		nodeBinaryLink: readOptionalTestLink(t, paths.NodeBinaryLink),
		controlBinary:  captureUpgradeDurableFile(t, paths.ControlBinary),
		environment:    captureUpgradeDurableFile(t, paths.EnvironmentFile),
		secret:         captureUpgradeDurableFile(t, paths.SecretFile),
		systemdUnit:    captureUpgradeDurableFile(t, paths.SystemdUnit),
		systemdDropIn:  captureUpgradeDurableFile(t, paths.SystemdDropIn),
		openRCUnit:     captureUpgradeDurableFile(t, paths.OpenRCUnit),
		generations:    readDirectoryNames(t, paths.Generations),
		bundleCache:    readDirectoryNames(t, paths.BundleCache),
	}
}

func captureUpgradeDurableFile(t *testing.T, path string) upgradeDurableFile {
	t.Helper()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return upgradeDurableFile{content: "<absent>"}
	}
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return upgradeDurableFile{mode: info.Mode(), content: string(data)}
}

func readOptionalTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "<absent>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readOptionalTestLink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if errors.Is(err, os.ErrNotExist) {
		return "<absent>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func readDirectoryNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func openTestBundle(t *testing.T, root string) *validatedBundle {
	t.Helper()
	bundle, err := openBundle(BundleInput{Root: root}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bundle.Close)
	return bundle
}
