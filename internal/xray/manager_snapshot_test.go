package xray

import (
	"testing"
	"time"
)

type managerTestSnapshot struct {
	state               lifecycleState
	process             *processState
	pendingConfigJSON   []byte
	pendingConfigSet    bool
	runtimeProcessEpoch uint64
	emptyConfigHash     string
	inboundHashCount    int
	inboundHashesSet    bool
	inboundTagCount     int
	inboundTagsSet      bool
}

// versionTestSnapshot keeps tests from reading version recovery fields without
// the Manager lock. It deliberately records only observable scheduling state;
// tests still drive the public Health and Shutdown paths.
type versionTestSnapshot struct {
	cached     *string
	busy       bool
	nextProbe  time.Time
	shutdown   bool
	contextErr error
}

func snapshotManagerForTest(manager *Manager) managerTestSnapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	return managerTestSnapshot{
		state:               manager.state,
		process:             manager.process,
		pendingConfigJSON:   append([]byte(nil), manager.runtime.pendingConfigJSON...),
		pendingConfigSet:    manager.runtime.pendingConfigJSON != nil,
		runtimeProcessEpoch: manager.runtime.runtimeProcessEpoch,
		emptyConfigHash:     manager.runtime.emptyConfigHash,
		inboundHashCount:    len(manager.runtime.inboundHashes),
		inboundHashesSet:    manager.runtime.inboundHashes != nil,
		inboundTagCount:     len(manager.runtime.inboundTags),
		inboundTagsSet:      manager.runtime.inboundTags != nil,
	}
}

func snapshotVersionForTest(manager *Manager) versionTestSnapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	return versionTestSnapshot{
		cached:     manager.xrayVersion,
		busy:       manager.versionProbeBusy,
		nextProbe:  manager.nextVersionProbe,
		shutdown:   manager.versionProbeShutdown,
		contextErr: manager.versionProbeContext.Err(),
	}
}

func setVersionRetryNowForTest(manager *Manager) {
	manager.mu.Lock()
	manager.nextVersionProbe = time.Time{}
	manager.mu.Unlock()
}

func waitForVersionProbeIdle(t testing.TB, manager *Manager) versionTestSnapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		snapshot := snapshotVersionForTest(manager)
		if !snapshot.busy {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatal("background version probe did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}
