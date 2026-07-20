package system

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubNetworkStatsProvider struct {
	stats *NetworkInterface
	calls atomic.Int64
}

func (p *stubNetworkStatsProvider) GetDefaultInterface() *NetworkInterface {
	p.calls.Add(1)
	return p.stats
}

func TestNodeArchitectureMatchesProcessArchNames(t *testing.T) {
	// Oracle: Node.js os.arch()/process.arch documented values.
	tests := map[string]string{
		"386":     "ia32",
		"amd64":   "x64",
		"arm":     "arm",
		"arm64":   "arm64",
		"mipsle":  "mipsel",
		"ppc64le": "ppc64",
		"riscv64": "riscv64",
		"s390x":   "s390x",
	}
	for goarch, want := range tests {
		if got := nodeArchitecture(goarch); got != want {
			t.Errorf("nodeArchitecture(%q) = %q, want %q", goarch, got, want)
		}
	}
}

func TestNodePlatformMatchesProcessPlatformNames(t *testing.T) {
	tests := map[string]string{
		"darwin":  "darwin",
		"linux":   "linux",
		"windows": "win32",
		"illumos": "sunos",
		"solaris": "sunos",
	}
	for goos, want := range tests {
		if got := nodePlatform(goos); got != want {
			t.Errorf("nodePlatform(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestNodeSystemTypeMatchesOSNames(t *testing.T) {
	tests := map[string]string{
		"aix":     "AIX",
		"darwin":  "Darwin",
		"freebsd": "FreeBSD",
		"linux":   "Linux",
		"openbsd": "OpenBSD",
		"solaris": "SunOS",
		"windows": "Windows_NT",
	}
	for goos, want := range tests {
		if got := nodeSystemType(goos); got != want {
			t.Errorf("nodeSystemType(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestCollectorInfoUsesKernelIdentity(t *testing.T) {
	info := NewCollector(nil).Info()
	if info.Arch != nodeArchitecture(runtime.GOARCH) {
		t.Fatalf("arch = %q, want Node name for %q", info.Arch, runtime.GOARCH)
	}
	if info.Platform != nodePlatform(runtime.GOOS) {
		t.Fatalf("platform = %q, want Node name for %q", info.Platform, runtime.GOOS)
	}
	if info.Type != nodeSystemType(runtime.GOOS) {
		t.Fatalf("type = %q, want %q", info.Type, nodeSystemType(runtime.GOOS))
	}
	if info.Release == "" || info.Version == "" {
		t.Fatalf("kernel identity is incomplete: release=%q version=%q", info.Release, info.Version)
	}
	if info.Release == runtime.GOOS || info.Version == runtime.Version() {
		t.Fatalf("kernel fields contain Go runtime placeholders: %+v", info)
	}
}

func TestCollectorUsesInjectedNetworkProvider(t *testing.T) {
	t.Parallel()

	provided := &NetworkInterface{
		Interface:     "eth0",
		RxBytesPerSec: 10,
		TxBytesPerSec: 20,
		RxTotal:       30,
		TxTotal:       40,
	}
	provider := &stubNetworkStatsProvider{stats: provided}
	startedAt := time.Unix(123, 0)
	collector := newCollector(provider, startedAt)

	stats := collector.Stats()
	if stats.Interface == nil || *stats.Interface != *provided {
		t.Fatalf("interface stats = %#v, want %#v", stats.Interface, provided)
	}
	if stats.Interface == provided {
		t.Fatal("collector returned provider-owned network stats without copying")
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	if collector.startedAt != startedAt {
		t.Fatalf("collector startedAt = %v, want %v", collector.startedAt, startedAt)
	}
}

func TestCollectorSupportsNoNetworkProvider(t *testing.T) {
	t.Parallel()

	collector := newCollector(nil, time.Now())
	if stats := collector.Stats(); stats.Interface != nil {
		t.Fatalf("interface stats = %#v, want nil", stats.Interface)
	}
	if snapshot := collector.Snapshot(); snapshot.Stats.Interface != nil {
		t.Fatalf("snapshot interface stats = %#v, want nil", snapshot.Stats.Interface)
	}
}

func TestCollectorSupportsConcurrentReads(t *testing.T) {
	t.Parallel()

	provider := &stubNetworkStatsProvider{stats: &NetworkInterface{Interface: "eth0"}}
	collector := newCollector(provider, time.Now())
	const goroutines = 16
	const readsPerGoroutine = 10

	var readers sync.WaitGroup
	readers.Add(goroutines)
	for range goroutines {
		go func() {
			defer readers.Done()
			for range readsPerGoroutine {
				if got := collector.Stats().Interface; got == nil || got.Interface != "eth0" {
					t.Errorf("stats interface = %#v", got)
				}
				if got := collector.Snapshot().Stats.Interface; got == nil || got.Interface != "eth0" {
					t.Errorf("snapshot interface = %#v", got)
				}
			}
		}()
	}
	readers.Wait()

	wantCalls := int64(goroutines * readsPerGoroutine * 2)
	if got := provider.calls.Load(); got != wantCalls {
		t.Fatalf("provider calls = %d, want %d", got, wantCalls)
	}
}
