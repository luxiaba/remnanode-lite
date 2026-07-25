package xray

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/executil"
)

const (
	versionProbeTimeout  = 5 * time.Second
	versionProbeRetry    = 30 * time.Second
	versionOutputMaxSize = 4 << 10
)

// Health returns the cached lifecycle and version view. When a previous core
// version probe failed, it schedules one throttled background retry. The
// Manager lock makes lifecycle state, retry scheduling, and Shutdown's join
// gate one atomic boundary.
func (m *Manager) Health() HealthResponse {
	m.mu.Lock()
	running := m.state == lifecycleRunning
	version := m.version.cached
	retryVersion := !m.version.shutdown && m.version.context.Err() == nil && version == nil && m.state != lifecycleStarting && !m.version.busy &&
		!time.Now().Before(m.version.nextProbe)
	var probeContext context.Context
	if retryVersion {
		m.version.busy = true
		m.version.nextProbe = time.Now().Add(versionProbeRetry)
		m.version.wg.Add(1)
		probeContext = m.version.context
	}
	m.mu.Unlock()
	if retryVersion {
		go func() {
			defer m.version.wg.Done()
			m.refreshUnknownVersion(probeContext)
		}()
	}

	return HealthResponse{
		IsAlive:                  true,
		XrayInternalStatusCached: running,
		XrayVersion:              version,
		NodeVersion:              m.nodeVersion,
	}
}

func (m *Manager) refreshVersion(parent context.Context) {
	version := m.probeVersion(parent)
	m.mu.Lock()
	m.publishVersionLocked(version)
	m.mu.Unlock()
}

func (m *Manager) probeVersion(parent context.Context) *string {
	m.mu.RLock()
	override := m.version.coreOverride
	m.mu.RUnlock()
	if override != "" {
		return &override
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, versionProbeTimeout)
	defer cancel()

	m.mu.RLock()
	probe := m.version.probe
	xrayBin := m.xrayBin
	m.mu.RUnlock()

	var version string
	var err error
	if probe != nil {
		version, err = probe(ctx)
	} else {
		var result executil.Result
		result, err = executil.RunWithEnv(
			ctx,
			nil,
			versionOutputMaxSize,
			sanitizedChildEnvironment(os.Environ()),
			xrayBin,
			"version",
		)
		if err == nil {
			version = parseVersionLine(string(result.Stdout))
		}
	}
	if err != nil || version == "" {
		return nil
	}
	return &version
}

func (m *Manager) publishVersionLocked(version *string) {
	m.version.cached = version
	if version == nil {
		m.version.nextProbe = time.Now().Add(versionProbeRetry)
	} else {
		m.version.nextProbe = time.Time{}
	}
}

func (m *Manager) refreshUnknownVersion(parent context.Context) {
	version := m.probeVersion(parent)
	m.mu.Lock()
	if !m.version.shutdown && m.version.cached == nil && version != nil {
		m.publishVersionLocked(version)
	}
	m.version.busy = false
	m.mu.Unlock()
}

// Shutdown permanently stops background version recovery. It is reserved for
// node process shutdown; Stop remains reusable for the public xray/stop route.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.version.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.version.shutdown = true
		cancel := m.version.cancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		go func() {
			m.version.wg.Wait()
			close(m.version.shutdownDone)
		}()
	})

	select {
	case <-m.version.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var xraySemverRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// parseVersionLine returns semver like "26.3.27", matching official node semver coercion.
func parseVersionLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if version := extractSemver(line); version != "" {
			return version
		}
	}
	return ""
}

func coerceSemver(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	return extractSemver(raw)
}

func extractSemver(raw string) string {
	if raw == "" {
		return ""
	}
	return xraySemverRe.FindString(raw)
}
