package plugin

import (
	"strings"
	"testing"
)

func TestValidatePreStartConfig(t *testing.T) {
	t.Parallel()

	maximumFiles := make([]any, maxPreStartSocketFiles)
	for index := range maximumFiles {
		maximumFiles[index] = "/dev/shm/rw-*.sock"
	}
	valid := []map[string]any{
		{"preStart": map[string]any{}},
		{"preStart": map[string]any{"enabled": false}},
		{"preStart": map[string]any{
			"enabled": true,
			"cleanupSockets": map[string]any{
				"enabled": true,
				"files":   []any{"  /dev/shm/*.sock  ", "/run/rw-?.sock", "/tmp/rw-[0-9].sock"},
			},
		}},
		{"preStart": map[string]any{
			"cleanupSockets": map[string]any{"enabled": false, "files": maximumFiles},
		}},
		{"preStart": map[string]any{
			"cleanupSockets": map[string]any{"enabled": true, "files": []any{"/" + strings.Repeat("x", 1_024)}},
		}},
	}
	for index, config := range valid {
		if err := ValidatePluginConfig(config); err != nil {
			t.Errorf("valid config %d rejected: %v", index, err)
		}
	}
}

func TestValidatePreStartConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tooManyFiles := make([]any, maxPreStartSocketFiles+1)
	for index := range tooManyFiles {
		tooManyFiles[index] = "/dev/shm/rw.sock"
	}
	tests := []map[string]any{
		{"preStart": nil},
		{"preStart": map[string]any{"enabled": nil}},
		{"preStart": map[string]any{"cleanupSockets": nil}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"files": []any{}}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true, "files": tooManyFiles}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true, "files": []any{nil}}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true, "files": []any{"   "}}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true, "files": []any{"relative.sock"}}}},
		{"preStart": map[string]any{"cleanupSockets": map[string]any{"enabled": true, "files": []any{"/tmp/rw\x00.sock"}}}},
	}
	for index, config := range tests {
		if err := ValidatePluginConfig(config); err == nil {
			t.Errorf("invalid config %d accepted: %#v", index, config)
		}
	}
}

func TestSyncPublishesDetachedPreStartPatterns(t *testing.T) {
	t.Parallel()

	state := NewState()
	service, _ := newReadyService(t, state, nil)
	request := mustSyncPlugin(t, map[string]any{
		"uuid": "00000000-0000-4000-8000-000000000001",
		"name": "pre-start-test",
		"config": map[string]any{"preStart": map[string]any{
			"enabled": true,
			"cleanupSockets": map[string]any{
				"enabled": true,
				"files":   []any{"  /dev/shm/*.sock  "},
			},
		}},
	})
	if response := service.Sync(request); !response.Accepted {
		t.Fatal("pre-start plugin config was rejected")
	}

	enabled, patterns := state.PreStartCleanupSockets()
	if !enabled || len(patterns) != 1 || patterns[0] != "/dev/shm/*.sock" {
		t.Fatalf("pre-start snapshot = enabled %v, patterns %#v", enabled, patterns)
	}
	patterns[0] = "/changed"
	_, current := state.PreStartCleanupSockets()
	if current[0] != "/dev/shm/*.sock" {
		t.Fatalf("caller mutated published snapshot: %#v", current)
	}

	if response := service.Sync(nil); !response.Accepted {
		t.Fatal("active plugin cleanup was rejected")
	}
	if enabled, patterns := state.PreStartCleanupSockets(); enabled || patterns != nil {
		t.Fatalf("pre-start state survived plugin cleanup: enabled %v, patterns %#v", enabled, patterns)
	}
}
