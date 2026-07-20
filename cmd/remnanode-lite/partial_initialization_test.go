package main

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestNodeComponentCleanupHandlesPartialInitialization(t *testing.T) {
	tests := []struct {
		name        string
		withManager bool
	}{
		{name: "network monitor only"},
		{name: "manager created", withManager: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var networkStops atomic.Int32
			var managerShutdowns atomic.Int32
			var coreStops atomic.Int32
			cleanup := &nodeComponentCleanup{
				stopNetwork: func() { networkStops.Add(1) },
			}
			if test.withManager {
				cleanup.shutdownManager = func(context.Context) error {
					managerShutdowns.Add(1)
					return nil
				}
				cleanup.stopCore = func() error {
					coreStops.Add(1)
					return nil
				}
			}

			if err := cleanup.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := cleanup.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := networkStops.Load(); got != 1 {
				t.Fatalf("network stops = %d, want 1", got)
			}
			wantManagerCalls := int32(0)
			if test.withManager {
				wantManagerCalls = 1
			}
			if got := managerShutdowns.Load(); got != wantManagerCalls {
				t.Fatalf("manager shutdowns = %d, want %d", got, wantManagerCalls)
			}
			if got := coreStops.Load(); got != wantManagerCalls {
				t.Fatalf("core stops = %d, want %d", got, wantManagerCalls)
			}
		})
	}
}
