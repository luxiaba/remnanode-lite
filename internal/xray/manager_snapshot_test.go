package xray

type managerTestSnapshot struct {
	state               lifecycleState
	process             *processState
	pendingConfigJSON   []byte
	runtimeProcessEpoch uint64
	emptyConfigHash     string
	inboundHashCount    int
	inboundTagCount     int
}

func snapshotManagerForTest(manager *Manager) managerTestSnapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	return managerTestSnapshot{
		state:               manager.state,
		process:             manager.process,
		pendingConfigJSON:   append([]byte(nil), manager.runtime.pendingConfigJSON...),
		runtimeProcessEpoch: manager.runtime.runtimeProcessEpoch,
		emptyConfigHash:     manager.runtime.emptyConfigHash,
		inboundHashCount:    len(manager.runtime.inboundHashes),
		inboundTagCount:     len(manager.runtime.inboundTags),
	}
}
