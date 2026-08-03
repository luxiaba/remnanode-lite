package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type delayedCancellationHost struct {
	*fakeHostController
	canceled       bool
	pending        *bool
	ignoreRecovery bool
}

func (host *delayedCancellationHost) SetActive(ctx context.Context, active bool) error {
	if !host.canceled {
		host.calls = append(host.calls, fmt.Sprintf("active:%t", active))
		host.canceled = true
		pending := active
		host.pending = &pending
		return context.Canceled
	}
	if host.pending != nil {
		host.status.Active = *host.pending
		host.pending = nil
	}
	if host.ignoreRecovery {
		host.calls = append(host.calls, fmt.Sprintf("active:%t", active))
		return nil
	}
	return host.fakeHostController.SetActive(ctx, active)
}

func (host *delayedCancellationHost) completePendingJob() {
	if host.pending == nil {
		return
	}
	host.status.Active = *host.pending
	host.pending = nil
}

func TestEngineInstallEmitsOrderedRealProgressPhases(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	sink := &recordingProgressSink{}
	ctx := withProgressSink(context.Background(), "install", sink)

	_, err := harness.engine.Install(ctx, InstallRequest{
		Bundle: BundleInput{Root: harness.bundle, ExpectedVersion: "2.8.0-rnl.1"},
		Port:   2222, SecretFile: harness.secret,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	want := []operationPhase{
		phaseVerifyBundle,
		phaseValidateHost,
		phasePrepareGeneration,
		phaseWriteConfiguration,
		phaseSwitchGeneration,
		phasePrepareService,
		phaseStartService,
		phaseWaitHealthy,
		phaseCommitState,
		phaseCleanUp,
	}
	assertOrderedProgressPhases(t, sink.events, want)
}

func TestEngineInstallReportsSuccessfulRecoverySeparatelyFromFailure(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.fail("wait-healthy", errors.New("health fixture failure"))
	sink := &recordingProgressSink{}
	ctx := withProgressSink(context.Background(), "install", sink)

	_, err := harness.engine.Install(ctx, InstallRequest{
		Bundle: BundleInput{Root: harness.bundle, ExpectedVersion: "2.8.0-rnl.1"},
		Port:   2222, SecretFile: harness.secret,
	})
	if err == nil {
		t.Fatal("Install() unexpectedly succeeded")
	}

	recoveryStarts := 0
	recoveryCompletions := 0
	for _, event := range sink.events {
		if event.Phase != phaseRestorePrevious {
			continue
		}
		if event.Kind == progressPhaseStarted {
			recoveryStarts++
		}
		if event.Kind == progressPhaseCompleted {
			recoveryCompletions++
			if !event.Success {
				t.Fatalf("recovery unexpectedly failed: %#v", sink.events)
			}
		}
	}
	if recoveryStarts != 1 || recoveryCompletions != 1 {
		t.Fatalf("recovery events = %#v, want exactly one start and one completion", sink.events)
	}
}

func TestEngineCanceledServiceControlRestoresPreviousState(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if _, err := harness.engine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink := &recordingProgressSink{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = withProgressSink(ctx, "start", sink)

	if _, err := harness.engine.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context cancellation", err)
	}
	if harness.host.status.Active {
		t.Fatalf("service remained active after canceled start: %#v", harness.host.status)
	}
	state, err := loadState(harness.paths)
	if err != nil || state == nil || state.Desired.Active {
		t.Fatalf("state after canceled start = %#v, %v", state, err)
	}
	if journal, err := loadJournal(harness.paths); err != nil || journal != nil {
		t.Fatalf("journal after canceled start recovery = %#v, %v", journal, err)
	}
	recoveryStarts := 0
	recoveryCompletions := 0
	for _, event := range sink.events {
		if event.Phase != phaseRestorePrevious {
			continue
		}
		if event.Kind == progressPhaseStarted {
			recoveryStarts++
		}
		if event.Kind == progressPhaseCompleted {
			recoveryCompletions++
			if !event.Success {
				t.Fatalf("recovery unexpectedly failed: %#v", sink.events)
			}
		}
	}
	if recoveryStarts != 1 || recoveryCompletions != 1 {
		t.Fatalf("progress events = %#v, want exactly one successful recovery phase", sink.events)
	}
}

func TestEngineCanceledServiceControlReassertsStateAfterDelayedManagerJob(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation string
		active    bool
	}{
		{name: "start", operation: "start", active: false},
		{name: "stop", operation: "stop", active: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newLifecycleHarness(t, "2.8.0-rnl.1")
			harness.install(t, false)
			if !test.active {
				if _, err := harness.engine.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			harness.host.calls = nil
			host := &delayedCancellationHost{fakeHostController: harness.host}
			harness.engine.host = host
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			var err error
			if test.operation == "start" {
				_, err = harness.engine.Start(ctx)
			} else {
				_, err = harness.engine.Stop(ctx)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s error = %v, want context cancellation", test.operation, err)
			}
			host.completePendingJob()

			if harness.host.status.Active != test.active || !harness.host.status.Enabled {
				t.Fatalf("service after delayed job = %#v, want enabled=%t active=%t; calls = %q", harness.host.status, true, test.active, harness.host.calls)
			}
			if countCall(harness.host.calls, fmt.Sprintf("active:%t", test.active)) != 1 {
				t.Fatalf("recovery calls = %q, want forced active:%t", harness.host.calls, test.active)
			}
			state, loadErr := loadState(harness.paths)
			if loadErr != nil || state == nil || state.Desired.Active != test.active {
				t.Fatalf("state after recovery = %#v, %v", state, loadErr)
			}
			if journal, loadErr := loadJournal(harness.paths); loadErr != nil || journal != nil {
				t.Fatalf("journal after recovery = %#v, %v", journal, loadErr)
			}
		})
	}
}

func TestEngineCanceledActivateReassertsPreparedServiceState(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	harness.host.calls = nil
	host := &delayedCancellationHost{fakeHostController: harness.host}
	harness.engine.host = host
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := harness.engine.Activate(ctx, ActivateRequest{SecretFile: harness.secret})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Activate() error = %v, want context cancellation", err)
	}
	host.completePendingJob()

	if harness.host.status.Active || harness.host.status.Enabled {
		t.Fatalf("service after delayed activation = %#v; calls = %q", harness.host.status, harness.host.calls)
	}
	if countCall(harness.host.calls, "active:false") != 1 || countCall(harness.host.calls, "enabled:false") != 1 {
		t.Fatalf("recovery calls = %q, want forced inactive and disabled state", harness.host.calls)
	}
	state, loadErr := loadState(harness.paths)
	if loadErr != nil || state == nil || !state.Prepared || state.Desired.Active || state.Desired.Enabled {
		t.Fatalf("state after canceled activation = %#v, %v", state, loadErr)
	}
	if journal, loadErr := loadJournal(harness.paths); loadErr != nil || journal != nil {
		t.Fatalf("journal after canceled activation = %#v, %v", journal, loadErr)
	}
}

func TestEngineCanceledServiceControlKeepsJournalUntilStateConverges(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, false)
	if _, err := harness.engine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	harness.host.calls = nil
	host := &delayedCancellationHost{
		fakeHostController: harness.host,
		ignoreRecovery:     true,
	}
	harness.engine.host = host
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := harness.engine.Start(ctx)
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "service recovery state") {
		t.Fatalf("Start() error = %v, want cancellation plus recovery mismatch", err)
	}
	if !harness.host.status.Active {
		t.Fatalf("fixture did not apply the delayed start job: %#v", harness.host.status)
	}
	journal, loadErr := loadJournal(harness.paths)
	if loadErr != nil || journal == nil || journal.Operation != "start" {
		t.Fatalf("journal after incomplete recovery = %#v, %v", journal, loadErr)
	}
}

func TestEngineRepairReportsInterruptedInstallCleanup(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.install(t, true)
	state, err := loadState(harness.paths)
	if err != nil || state == nil {
		t.Fatalf("loadState() = %#v, %v", state, err)
	}
	current := state.Generations[state.Current]
	if err := removeAndSync(harness.paths.StateFile); err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		SchemaVersion: journalSchemaVersion,
		Operation:     "install",
		Phase:         "service-prepared",
		Target:        current,
		Desired:       state.Desired,
		Prepared:      state.Prepared,
		Account:       state.Account,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveJournal(harness.paths, journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(harness.paths.EnvironmentFile); err != nil {
		t.Fatal(err)
	}

	sink := &recordingProgressSink{}
	ctx := withProgressSink(context.Background(), "repair", sink)
	result, err := harness.engine.Repair(ctx, RepairRequest{})
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !result.Changed || result.Generation != "" {
		t.Fatalf("Repair() = %#v, want cleanup to absent", result)
	}

	cleanupStarts := 0
	cleanupCompletions := 0
	for _, event := range sink.events {
		if event.Phase != phaseCleanUp {
			continue
		}
		if event.Kind == progressPhaseStarted {
			cleanupStarts++
		}
		if event.Kind == progressPhaseCompleted && event.Success {
			cleanupCompletions++
		}
	}
	if cleanupStarts != 1 || cleanupCompletions != 1 {
		t.Fatalf("cleanup events = %#v, want exactly one successful cleanup phase", sink.events)
	}
}

func assertOrderedProgressPhases(t *testing.T, events []progressEvent, want []operationPhase) {
	t.Helper()
	next := 0
	for _, event := range events {
		if event.Kind != progressPhaseStarted || next == len(want) {
			continue
		}
		if event.Phase == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("progress events = %#v, missing ordered phase %v at index %d", events, want[next], next)
	}
}
