package xray

import (
	"context"
	"regexp"
	"strings"
	"time"
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
	binary := m.xrayBin
	if m.process != nil && m.process.binary != "" {
		binary = m.process.binary
	}
	m.mu.RUnlock()
	return m.probeVersionForBinary(parent, binary)
}

func (m *Manager) probeVersionForBinary(parent context.Context, binary string) *string {
	m.mu.RLock()
	override := m.version.coreOverride
	bundledBinary := m.xrayBin
	m.mu.RUnlock()
	if override != "" && binary == bundledBinary {
		return &override
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, versionProbeTimeout)
	defer cancel()

	m.mu.RLock()
	probe := m.version.probe
	m.mu.RUnlock()

	var version string
	var err error
	if probe != nil {
		version, err = probe(ctx)
	} else {
		version, err = probeCoreBinary(ctx, binary)
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

var (
	xraySemverRe     = regexp.MustCompile(`\d+\.\d+\.\d+`)
	xrayPrereleaseRe = regexp.MustCompile(`^[0-9A-Za-z.-]+`)
)

// parseVersionLine returns semver like "26.3.27" or "26.3.27-rc.1",
// matching official node coercion, which omits build metadata.
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
	location := xraySemverRe.FindStringIndex(raw)
	if location == nil {
		return ""
	}

	version := raw[location[0]:location[1]]
	if !validSemverCore(version) {
		return ""
	}
	suffix := raw[location[1]:]
	if !strings.HasPrefix(suffix, "-") {
		return version
	}

	prerelease := xrayPrereleaseRe.FindString(suffix[1:])
	if prerelease == "" {
		return version
	}
	identifiers := strings.Split(prerelease, ".")
	valid := 0
	for _, identifier := range identifiers {
		invalidNumeric := len(identifier) > 1 && identifier[0] == '0' && strings.Trim(identifier, "0123456789") == ""
		if identifier == "" || invalidNumeric {
			break
		}
		valid++
	}
	if valid == 0 {
		return version
	}
	return version + "-" + strings.Join(identifiers[:valid], ".")
}

func validSemverCore(version string) bool {
	const maxSafeInteger = "9007199254740991"
	for _, identifier := range strings.Split(version, ".") {
		if len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
		if len(identifier) > len(maxSafeInteger) ||
			(len(identifier) == len(maxSafeInteger) && identifier > maxSafeInteger) {
			return false
		}
	}
	return true
}
