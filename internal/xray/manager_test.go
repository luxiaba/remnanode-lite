package xray

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/system"
)

func TestBuildCommandArgs(t *testing.T) {
	args := BuildCommandArgs("/run/remnawave.sock")

	if len(args) != 4 {
		t.Fatalf("unexpected args: %#v", args)
	}
	if args[0] != "-config" || args[2] != "-format" || args[3] != "json" {
		t.Fatalf("unexpected args: %#v", args)
	}
	if got := args[1]; got != "http+unix:///run/remnawave.sock/internal/get-config" {
		t.Fatalf("unexpected config URL: %s", got)
	}
}

func TestGenerateAPIConfigInjectsRemnawaveAPI(t *testing.T) {
	config := generateAPIConfig(map[string]any{
		"inbounds": []any{map[string]any{"tag": "public"}},
		"routing": map[string]any{
			"rules": []any{map[string]any{"outboundTag": "direct"}},
		},
	}, "remnanode-xtls-test", TorrentBlockerOptions{})

	inbounds, ok := config["inbounds"].([]any)
	if !ok || len(inbounds) != 2 {
		t.Fatalf("expected API inbound plus original inbound, got %#v", config["inbounds"])
	}
	apiInbound, ok := inbounds[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected API inbound type: %#v", inbounds[0])
	}
	if apiInbound["tag"] != apiInboundTag || apiInbound["listen"] != "@remnanode-xtls-test" || apiInbound["protocol"] != "tunnel" {
		t.Fatalf("unexpected API inbound: %#v", apiInbound)
	}
	if _, ok := config["stats"].(map[string]any); !ok {
		t.Fatalf("expected stats object")
	}

	api, ok := config["api"].(map[string]any)
	if !ok || api["tag"] != apiTag {
		t.Fatalf("expected API model, got %#v", config["api"])
	}

	routing, ok := config["routing"].(map[string]any)
	if !ok {
		t.Fatalf("expected routing object")
	}
	rules, ok := routing["rules"].([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("expected injected routing rule plus original rules, got %#v", routing["rules"])
	}
}

func TestStartDoesNotCommitConfigWhenCommandFails(t *testing.T) {
	manager, err := NewManager(Options{
		XrayBin:            "definitely-missing-rw-core",
		GeoDir:             "/tmp",
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnawave.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	response := manager.Start(context.Background(), StartRequest{
		XrayConfig: map[string]any{
			"inbounds": []any{"one"},
		},
	})

	if response.IsStarted {
		t.Fatal("start with missing rw-core must not report Xray as started")
	}
	if response.Error == nil || !strings.Contains(*response.Error, "start rw-core") {
		t.Fatalf("expected start error, got %#v", response.Error)
	}

	if config := string(manager.CurrentConfigJSON()); config != "{}" {
		t.Fatalf("failed start retained config: %s", config)
	}
}

func TestStopClearsConfig(t *testing.T) {
	manager, err := NewManager(Options{
		XrayBin:            "definitely-missing-rw-core",
		GeoDir:             "/tmp",
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnawave.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	manager.Start(context.Background(), StartRequest{XrayConfig: map[string]any{"a": "b"}})
	manager.Stop()

	if string(manager.CurrentConfigJSON()) != "{}" {
		t.Fatalf("expected config to be cleared")
	}
}

func TestPluginXrayCleanupIsIdempotentWhileOffline(t *testing.T) {
	t.Parallel()

	manager := &Manager{state: lifecycleStopped}
	if err := manager.StopIfOnline(); err != nil {
		t.Fatalf("StopIfOnline: %v", err)
	}
	if err := manager.RemoveTorrentBlockerOutbound(); err != nil {
		t.Fatalf("RemoveTorrentBlockerOutbound: %v", err)
	}
}

func TestCurrentConfigJSONRemainsEmptyAfterFailedStart(t *testing.T) {
	manager, err := NewManager(Options{
		XrayBin:            "definitely-missing-rw-core",
		GeoDir:             "/tmp",
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnawave.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if got := string(manager.CurrentConfigJSON()); got != "{}" {
		t.Fatalf("expected empty object before start, got %s", got)
	}

	manager.Start(context.Background(), StartRequest{XrayConfig: map[string]any{
		"inbounds": []any{map[string]any{"tag": "public"}},
	}})

	if got := string(manager.CurrentConfigJSON()); got != "{}" {
		t.Fatalf("failed start committed cached JSON: %s", got)
	}

	manager.Stop()
	if got := string(manager.CurrentConfigJSON()); got != "{}" {
		t.Fatalf("expected cache cleared after stop, got %s", got)
	}
}

func TestParseVersionLine(t *testing.T) {
	raw := "Xray 26.3.27 (Xray, Penetrates Everything.) d2758a0 (go1.26.1 linux/amd64)\nA unified platform..."
	if got := parseVersionLine(raw); got != "26.3.27" {
		t.Fatalf("parseVersionLine() = %q, want 26.3.27", got)
	}

	if got := parseVersionLine("not a version"); got != "" {
		t.Fatalf("parseVersionLine() = %q, want empty", got)
	}
}

func TestManagerFreezesInjectedVersionsAndSystemSnapshotter(t *testing.T) {
	collector := system.NewCollector(nil)
	manager, err := NewManager(Options{
		XrayBin:            "definitely-missing-rw-core",
		GeoDir:             t.TempDir(),
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnawave.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.1",
		CoreVersion:        "v26.6.27",
		System:             collector,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	health := manager.Health()
	if health.NodeVersion != "2.8.1" || health.XrayVersion == nil || *health.XrayVersion != "26.6.27" {
		t.Fatalf("health versions = %+v", health)
	}
	t.Setenv("NODE_CONTRACT_VERSION", "9.9.9")
	t.Setenv("XRAY_CORE_VERSION", "v9.9.9")
	health = manager.Health()
	if health.NodeVersion != "2.8.1" || health.XrayVersion == nil || *health.XrayVersion != "26.6.27" {
		t.Fatalf("environment changed frozen versions: %+v", health)
	}
}

func TestManagerInitialVersionProbeUsesLifetime(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	geoDir := t.TempDir()
	logDir := t.TempDir()
	probeStarted := make(chan struct{})
	type constructorResult struct {
		manager *Manager
		err     error
	}
	result := make(chan constructorResult, 1)
	go func() {
		manager, err := newManager(Options{
			Lifetime:           lifetime,
			XrayBin:            "unused-rw-core",
			GeoDir:             geoDir,
			LogDir:             logDir,
			InternalSocketPath: "/run/remnawave.sock",
			InternalRESTToken:  "token",
			NodeVersion:        "2.8.0",
			System:             system.NewCollector(nil),
		}, func(ctx context.Context) (string, error) {
			close(probeStarted)
			<-ctx.Done()
			return "", ctx.Err()
		})
		result <- constructorResult{manager: manager, err: err}
	}()

	select {
	case <-probeStarted:
	case <-time.After(time.Second):
		t.Fatal("initial version probe did not start")
	}
	cancelLifetime()

	select {
	case outcome := <-result:
		if outcome.err != nil {
			t.Fatalf("newManager: %v", outcome.err)
		}
		if outcome.manager == nil {
			t.Fatal("newManager returned a nil manager")
		}
		if !errors.Is(outcome.manager.versionProbeContext.Err(), context.Canceled) {
			t.Fatalf("version probe context error = %v, want context canceled", outcome.manager.versionProbeContext.Err())
		}
		if err := outcome.manager.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("manager construction did not stop after lifetime cancellation")
	}
}

func TestManagerLifetimeCancelsBackgroundVersionProbeAndShutdownRemainsIdempotent(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	probeStarted := make(chan struct{})
	probeExited := make(chan struct{})
	var calls atomic.Int32
	manager, err := newManager(Options{
		Lifetime:           lifetime,
		XrayBin:            "unused-rw-core",
		GeoDir:             t.TempDir(),
		LogDir:             t.TempDir(),
		InternalSocketPath: "/run/remnawave.sock",
		InternalRESTToken:  "token",
		NodeVersion:        "2.8.0",
		System:             system.NewCollector(nil),
	}, func(ctx context.Context) (string, error) {
		if calls.Add(1) == 1 {
			return "", errors.New("initial probe failed")
		}
		close(probeStarted)
		<-ctx.Done()
		close(probeExited)
		return "", ctx.Err()
	})
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}

	manager.mu.Lock()
	manager.nextVersionProbe = time.Time{}
	manager.mu.Unlock()
	_ = manager.Health()
	select {
	case <-probeStarted:
	case <-time.After(time.Second):
		t.Fatal("background version probe did not start")
	}

	cancelLifetime()
	select {
	case <-probeExited:
	case <-time.After(time.Second):
		t.Fatal("background version probe did not stop after lifetime cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.RLock()
		busy := manager.versionProbeBusy
		manager.mu.RUnlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background version probe did not publish completion")
		}
		time.Sleep(time.Millisecond)
	}
	manager.mu.Lock()
	manager.nextVersionProbe = time.Time{}
	manager.mu.Unlock()
	_ = manager.Health()
	if got := calls.Load(); got != 2 {
		t.Fatalf("version probes after lifetime cancellation = %d, want 2", got)
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	for attempt := 1; attempt <= 2; attempt++ {
		if err := manager.Shutdown(shutdownContext); err != nil {
			t.Fatalf("Shutdown attempt %d: %v", attempt, err)
		}
	}
}
