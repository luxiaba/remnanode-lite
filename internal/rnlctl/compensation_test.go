package rnlctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type cancellationAwareHost struct {
	*fakeHostController
}

func (host *cancellationAwareHost) Prepare(ctx context.Context, generationRoot string, paths Paths) (ManagedAccount, error) {
	host.calls = append(host.calls, "prepare:"+filepath.Base(generationRoot))
	host.paths = paths
	if err := ctx.Err(); err != nil {
		return host.account, err
	}
	return host.account, host.nextFailure("prepare")
}

func (host *cancellationAwareHost) RemoveService(ctx context.Context, _ Paths) error {
	host.calls = append(host.calls, "remove-service")
	if err := ctx.Err(); err != nil {
		return err
	}
	return host.nextFailure("remove-service")
}

func (host *cancellationAwareHost) RemoveAccount(ctx context.Context, account ManagedAccount) error {
	host.calls = append(host.calls, fmt.Sprintf("remove-account:user=%t:group=%t", account.UserCreated, account.GroupCreated))
	if err := ctx.Err(); err != nil {
		return err
	}
	return host.nextFailure("remove-account")
}

func (host *cancellationAwareHost) ApplyOwnership(ctx context.Context, _ Paths) error {
	host.calls = append(host.calls, "apply-ownership")
	if err := ctx.Err(); err != nil {
		return err
	}
	return host.nextFailure("apply-ownership")
}

func TestCompensationContextIgnoresCancellationAndRetainsValues(t *testing.T) {
	type contextKey struct{}
	parent := context.WithValue(context.Background(), contextKey{}, "request-value")
	parent, cancelParent := context.WithCancel(parent)
	cancelParent()

	started := time.Now()
	ctx, cancel := newCompensationContext(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("newCompensationContext() error = %v", err)
	}
	if got := ctx.Value(contextKey{}); got != "request-value" {
		t.Fatalf("newCompensationContext() value = %v", got)
	}
	deadline, ok := ctx.Deadline()
	if !ok || deadline.Before(started.Add(compensationTimeout-time.Second)) || deadline.After(started.Add(compensationTimeout+time.Second)) {
		t.Fatalf("newCompensationContext() deadline = %v, want approximately %s", deadline, compensationTimeout)
	}
}

func TestRunRecoveryPhaseSuppressesNestedProgressAndCompletesOnce(t *testing.T) {
	sink := &recordingProgressSink{}
	ctx := withProgressSink(context.Background(), "upgrade", sink)
	emitProgressPhase(ctx, phaseSwitchGeneration)

	err := runRecoveryPhase(ctx, func(recoveryCtx context.Context) error {
		emitProgressPhase(recoveryCtx, phaseStartService)
		completeProgressPhase(recoveryCtx, phaseStartService, true)
		emitProgressPhase(recoveryCtx, phaseWaitHealthy)
		completeProgressPhase(recoveryCtx, phaseWaitHealthy, true)
		return nil
	})
	if err != nil {
		t.Fatalf("runRecoveryPhase() error = %v", err)
	}

	if len(sink.events) != 4 {
		t.Fatalf("events = %#v, want failed active phase plus one recovery phase", sink.events)
	}
	wantKinds := []progressEventKind{
		progressPhaseStarted,
		progressActivePhaseCompleted,
		progressPhaseStarted,
		progressPhaseCompleted,
	}
	for index, want := range wantKinds {
		if sink.events[index].Kind != want {
			t.Fatalf("event %d = %#v, want kind %v", index, sink.events[index], want)
		}
	}
	if got := sink.events[2]; got.Phase != phaseRestorePrevious {
		t.Fatalf("recovery start = %#v", got)
	}
	if got := sink.events[3]; got.Phase != phaseRestorePrevious || !got.Success {
		t.Fatalf("recovery completion = %#v", got)
	}
}

func TestEngineInstallRollbackSurvivesCallerCancellation(t *testing.T) {
	harness := newLifecycleHarness(t, "2.8.0-rnl.1")
	harness.host.account.UserCreated = true
	harness.host.account.GroupCreated = true
	harness.engine.host = &cancellationAwareHost{fakeHostController: harness.host}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := harness.engine.Install(ctx, InstallRequest{
		Bundle: BundleInput{Root: harness.bundle}, SecretFile: harness.secret,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Install() error = %v, want context.Canceled", err)
	}
	if !containsCall(harness.host.calls, "remove-service") || !containsCall(harness.host.calls, "remove-account:user=true:group=true") {
		t.Fatalf("rollback host calls = %q", harness.host.calls)
	}
	if journal, loadErr := loadJournal(harness.paths); loadErr != nil || journal != nil {
		t.Fatalf("journal after canceled install = %#v, %v; want cleared", journal, loadErr)
	}
	if state, loadErr := loadState(harness.paths); loadErr != nil || state != nil {
		t.Fatalf("state after canceled install = %#v, %v; want absent", state, loadErr)
	}
}

func TestManagedConfigurationRestorationSurvivesCallerCancellation(t *testing.T) {
	tests := []struct {
		name string
		path func(Paths) string
		run  func(*testing.T, context.Context, lifecycleHarness) error
	}{
		{
			name: "node.env",
			path: func(paths Paths) string { return paths.EnvironmentFile },
			run: func(_ *testing.T, ctx context.Context, harness lifecycleHarness) error {
				_, err := harness.engine.UpdateConfiguration(ctx, ConfigurationUpdateRequest{
					Set: map[string]string{"NODE_PORT": "12345"},
				})
				return err
			},
		},
		{
			name: "Secret Key",
			path: func(paths Paths) string { return paths.SecretFile },
			run: func(t *testing.T, ctx context.Context, harness lifecycleHarness) error {
				secret := writeTestSecretValue(t, filepath.Join(t.TempDir(), "new-secret.key"), "replacement")
				_, err := harness.engine.SetSecret(ctx, SecretUpdateRequest{File: secret})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLifecycleHarness(t, "2.8.0-rnl.1")
			harness.install(t, false)
			managedPath := test.path(harness.paths)
			original, err := os.ReadFile(managedPath)
			if err != nil {
				t.Fatal(err)
			}
			harness.host.calls = nil
			harness.engine.host = &cancellationAwareHost{fakeHostController: harness.host}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err = test.run(t, ctx, harness)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("configuration mutation error = %v, want context.Canceled", err)
			}
			got, readErr := os.ReadFile(managedPath)
			if readErr != nil || !reflect.DeepEqual(got, original) {
				t.Fatalf("restored %s = %q, %v; want original %q", test.name, got, readErr, original)
			}
			if countCall(harness.host.calls, "apply-ownership") != 2 {
				t.Fatalf("host calls = %q, want failed write ownership and successful restoration ownership", harness.host.calls)
			}
		})
	}
}
