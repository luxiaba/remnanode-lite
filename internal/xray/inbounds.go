package xray

func extractInboundTags(config map[string]any) []string {
	raw, ok := config["inbounds"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	tags := make([]string, 0, len(items))
	for _, item := range items {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag, ok := inbound["tag"].(string)
		if !ok || tag == "" {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func (m *Manager) AddInboundTag(tag string) {
	if tag == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtime.inboundTags == nil {
		m.runtime.inboundTags = make(map[string]struct{})
	}
	m.runtime.inboundTags[tag] = struct{}{}
}

func (m *Manager) InboundTags() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.runtime.inboundTags) == 0 {
		return nil
	}
	tags := make([]string, 0, len(m.runtime.inboundTags))
	for tag := range m.runtime.inboundTags {
		tags = append(tags, tag)
	}
	return tags
}

func (m *Manager) clearInboundTagsLocked() {
	m.runtime.inboundTags = nil
}
