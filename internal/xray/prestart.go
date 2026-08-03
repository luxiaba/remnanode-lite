package xray

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxPreStartMatchesPerPattern = 256

func (m *Manager) runPreStart(ctx context.Context) {
	m.mu.RLock()
	provider := m.preStart
	m.mu.RUnlock()
	if provider == nil {
		return
	}
	enabled, patterns := provider.PreStartCleanupSockets()
	if !enabled {
		return
	}

	started := time.Now()
	removed := cleanupPreStartSockets(ctx, patterns)
	log.Printf(
		"pre-start socket cleanup completed (removed=%d elapsed=%s)",
		removed,
		time.Since(started).Round(time.Millisecond),
	)
}

func cleanupPreStartSockets(ctx context.Context, patterns []string) int {
	removed := 0
	for _, pattern := range patterns {
		if ctx.Err() != nil {
			return removed
		}
		for _, path := range resolvePreStartPattern(pattern) {
			if ctx.Err() != nil {
				return removed
			}
			info, err := os.Lstat(path)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					log.Printf("pre-start socket cleanup: lstat %q: %v", path, err)
				}
				continue
			}
			if info.Mode()&os.ModeSocket == 0 {
				continue
			}
			if err := os.Remove(path); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					log.Printf("pre-start socket cleanup: remove %q: %v", path, err)
				}
				continue
			}
			removed++
			log.Printf("pre-start socket cleanup: removed stale socket %q", path)
		}
	}
	return removed
}

func resolvePreStartPattern(pattern string) []string {
	if !strings.ContainsAny(pattern, "*?[]") {
		return []string{pattern}
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("pre-start socket cleanup: expand %q: %v", pattern, err)
		return nil
	}
	if len(matches) > maxPreStartMatchesPerPattern {
		log.Printf(
			"pre-start socket cleanup: pattern %q matched more than %d entries; remaining entries skipped",
			pattern,
			maxPreStartMatchesPerPattern,
		)
		matches = matches[:maxPreStartMatchesPerPattern]
	}
	return matches
}
