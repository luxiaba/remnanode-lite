package xray

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/system"
)

func TestHealthDoesNotRecoverUnknownVersionWhileStarting(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	var calls atomic.Int32
	startProbeEntered := make(chan struct{})
	releaseStartProbe := make(chan struct{})
	var releaseStartOnce sync.Once
	releaseStart := func() { releaseStartOnce.Do(func() { close(releaseStartProbe) }) }
	defer releaseStart()
	manager.readinessProbe = func(context.Context) bool { return true }

	manager.version.probe = func(context.Context) (string, error) {
		if calls.Add(1) == 1 {
			close(startProbeEntered)
			<-releaseStartProbe
		}
		return "26.6.27", nil
	}

	response := make(chan StartResponse, 1)
	go func() { response <- manager.Start(context.Background(), lifecycleStartRequest("client-a")) }()
	awaitSignal(t, startProbeEntered, "start version probe")

	health := manager.Health()
	snapshot := snapshotVersionForTest(manager)
	if health.XrayInternalStatusCached || health.XrayVersion != nil {
		t.Fatalf("health while starting = %+v", health)
	}
	if snapshot.busy || calls.Load() != 1 {
		t.Fatalf("Health started a second version probe while Start was pending: %+v calls=%d", snapshot, calls.Load())
	}

	releaseStart()
	if response := awaitStartResponse(t, response); !response.IsStarted || response.Version == nil || *response.Version != "26.6.27" {
		t.Fatalf("start response = %#v", response)
	}
}

func TestHealthVersionRecoveryFailureClearsBusyAndCanRetry(t *testing.T) {
	var calls atomic.Int32
	attempted := make(chan struct{}, 2)
	manager, err := newManager(Options{
		XrayBin:            "unused-rw-core",
		GeoDir:             t.TempDir(),
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnanode-lite-test.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	}, func(context.Context) (string, error) {
		if calls.Add(1) > 1 {
			attempted <- struct{}{}
		}
		return "", errors.New("version probe failed")
	})
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	setVersionRetryNowForTest(manager)
	_ = manager.Health()
	awaitSignal(t, attempted, "failed background version probe")
	snapshot := waitForVersionProbeIdle(t, manager)
	if snapshot.cached != nil || !snapshot.nextProbe.After(time.Now()) {
		t.Fatalf("version state after failed recovery = %+v", snapshot)
	}

	for range 8 {
		_ = manager.Health()
	}
	snapshot = snapshotVersionForTest(manager)
	if snapshot.busy || calls.Load() != 2 {
		t.Fatalf("retry backoff did not hold after a failed recovery: %+v calls=%d", snapshot, calls.Load())
	}

	setVersionRetryNowForTest(manager)
	_ = manager.Health()
	awaitSignal(t, attempted, "second failed background version probe")
	snapshot = waitForVersionProbeIdle(t, manager)
	if snapshot.cached != nil || !snapshot.nextProbe.After(time.Now()) {
		t.Fatalf("version state after second failed recovery = %+v", snapshot)
	}
	if got := calls.Load(); got != 3 { // one synchronous constructor probe, two Health retries
		t.Fatalf("version probe calls = %d, want 3", got)
	}
}

func TestShutdownCanFinishAfterAnEarlierTimeout(t *testing.T) {
	var calls atomic.Int32
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseProbe) }) }

	manager, err := newManager(Options{
		XrayBin:            "unused-rw-core",
		GeoDir:             t.TempDir(),
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnanode-lite-test.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	}, func(context.Context) (string, error) {
		if calls.Add(1) == 1 {
			return "", errors.New("initial version probe failed")
		}
		close(probeStarted)
		<-releaseProbe // Deliberately ignore cancellation until the test releases it.
		return "", errors.New("background version probe failed")
	})
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	t.Cleanup(func() {
		release()
		_ = manager.Shutdown(context.Background())
	})

	setVersionRetryNowForTest(manager)
	_ = manager.Health()
	awaitSignal(t, probeStarted, "background version probe")

	shortContext, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = manager.Shutdown(shortContext)
	cancelShort()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Shutdown error = %v, want deadline exceeded", err)
	}

	release()
	longContext, cancelLong := context.WithTimeout(context.Background(), time.Second)
	defer cancelLong()
	if err := manager.Shutdown(longContext); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	snapshot := snapshotVersionForTest(manager)
	if !snapshot.shutdown || snapshot.busy || !errors.Is(snapshot.contextErr, context.Canceled) {
		t.Fatalf("version state after completed shutdown = %+v", snapshot)
	}
}

func TestCoreVersionOverrideDoesNotStartBackgroundRecovery(t *testing.T) {
	var calls atomic.Int32
	manager, err := newManager(Options{
		XrayBin:            "unused-rw-core",
		GeoDir:             t.TempDir(),
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnanode-lite-test.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		CoreVersion:        "v26.6.27",
		System:             system.NewCollector(nil),
	}, func(context.Context) (string, error) {
		calls.Add(1)
		return "unexpected", nil
	})
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	health := manager.Health()
	snapshot := snapshotVersionForTest(manager)
	if health.XrayVersion == nil || *health.XrayVersion != "26.6.27" {
		t.Fatalf("health version = %#v", health.XrayVersion)
	}
	if calls.Load() != 0 || snapshot.busy {
		t.Fatalf("override scheduled a version probe: %+v calls=%d", snapshot, calls.Load())
	}
}
