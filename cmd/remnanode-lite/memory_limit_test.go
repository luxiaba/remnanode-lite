package main

import (
	"runtime/debug"
	"testing"

	"github.com/Luxiaba/remnanode-lite/internal/config"
)

func TestApplyMemoryLimitPolicy(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want int64
	}{
		{
			name: "low-memory default",
			cfg:  config.Config{LowMemory: true},
			want: 180 << 20,
		},
		{
			name: "explicit limit wins",
			cfg: config.Config{
				LowMemory:          true,
				GoMemoryLimitSet:   true,
				GoMemoryLimitBytes: 96 << 20,
			},
			want: 96 << 20,
		},
		{
			name: "normal mode leaves limit unchanged",
			cfg:  config.Config{},
			want: 333 << 20,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const sentinel = int64(333 << 20)
			previous := debug.SetMemoryLimit(sentinel)
			defer debug.SetMemoryLimit(previous)

			applyMemoryLimit(test.cfg)
			got := debug.SetMemoryLimit(sentinel)
			if got != test.want {
				t.Fatalf("Go memory limit = %d, want %d", got, test.want)
			}
		})
	}
}
