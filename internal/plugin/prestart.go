package plugin

type preStartSettings struct {
	enabled        bool
	cleanupSockets bool
	files          []string
}

// PreStartCleanupSockets returns the socket patterns for the next actual
// rw-core start. The returned slice is detached from the published snapshot.
func (s *State) PreStartCleanupSockets() (bool, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil || !s.active.preStart.enabled || !s.active.preStart.cleanupSockets {
		return false, nil
	}
	return true, append([]string(nil), s.active.preStart.files...)
}
