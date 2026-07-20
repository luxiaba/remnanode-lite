package xray

import (
	"context"
	"testing"
	"time"
)

func TestUnchangedStartDrainsMutationBeforeHashDecision(t *testing.T) {
	manager, helper := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}

	manager.mu.RLock()
	initialProcess := manager.process
	initialOperationEpoch := manager.operationEpoch
	manager.mu.RUnlock()
	if initialProcess == nil {
		t.Fatal("initial process is missing")
	}

	leaseContext, release, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	token, _ := leaseContext.Value(mutationContextKey{}).(*mutationToken)

	request := lifecycleStartRequest("client-a")
	request.Internals.Hashes.Inbounds[0].UsersCount = 2
	request.Internals.Hashes.Inbounds[0].Hash = NewHashedSet("client-a", "client-b").Hash64String()
	clients := request.XrayConfig["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["clients"].([]any)
	request.XrayConfig["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["clients"] = append(
		clients,
		map[string]any{"id": "client-b"},
	)

	result := make(chan StartResponse, 1)
	go func() { result <- manager.Start(context.Background(), request) }()
	waitForState(t, manager, lifecycleStarting)
	select {
	case response := <-result:
		t.Fatalf("start passed an active process mutation lease: %+v", response)
	default:
	}

	if !manager.commitUserAdded(token, "public", "client-b") {
		t.Fatal("same-process hash commit was rejected in lifecycleStarting")
	}
	release()

	response := awaitStartResponse(t, result)
	if !response.IsStarted {
		t.Fatalf("unchanged start failed: %+v", response)
	}
	manager.mu.RLock()
	finalProcess := manager.process
	finalOperationEpoch := manager.operationEpoch
	manager.mu.RUnlock()
	if finalProcess != initialProcess {
		t.Fatal("unchanged start replaced the process")
	}
	if finalProcess.epoch != initialProcess.epoch || finalProcess.socket != initialProcess.socket {
		t.Fatalf("unchanged start changed identity: before=%d/%q after=%d/%q",
			initialProcess.epoch, initialProcess.socket, finalProcess.epoch, finalProcess.socket)
	}
	if finalOperationEpoch != initialOperationEpoch+1 {
		t.Fatalf("operation epoch = %d, want %d", finalOperationEpoch, initialOperationEpoch+1)
	}
	if starts := helper.starts.Load(); starts != 1 {
		t.Fatalf("spawn count = %d, want 1", starts)
	}
}

func TestStopDrainsMutationBeforeTerminatingProcess(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}

	leaseContext, release, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	token, _ := leaseContext.Value(mutationContextKey{}).(*mutationToken)
	result := make(chan StopResponse, 1)
	go func() { result <- manager.Stop() }()
	waitForState(t, manager, lifecycleStopping)
	select {
	case response := <-result:
		t.Fatalf("stop passed an active process mutation lease: %+v", response)
	default:
	}
	if !manager.commitUserRemoved(token, "public", "client-a") {
		t.Fatal("same-process hash commit was rejected in lifecycleStopping")
	}
	release()

	select {
	case response := <-result:
		if !response.IsStopped {
			t.Fatalf("stop failed: %+v", response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not resume after mutation release")
	}
	assertStoppedAndCleared(t, manager)
}

func TestNestedMutationReleaseCannotUnlockOuterProcessLease(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	request := lifecycleStartRequest("client-a")
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}

	outerContext, releaseOuter, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin outer mutation: %v", err)
	}
	_, releaseNested, err := manager.BeginMutation(outerContext)
	if err != nil {
		releaseOuter()
		t.Fatalf("begin nested mutation: %v", err)
	}
	releaseNested()

	request = lifecycleStartRequest("client-a")
	request.Internals.ForceRestart = true
	result := make(chan StartResponse, 1)
	go func() { result <- manager.Start(context.Background(), request) }()
	waitForState(t, manager, lifecycleStarting)
	select {
	case response := <-result:
		releaseOuter()
		t.Fatalf("nested release unlocked the outer lease: %+v", response)
	default:
	}

	releaseOuter()
	if _, release, err := manager.BeginMutation(outerContext); err == nil {
		release()
		t.Fatal("released mutation token was accepted")
	}
	if response := awaitStartResponse(t, result); !response.IsStarted {
		t.Fatalf("forced restart failed after outer release: %+v", response)
	}
}

func TestRestartAssignsNewProcessEpochAndSocket(t *testing.T) {
	manager, helper := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	request := lifecycleStartRequest("client-a")
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}
	manager.mu.RLock()
	first := manager.process
	manager.mu.RUnlock()

	request = lifecycleStartRequest("client-a")
	request.Internals.ForceRestart = true
	if response := manager.Start(context.Background(), request); !response.IsStarted {
		t.Fatalf("forced restart failed: %+v", response)
	}
	manager.mu.RLock()
	second := manager.process
	manager.mu.RUnlock()
	if first == nil || second == nil || first == second {
		t.Fatalf("process identity did not change: first=%p second=%p", first, second)
	}
	if second.epoch <= first.epoch {
		t.Fatalf("process epoch did not advance: first=%d second=%d", first.epoch, second.epoch)
	}
	if second.socket == first.socket {
		t.Fatalf("replacement reused abstract socket %q", second.socket)
	}
	if starts := helper.starts.Load(); starts != 2 {
		t.Fatalf("spawn count = %d, want 2", starts)
	}
}

func TestPrepareFailureRestoresPreviousProcessMutationAdmission(t *testing.T) {
	manager, helper := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}
	manager.mu.RLock()
	previous := manager.process
	manager.mu.RUnlock()

	request := lifecycleStartRequest("client-a")
	request.Internals.ForceRestart = true
	request.XrayConfig["unsupported"] = make(chan struct{})
	if response := manager.Start(context.Background(), request); response.IsStarted {
		t.Fatalf("invalid config unexpectedly started: %+v", response)
	}
	manager.mu.RLock()
	current := manager.process
	state := manager.state
	manager.mu.RUnlock()
	if current != previous || state != lifecycleRunning {
		t.Fatalf("prepare failure did not restore running process: state=%s previous=%p current=%p", state, previous, current)
	}
	if starts := helper.starts.Load(); starts != 1 {
		t.Fatalf("prepare failure spawned another process: %d", starts)
	}
	_, release, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("mutation admission remained closed after prepare failure: %v", err)
	}
	release()
}

func TestPrepareFailureDoesNotReviveProcessThatExitedDuringStart(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}

	_, releaseMutation, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	manager.mu.RLock()
	previous := manager.process
	manager.mu.RUnlock()
	if previous == nil {
		t.Fatal("running process is missing")
	}

	request := lifecycleStartRequest("client-a")
	request.Internals.ForceRestart = true
	request.XrayConfig["unsupported"] = make(chan struct{})
	result := make(chan StartResponse, 1)
	go func() { result <- manager.Start(context.Background(), request) }()
	waitForState(t, manager, lifecycleStarting)

	if err := previous.cmd.Process.Kill(); err != nil {
		releaseMutation()
		t.Fatalf("kill previous process: %v", err)
	}
	select {
	case <-previous.monitorDone:
	case <-time.After(3 * time.Second):
		releaseMutation()
		t.Fatal("previous process monitor did not finish")
	}
	releaseMutation()

	response := awaitStartResponse(t, result)
	if response.IsStarted {
		t.Fatalf("invalid replacement unexpectedly started: %+v", response)
	}
	waitForState(t, manager, lifecycleStopped)
	assertStoppedAndCleared(t, manager)
	if _, release, err := manager.BeginMutation(context.Background()); err == nil {
		release()
		t.Fatal("dead previous process still admitted mutations")
	}
}

func TestCanceledStartDoesNotReviveProcessThatExitedWhileWaitingForMutation(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}

	_, releaseMutation, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	defer releaseMutation()
	manager.mu.RLock()
	previous := manager.process
	manager.mu.RUnlock()
	if previous == nil {
		t.Fatal("running process is missing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := lifecycleStartRequest("client-a")
	request.Internals.ForceRestart = true
	result := make(chan StartResponse, 1)
	go func() { result <- manager.Start(ctx, request) }()
	waitForState(t, manager, lifecycleStarting)

	if err := previous.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill previous process: %v", err)
	}
	select {
	case <-previous.monitorDone:
	case <-time.After(3 * time.Second):
		t.Fatal("previous process monitor did not finish")
	}
	cancel()
	releaseMutation()

	response := awaitStartResponse(t, result)
	if response.IsStarted {
		t.Fatalf("canceled replacement unexpectedly started: %+v", response)
	}
	waitForState(t, manager, lifecycleStopped)
	assertStoppedAndCleared(t, manager)
	if _, release, err := manager.BeginMutation(context.Background()); err == nil {
		release()
		t.Fatal("dead previous process still admitted mutations")
	}
}

func TestNaturalExitDetachesActiveMutationTokenFromReplacement(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("initial start failed: %+v", response)
	}
	leaseContext, release, err := manager.BeginMutation(context.Background())
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	defer release()
	token, _ := leaseContext.Value(mutationContextKey{}).(*mutationToken)
	oldProcess := token.process
	if err := oldProcess.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill old process: %v", err)
	}
	waitForState(t, manager, lifecycleStopped)

	if response := manager.Start(context.Background(), lifecycleStartRequest("client-a")); !response.IsStarted {
		t.Fatalf("replacement start failed: %+v", response)
	}
	manager.mu.RLock()
	replacement := manager.process
	manager.mu.RUnlock()
	if replacement == nil {
		t.Fatal("replacement process is missing")
	}
	if replacement == oldProcess || replacement.socket == oldProcess.socket {
		t.Fatalf("replacement identity is not isolated: old=%p/%q new=%p/%q",
			oldProcess, oldProcess.socket, replacement, replacement.socket)
	}
	if manager.commitUserAdded(token, "public", "stale-user") {
		t.Fatal("natural-exit token committed into replacement runtime state")
	}
}
