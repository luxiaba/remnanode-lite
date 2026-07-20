package xray

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Luxiaba/remnanode-lite/internal/executil"
	"github.com/Luxiaba/remnanode-lite/internal/system"
)

type Options struct {
	// Lifetime bounds initial and background version probes. A nil value uses context.Background.
	Lifetime           context.Context
	XrayBin            string
	GeoDir             string
	LogDir             string
	InternalSocketPath string
	InternalRESTToken  string
	DisableHashCheck   bool
	LowMemory          bool
	NodeVersion        string
	CoreVersion        string
	System             SystemSnapshotter
	TorrentBlocker     TorrentBlockerConfigProvider
}

type SystemSnapshotter interface {
	Snapshot() system.Snapshot
}

type TorrentBlockerConfigProvider interface {
	TorrentBlockerEnabled() bool
	TorrentBlockerIncludeRuleTags() []string
}

type Manager struct {
	// lifecycleMu serializes process ownership. State publication and
	// lifecycleMu acquisition/release are performed while mu is held.
	lifecycleMu      sync.Mutex
	logRotateMu      sync.Mutex
	mu               sync.RWMutex
	xrayBin          string
	geoDir           string
	logDir           string
	socketPath       string
	token            string
	socketPrefix     string
	disableHashCheck bool
	lowMemory        bool
	nodeVersion      string
	coreVersion      string
	system           SystemSnapshotter
	torrentBlocker   TorrentBlockerConfigProvider

	xrayVersion         *string
	state               lifecycleState
	operationEpoch      uint64
	nextProcessEpoch    uint64
	runtimeProcessEpoch uint64
	startCancel         context.CancelFunc
	stopOp              *stopOperation
	process             *processState

	// pendingConfigJSON is the only full config retained by the manager. It is
	// served while rw-core starts and released as soon as the gRPC API is ready.
	pendingConfigJSON []byte
	emptyConfigHash   string
	inboundHashes     map[string]*HashedSet
	inboundTags       map[string]struct{}

	readinessProbe      func(context.Context) bool
	readinessInterval   time.Duration
	startupTimeout      time.Duration
	interruptTimeout    time.Duration
	killTimeout         time.Duration
	processCommand      func() *exec.Cmd
	processGroupCleanup func(*os.Process, time.Duration) error
	processWaitDelay    time.Duration
	versionProbe        func(context.Context) (string, error)
	versionProbeBusy    bool
	nextVersionProbe    time.Time

	versionProbeContext      context.Context
	versionProbeCancel       context.CancelFunc
	versionProbeWG           sync.WaitGroup
	versionProbeShutdownOnce sync.Once
	versionProbeShutdownDone chan struct{}
	versionProbeShutdown     bool
}

const (
	versionProbeTimeout  = 5 * time.Second
	versionProbeRetry    = 30 * time.Second
	versionOutputMaxSize = 4 << 10
)

type StartRequest struct {
	Internals  StartInternals `json:"internals"`
	XrayConfig map[string]any `json:"xrayConfig"`
}

type StartInternals struct {
	ForceRestart bool       `json:"forceRestart"`
	Hashes       ConfigHash `json:"hashes"`
}

type ConfigHash struct {
	EmptyConfig string        `json:"emptyConfig"`
	Inbounds    []InboundHash `json:"inbounds"`
}

type InboundHash struct {
	UsersCount float64 `json:"usersCount"`
	Hash       string  `json:"hash"`
	Tag        string  `json:"tag"`
}

type StartResponse struct {
	IsStarted       bool            `json:"isStarted"`
	Version         *string         `json:"version"`
	Error           *string         `json:"error"`
	NodeInformation NodeInformation `json:"nodeInformation"`
	System          system.Snapshot `json:"system"`
}

type NodeInformation struct {
	Version *string `json:"version"`
}

type StopResponse struct {
	IsStopped bool `json:"isStopped"`
}

type HealthResponse struct {
	IsAlive                  bool    `json:"isAlive"`
	XrayInternalStatusCached bool    `json:"xrayInternalStatusCached"`
	XrayVersion              *string `json:"xrayVersion"`
	NodeVersion              string  `json:"nodeVersion"`
}

func NewManager(opts Options) (*Manager, error) {
	return newManager(opts, nil)
}

func newManager(opts Options, versionProbe func(context.Context) (string, error)) (*Manager, error) {
	if strings.TrimSpace(opts.NodeVersion) == "" {
		return nil, errors.New("xray: node version is required")
	}
	if opts.System == nil {
		return nil, errors.New("xray: system snapshotter is required")
	}
	coreVersion := coerceSemver(opts.CoreVersion)
	if strings.TrimSpace(opts.CoreVersion) != "" && coreVersion == "" {
		return nil, errors.New("xray: core version override is invalid")
	}
	socket, err := generateXtlsSocketName()
	if err != nil {
		return nil, fmt.Errorf("generate xtls api socket name: %w", err)
	}
	lifetime := opts.Lifetime
	if lifetime == nil {
		lifetime = context.Background()
	}
	versionProbeContext, versionProbeCancel := context.WithCancel(lifetime)
	manager := &Manager{
		xrayBin:                  opts.XrayBin,
		geoDir:                   opts.GeoDir,
		logDir:                   opts.LogDir,
		socketPath:               opts.InternalSocketPath,
		token:                    opts.InternalRESTToken,
		socketPrefix:             socket,
		disableHashCheck:         opts.DisableHashCheck,
		lowMemory:                opts.LowMemory,
		nodeVersion:              strings.TrimSpace(opts.NodeVersion),
		coreVersion:              coreVersion,
		system:                   opts.System,
		torrentBlocker:           opts.TorrentBlocker,
		readinessInterval:        defaultReadinessInterval,
		interruptTimeout:         defaultInterruptTimeout,
		killTimeout:              defaultKillTimeout,
		processWaitDelay:         defaultProcessWaitDelay,
		processGroupCleanup:      cleanupOwnedProcessGroup,
		versionProbe:             versionProbe,
		versionProbeContext:      versionProbeContext,
		versionProbeCancel:       versionProbeCancel,
		versionProbeShutdownDone: make(chan struct{}),
	}
	manager.refreshVersion(versionProbeContext)
	return manager, nil
}

// generateXtlsSocketName returns a node-process-unique prefix for Xray gRPC
// sockets. Each rw-core process appends its own epoch so a lazy client for an
// old core can never connect to its replacement.
func generateXtlsSocketName() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "remnanode-xtls-" + hex.EncodeToString(buf), nil
}

func (m *Manager) torrentBlockerOptions() TorrentBlockerOptions {
	m.mu.RLock()
	socketPath := m.socketPath
	token := m.token
	provider := m.torrentBlocker
	m.mu.RUnlock()

	opts := TorrentBlockerOptions{
		SocketPath: socketPath,
		RESTToken:  token,
	}
	if provider != nil {
		opts.Enabled = provider.TorrentBlockerEnabled()
		opts.IncludeRuleTags = provider.TorrentBlockerIncludeRuleTags()
	}
	return opts
}

func (m *Manager) Health() HealthResponse {
	m.mu.Lock()
	running := m.state == lifecycleRunning
	version := m.xrayVersion
	retryVersion := !m.versionProbeShutdown && m.versionProbeContext.Err() == nil && version == nil && m.state != lifecycleStarting && !m.versionProbeBusy &&
		!time.Now().Before(m.nextVersionProbe)
	var probeContext context.Context
	if retryVersion {
		m.versionProbeBusy = true
		m.nextVersionProbe = time.Now().Add(versionProbeRetry)
		m.versionProbeWG.Add(1)
		probeContext = m.versionProbeContext
	}
	m.mu.Unlock()
	if retryVersion {
		go func() {
			defer m.versionProbeWG.Done()
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

// CurrentConfigJSON returns the config exactly as served to a starting
// rw-core. Once readiness is confirmed the process has consumed the config,
// so the cache is released and this method returns an empty object.
// Callers must treat the returned slice as read-only.
func (m *Manager) CurrentConfigJSON() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.pendingConfigJSON) == 0 {
		return emptyConfigJSON
	}
	return m.pendingConfigJSON
}

func (m *Manager) clearRuntimeLocked() {
	m.pendingConfigJSON = nil
	m.runtimeProcessEpoch = 0
	m.clearHashStateLocked()
	m.clearInboundTagsLocked()
}

func (m *Manager) XrayBin() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.xrayBin
}

func (m *Manager) CommandArgs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return BuildCommandArgs(m.socketPath)
}

func BuildCommandArgs(socketPath string) []string {
	return []string{
		"-config",
		BuildConfigURL(socketPath),
		"-format",
		"json",
	}
}

func BuildConfigURL(socketPath string) string {
	return fmt.Sprintf("http+unix://%s/internal/get-config", socketPath)
}

func (m *Manager) refreshVersion(parent context.Context) {
	version := m.probeVersion(parent)
	m.mu.Lock()
	m.publishVersionLocked(version)
	m.mu.Unlock()
}

func (m *Manager) probeVersion(parent context.Context) *string {
	m.mu.RLock()
	override := m.coreVersion
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
	probe := m.versionProbe
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
	if err != nil {
		return nil
	}
	if version == "" {
		return nil
	}
	return &version
}

func (m *Manager) publishVersionLocked(version *string) {
	m.xrayVersion = version
	if version == nil {
		m.nextVersionProbe = time.Now().Add(versionProbeRetry)
	} else {
		m.nextVersionProbe = time.Time{}
	}
}

func (m *Manager) refreshUnknownVersion(parent context.Context) {
	version := m.probeVersion(parent)
	m.mu.Lock()
	if !m.versionProbeShutdown && m.xrayVersion == nil && version != nil {
		m.publishVersionLocked(version)
	}
	m.versionProbeBusy = false
	m.mu.Unlock()
}

// Shutdown permanently stops background version recovery. It is reserved for
// node process shutdown; Stop remains reusable for the public xray/stop route.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.versionProbeShutdownOnce.Do(func() {
		m.mu.Lock()
		m.versionProbeShutdown = true
		cancel := m.versionProbeCancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		go func() {
			m.versionProbeWG.Wait()
			close(m.versionProbeShutdownDone)
		}()
	})

	select {
	case <-m.versionProbeShutdownDone:
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
		if v := extractSemver(line); v != "" {
			return v
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

func (m *Manager) startResponse(isStarted bool, message *string) StartResponse {
	m.mu.RLock()
	version := m.xrayVersion
	m.mu.RUnlock()

	return StartResponse{
		IsStarted: isStarted,
		Version:   version,
		Error:     message,
		NodeInformation: NodeInformation{
			Version: stringPtr(m.nodeVersion),
		},
		System: m.system.Snapshot(),
	}
}

func stringPtr(value string) *string {
	return &value
}
