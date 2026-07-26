package xray

import (
	"log/slog"
	"sort"
)

type runtimeHashState struct {
	emptyConfigHash string
	inboundHashes   map[string]*HashedSet
	inboundTags     map[string]struct{}
}

func buildRuntimeHashState(hashes ConfigHash, config map[string]any) runtimeHashState {
	state := runtimeHashState{
		emptyConfigHash: hashes.EmptyConfig,
		inboundHashes:   make(map[string]*HashedSet),
		inboundTags:     make(map[string]struct{}),
	}

	validTags := make(map[string]struct{}, len(hashes.Inbounds))
	for _, inbound := range hashes.Inbounds {
		if inbound.Tag != "" {
			validTags[inbound.Tag] = struct{}{}
		}
	}

	rawInbounds, ok := config["inbounds"].([]any)
	if !ok {
		return state
	}

	for _, item := range rawInbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := inbound["tag"].(string)
		if tag == "" {
			continue
		}
		if _, allowed := validTags[tag]; !allowed {
			continue
		}

		ids := extractClientIDs(inbound)
		set := NewHashedSet(ids...)
		state.inboundHashes[tag] = set
		state.inboundTags[tag] = struct{}{}
		slog.Debug("extracted inbound users", "tag", tag, "count", set.Size(), "hash", set.Hash64String())
	}
	return state
}

func (m *Manager) applyRuntimeHashStateLocked(state runtimeHashState) {
	m.runtime.emptyConfigHash = state.emptyConfigHash
	m.runtime.inboundHashes = state.inboundHashes
	m.runtime.inboundTags = state.inboundTags
}

func (m *Manager) extractUsersFromConfigLocked(hashes ConfigHash, config map[string]any) {
	m.applyRuntimeHashStateLocked(buildRuntimeHashState(hashes, config))
}

func (m *Manager) isNeedRestartCoreLocked(incoming ConfigHash) bool {
	if m.runtime.emptyConfigHash == "" {
		return true
	}
	if incoming.EmptyConfig != m.runtime.emptyConfigHash {
		slog.Warn("detected changes in Xray Core base configuration")
		return true
	}
	if len(incoming.Inbounds) != len(m.runtime.inboundHashes) {
		slog.Warn("number of Xray Core inbounds has changed")
		return true
	}

	for tag, usersSet := range m.runtime.inboundHashes {
		var incomingInbound *InboundHash
		for i := range incoming.Inbounds {
			if incoming.Inbounds[i].Tag == tag {
				incomingInbound = &incoming.Inbounds[i]
				break
			}
		}
		if incomingInbound == nil {
			slog.Warn("inbound no longer exists in Xray Core configuration", "tag", tag)
			return true
		}
		if usersSet.Hash64String() != incomingInbound.Hash {
			slog.Warn(
				"user configuration changed for inbound",
				"tag", tag,
				"current", usersSet.Hash64String(),
				"incoming", incomingInbound.Hash,
			)
			return true
		}
	}

	slog.Info("Xray Core configuration is up-to-date - no restart required")
	return false
}

func (m *Manager) commitUserAdded(token *mutationToken, inboundTag, userUUID string) bool {
	if inboundTag == "" || userUUID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.mutationTokenCurrentLocked(token) {
		return false
	}

	set, ok := m.runtime.inboundHashes[inboundTag]
	if !ok {
		if m.runtime.inboundHashes == nil {
			m.runtime.inboundHashes = make(map[string]*HashedSet)
		}
		set = NewHashedSet()
		m.runtime.inboundHashes[inboundTag] = set
	}
	if m.runtime.inboundTags == nil {
		m.runtime.inboundTags = make(map[string]struct{})
	}
	set.Add(userUUID)
	m.runtime.inboundTags[inboundTag] = struct{}{}
	return true
}

func (m *Manager) commitUserRemoved(token *mutationToken, inboundTag, userUUID string) bool {
	if inboundTag == "" || userUUID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.mutationTokenCurrentLocked(token) {
		return false
	}

	set, ok := m.runtime.inboundHashes[inboundTag]
	if !ok {
		return true
	}
	set.Delete(userUUID)
	if set.Size() == 0 {
		delete(m.runtime.inboundHashes, inboundTag)
		delete(m.runtime.inboundTags, inboundTag)
		slog.Warn("inbound has no users, clearing hash map", "tag", inboundTag)
	}
	return true
}

func (m *Manager) clearHashStateLocked() {
	m.runtime.emptyConfigHash = ""
	m.runtime.inboundHashes = nil
}

func extractClientIDs(inbound map[string]any) []string {
	settings, ok := inbound["settings"].(map[string]any)
	if !ok {
		return nil
	}
	clients, ok := settings["clients"].([]any)
	if !ok {
		return nil
	}

	ids := make([]string, 0, len(clients))
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := client["id"].(string)
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
