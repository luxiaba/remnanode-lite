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
		pendingConfigJSON:   append([]byte(nil), manager.pendingConfigJSON...),
		runtimeProcessEpoch: manager.runtimeProcessEpoch,
		emptyConfigHash:     manager.emptyConfigHash,
		inboundHashCount:    len(manager.inboundHashes),
		inboundTagCount:     len(manager.inboundTags),
	}
}
