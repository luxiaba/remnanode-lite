package xray

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
