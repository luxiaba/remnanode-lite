package system

import "time"

// NetworkStatsProvider supplies the latest default-interface counters. The
// returned value is copied before it is included in a response.
type NetworkStatsProvider interface {
	GetDefaultInterface() *NetworkInterface
}

// Collector owns the process-relative system collection context. It does not
// start or stop its network provider; the composition root owns that lifecycle.
type Collector struct {
	network   NetworkStatsProvider
	startedAt time.Time
}

func NewCollector(network NetworkStatsProvider) *Collector {
	return newCollector(network, time.Now())
}

func newCollector(network NetworkStatsProvider, startedAt time.Time) *Collector {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Collector{network: network, startedAt: startedAt}
}

func (c *Collector) Info() Info {
	return collectInfo()
}

func (c *Collector) Stats() Stats {
	if c == nil {
		return collectStats(nil, time.Now())
	}
	return collectStats(c.network, c.startedAt)
}

func (c *Collector) Snapshot() Snapshot {
	return Snapshot{
		Info:  c.Info(),
		Stats: c.Stats(),
	}
}

func collectStats(network NetworkStatsProvider, startedAt time.Time) Stats {
	free, total := memoryFreeAndTotal()
	used := uint64(0)
	if total > free {
		used = total - free
	}
	return Stats{
		MemoryFree: free,
		MemoryUsed: used,
		Uptime:     uptime(startedAt),
		LoadAvg:    loadAvg(),
		Interface:  networkInterfaceStats(network),
	}
}

func networkInterfaceStats(provider NetworkStatsProvider) *NetworkInterface {
	if provider == nil {
		return nil
	}
	stats := provider.GetDefaultInterface()
	if stats == nil {
		return nil
	}
	copy := *stats
	return &copy
}
