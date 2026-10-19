package xray

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStartDistinguishesNewProcessFromUnchangedSync(t *testing.T) {
	manager, _ := newLifecycleManager(t, "hold")
	manager.readinessProbe = func(context.Context) bool { return true }
	first := manager.Start(context.Background(), lifecycleStartRequest("client-a"))
	if !first.IsStarted || !first.CoreStarted {
		t.Fatalf("initial start = %+v", first)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "CoreStarted") || strings.Contains(string(raw), "coreStarted") {
		t.Fatal("internal transition leaked into public response")
	}
	second := manager.Start(context.Background(), lifecycleStartRequest("client-a"))
	if !second.IsStarted || second.CoreStarted {
		t.Fatalf("unchanged start = %+v", second)
	}
	req := lifecycleStartRequest("client-a")
	req.Internals.ForceRestart = true
	third := manager.Start(context.Background(), req)
	if !third.IsStarted || !third.CoreStarted {
		t.Fatalf("forced start = %+v", third)
	}
}

func TestFailedStartDoesNotPublishCoreStarted(t *testing.T) {
	manager, _ := newLifecycleManager(t, "exit-immediately")
	manager.readinessProbe = func(context.Context) bool { return false }
	response := manager.Start(context.Background(), lifecycleStartRequest("client-a"))
	if response.IsStarted || response.CoreStarted {
		t.Fatalf("failed start = %+v", response)
	}
}
