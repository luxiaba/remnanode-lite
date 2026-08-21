package xray

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/system"
)

type Options struct {
	// Lifetime bounds initial and background version probes. A nil value uses context.Background.
	Lifetime           context.Context
	XrayBin            string
	GeoDir             string
	LogDir             string
	PanelRuntimeDir    string
	InternalSocketPath string
	InternalRESTToken  string
	DisableHashCheck   bool
	LowMemory          bool
	NodeVersion        string
	CoreVersion        string
	System             SystemSnapshotter
	TorrentBlocker     TorrentBlockerConfigProvider
	PreStart           PreStartConfigProvider
}

type SystemSnapshotter interface {
	Snapshot() system.Snapshot
}

type TorrentBlockerConfigProvider interface {
	TorrentBlockerEnabled() bool
	TorrentBlockerIncludeRuleTags() []string
	TorrentBlockerRulePosition() float64
}

type PreStartConfigProvider interface {
	PreStartCleanupSockets() (bool, []string)
}

// runtimeState is guarded by Manager.mu. It intentionally has no lock of its
// own so lifecycle state and process-bound runtime data remain one atomic
// publication boundary.
type runtimeState struct {
	// pendingConfigJSON is served while rw-core starts and released as soon as
	// the gRPC API is ready. It is the only full config retained by the manager.
	pendingConfigJSON   []byte
	runtimeProcessEpoch uint64
	emptyConfigHash     string
	inboundHashes       map[string]*HashedSet
	inboundTags         map[string]struct{}
}

// versionState shares Manager.mu with lifecycle state. That lock protects the
// cached version and recovery scheduling fields, and makes Health's Add happen
// before Shutdown closes the scheduling gate. WaitGroup and Once retain their
// own synchronization semantics outside that critical section.
type versionState struct {
	coreOverride string
	cached       *string
	probe        func(context.Context) (string, error)
	busy         bool
	nextProbe    time.Time

	context      context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdown     bool
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
	panelRuntimeDir  string
	socketPath       string
	token            string
	socketPrefix     string
	disableHashCheck bool
	lowMemory        bool
	nodeVersion      string
	system           SystemSnapshotter
	torrentBlocker   TorrentBlockerConfigProvider
	preStart         PreStartConfigProvider

	state            lifecycleState
	operationEpoch   uint64
	nextProcessEpoch uint64
	startCancel      context.CancelFunc
	stopOp           *stopOperation
	process          *processState

	runtime runtimeState
	version versionState

	readinessProbe      func(context.Context) bool
	readinessInterval   time.Duration
	startupTimeout      time.Duration
	interruptTimeout    time.Duration
	killTimeout         time.Duration
	processCommand      func() *exec.Cmd
	processGroupCleanup func(*os.Process, time.Duration) error
	processWaitDelay    time.Duration
	downloadPanelFile   func(context.Context, string, string, string, os.FileMode) (downloadResult, error)
}

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
	socket, err := generateXrayRPCSocketName()
	if err != nil {
		return nil, fmt.Errorf("generate Xray RPC socket name: %w", err)
	}
	lifetime := opts.Lifetime
	if lifetime == nil {
		lifetime = context.Background()
	}
	versionProbeContext, versionProbeCancel := context.WithCancel(lifetime)
	panelRuntimeDir := strings.TrimSpace(opts.PanelRuntimeDir)
	if panelRuntimeDir == "" {
		panelRuntimeDir = "/var/lib/remnanode-lite/panel-runtime"
	}
	manager := &Manager{
		xrayBin:             opts.XrayBin,
		geoDir:              opts.GeoDir,
		logDir:              opts.LogDir,
		panelRuntimeDir:     panelRuntimeDir,
		socketPath:          opts.InternalSocketPath,
		token:               opts.InternalRESTToken,
		socketPrefix:        socket,
		disableHashCheck:    opts.DisableHashCheck,
		lowMemory:           opts.LowMemory,
		nodeVersion:         strings.TrimSpace(opts.NodeVersion),
		system:              opts.System,
		torrentBlocker:      opts.TorrentBlocker,
		preStart:            opts.PreStart,
		readinessInterval:   defaultReadinessInterval,
		interruptTimeout:    defaultInterruptTimeout,
		killTimeout:         defaultKillTimeout,
		processWaitDelay:    defaultProcessWaitDelay,
		processGroupCleanup: cleanupOwnedProcessGroup,
		downloadPanelFile:   downloadPanelFile,
		version: versionState{
			coreOverride: coreVersion,
			probe:        versionProbe,
			context:      versionProbeContext,
			cancel:       versionProbeCancel,
			shutdownDone: make(chan struct{}),
		},
	}
	manager.refreshVersion(versionProbeContext)
	return manager, nil
}

// generateXrayRPCSocketName returns a node-process-unique prefix for Xray gRPC
// sockets. Each rw-core process appends its own epoch so a lazy client for an
// old core can never connect to its replacement.
func generateXrayRPCSocketName() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "remnanode-lite-xtls-" + hex.EncodeToString(buf), nil
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
		opts.RulePosition = provider.TorrentBlockerRulePosition()
	}
	return opts
}

// CurrentConfigJSON returns the config exactly as served to a starting
// rw-core. Once readiness is confirmed the process has consumed the config,
// so the cache is released and this method returns an empty object.
// Callers must treat the returned slice as read-only.
func (m *Manager) CurrentConfigJSON() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.runtime.pendingConfigJSON) == 0 {
		return emptyConfigJSON
	}
	return m.runtime.pendingConfigJSON
}

func (m *Manager) clearRuntimeLocked() {
	m.runtime.pendingConfigJSON = nil
	m.runtime.runtimeProcessEpoch = 0
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

func (m *Manager) startResponse(isStarted bool, message *string) StartResponse {
	m.mu.RLock()
	version := m.version.cached
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
