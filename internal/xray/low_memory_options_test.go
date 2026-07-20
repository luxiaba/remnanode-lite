package xray

import (
	"context"
	"testing"
	"time"

	"github.com/Luxiaba/remnanode-lite/internal/system"
)

func TestManagerLowMemoryStartupTimeoutPolicy(t *testing.T) {
	tests := []struct {
		name              string
		lowMemory         bool
		configuredTimeout time.Duration
		want              time.Duration
	}{
		{name: "normal default", want: 20 * time.Second},
		{name: "low-memory default", lowMemory: true, want: 90 * time.Second},
		{name: "explicit timeout wins", lowMemory: true, configuredTimeout: 7 * time.Second, want: 7 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := NewManager(Options{
				XrayBin:     "unused",
				GeoDir:      t.TempDir(),
				LogDir:      t.TempDir(),
				LowMemory:   test.lowMemory,
				NodeVersion: "2.8.0",
				CoreVersion: "26.6.27",
				System:      system.NewCollector(nil),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
			manager.startupTimeout = test.configuredTimeout

			if got := manager.grpcStartupTimeout(); got != test.want {
				t.Fatalf("startup timeout = %s, want %s", got, test.want)
			}
		})
	}
}
