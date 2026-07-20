package system

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNetworkMonitorReplacesSamplesAndClearsMissingInterface(t *testing.T) {
	t.Parallel()

	started := time.Unix(100, 0)
	monitor := &NetworkMonitor{
		defaultIface: "eth0",
		previous: map[string]interfaceSample{
			"eth0":  {rxBytes: 100, txBytes: 200, timestamp: started},
			"stale": {rxBytes: 1, txBytes: 1, timestamp: started},
		},
	}
	monitor.updateSamples(map[string]interfaceSample{
		"eth0": {rxBytes: 300, txBytes: 500},
	}, started.Add(2*time.Second))

	if len(monitor.previous) != 1 {
		t.Fatalf("previous samples = %d, want 1", len(monitor.previous))
	}
	if _, exists := monitor.previous["stale"]; exists {
		t.Fatal("removed interface remained in previous samples")
	}
	if monitor.current == nil || monitor.current.RxBytesPerSec != 100 || monitor.current.TxBytesPerSec != 150 {
		t.Fatalf("current sample = %#v", monitor.current)
	}

	monitor.updateSamples(map[string]interfaceSample{}, started.Add(3*time.Second))
	if monitor.current != nil || len(monitor.previous) != 0 {
		t.Fatalf("missing interface left stale state: current=%#v previous=%d", monitor.current, len(monitor.previous))
	}
}

func TestNetworkMonitorFollowsDefaultInterfaceChanges(t *testing.T) {
	t.Parallel()

	started := time.Unix(100, 0)
	monitor := &NetworkMonitor{
		defaultIface: "eth0",
		previous: map[string]interfaceSample{
			"eth0": {rxBytes: 100, txBytes: 100, timestamp: started},
			"ens3": {rxBytes: 200, txBytes: 300, timestamp: started},
		},
	}
	monitor.updateSamplesForInterface(map[string]interfaceSample{
		"eth0": {rxBytes: 150, txBytes: 150},
		"ens3": {rxBytes: 400, txBytes: 700},
	}, started.Add(2*time.Second), "ens3")

	if monitor.current == nil || monitor.current.Interface != "ens3" ||
		monitor.current.RxBytesPerSec != 100 || monitor.current.TxBytesPerSec != 200 {
		t.Fatalf("current sample after route change = %#v", monitor.current)
	}
	if monitor.defaultIface != "ens3" {
		t.Fatalf("default interface = %q, want ens3", monitor.defaultIface)
	}
}

func TestNetworkMonitorStopIsConcurrentAndIdempotent(t *testing.T) {
	t.Parallel()

	pollStarted := make(chan struct{})
	releasePoll := make(chan struct{})
	var reads atomic.Int64
	monitor := newNetworkMonitor(networkMonitorConfig{
		available:    true,
		pollInterval: time.Millisecond,
		readSamples: func() map[string]interfaceSample {
			if reads.Add(1) == 2 {
				close(pollStarted)
				<-releasePoll
			}
			return map[string]interfaceSample{
				"eth0": {rxBytes: uint64(reads.Load()), txBytes: uint64(reads.Load())},
			}
		},
		resolveIface: func() string { return "eth0" },
		now:          time.Now,
	})
	select {
	case <-pollStarted:
	case <-time.After(time.Second):
		close(releasePoll)
		monitor.Stop()
		t.Fatal("network poll did not start")
	}

	var callers sync.WaitGroup
	returned := make(chan struct{}, 8)
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			monitor.Stop()
			returned <- struct{}{}
		}()
	}

	// The first caller closes stop, but every caller must wait for the active
	// poll to return and the loop to close done.
	select {
	case <-monitor.stop:
	case <-time.After(time.Second):
		close(releasePoll)
		t.Fatal("Stop did not signal the poller")
	}
	select {
	case <-returned:
		close(releasePoll)
		t.Fatal("Stop returned while a poll was still running")
	default:
	}
	close(releasePoll)

	finished := make(chan struct{})
	go func() {
		callers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("concurrent Stop callers did not return after poll exit")
	}
	select {
	case <-monitor.done:
	default:
		t.Fatal("Stop returned before the poller closed done")
	}
}

func TestZeroValueNetworkMonitorStopDoesNotPanic(t *testing.T) {
	t.Parallel()
	var monitor NetworkMonitor
	monitor.Stop()
	monitor.Stop()
	var nilMonitor *NetworkMonitor
	nilMonitor.Stop()
}

func TestUnavailableNetworkMonitorDoesNotStartPoller(t *testing.T) {
	t.Parallel()

	var reads atomic.Int64
	var resolves atomic.Int64
	monitor := newNetworkMonitor(networkMonitorConfig{
		available: false,
		readSamples: func() map[string]interfaceSample {
			reads.Add(1)
			return nil
		},
		resolveIface: func() string {
			resolves.Add(1)
			return "eth0"
		},
	})

	select {
	case <-monitor.done:
	default:
		t.Fatal("unavailable monitor did not close done during construction")
	}
	monitor.Stop()
	if got := reads.Load(); got != 0 {
		t.Fatalf("sample reads = %d, want 0", got)
	}
	if got := resolves.Load(); got != 0 {
		t.Fatalf("interface resolutions = %d, want 0", got)
	}
}

func TestParseDefaultInterfaceChoosesLowestMetric(t *testing.T) {
	t.Parallel()

	routes := []byte(`Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0100000A 0003 0 0 200 00000000 0 0 0
eth1 00000000 0100000A 0003 0 0 20 00000000 0 0 0
eth2 00000000 0100000A 0003 0 0 100 00000000 0 0 0
`)
	if got := parseDefaultInterface(routes); got != "eth1" {
		t.Fatalf("default interface = %q, want eth1", got)
	}
}

func TestParseDefaultInterfaceSkipsMalformedAndInactiveRoutes(t *testing.T) {
	t.Parallel()

	routes := []byte(`Destination Iface Metric Flags Mask Gateway
00000000 bad-metric nope 0003 00000000 0100000A
00000000 down0 1 0000 00000000 0100000A
00000000 reject0 1 0201 00000000 0100000A
00000000 host-route 2 0001 FFFFFFFF 00000000
not-hex malformed 0 0001 00000000 00000000
00000000 truncated
00000000 ens3 50 0001 00000000 0100000A
`)
	if got := parseDefaultInterface(routes); got != "ens3" {
		t.Fatalf("default interface = %q, want ens3", got)
	}
}

func TestParseDefaultInterfaceSkipsBlackholePseudoInterface(t *testing.T) {
	t.Parallel()

	routes := []byte(`Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
* 00000000 00000000 0001 0 0 1 00000000 0 0 0
ens3 00000000 0100000A 0003 0 0 50 00000000 0 0 0
`)
	if got := parseDefaultInterface(routes); got != "ens3" {
		t.Fatalf("default interface = %q, want ens3", got)
	}
}

func TestParseDefaultInterfaceRejectsInvalidHeader(t *testing.T) {
	t.Parallel()

	if got := parseDefaultInterface([]byte("Iface Destination Flags\neth0 00000000 0001\n")); got != "" {
		t.Fatalf("default interface = %q, want empty for incomplete header", got)
	}
}
