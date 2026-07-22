package rnlctl

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

const (
	stateSchemaVersion   = 1
	journalSchemaVersion = 1
	manifestSchema       = 2
	managedCorePolicy    = "bundled"
)

// Paths contains every host path owned by the Native lifecycle manager. Tests
// use PathsAt to exercise transactions without touching the host installation.
type Paths struct {
	LibraryRoot      string
	Generations      string
	CurrentLink      string
	PreviousLink     string
	NodeBinaryLink   string
	ControlBinary    string
	ConfigDirectory  string
	EnvironmentFile  string
	SecretFile       string
	InstallerState   string
	StateFile        string
	JournalFile      string
	RetainedFile     string
	LockFile         string
	BundleCache      string
	ApplicationState string
	LogDirectory     string
	RuntimeDirectory string
	SystemdUnit      string
	SystemdDropIn    string
	OpenRCUnit       string
}

// ProductionPaths returns the stable Native Linux filesystem contract.
func ProductionPaths() Paths { return PathsAt("") }

// PathsAt prefixes the production layout with root. It is intended for tests
// and image construction; root must itself be a trusted directory.
func PathsAt(root string) Paths {
	join := func(path string) string {
		if root == "" {
			return path
		}
		return filepath.Join(root, filepath.FromSlash(path[1:]))
	}
	library := join("/usr/local/lib/remnanode-lite")
	installerState := join("/var/lib/remnanode-lite-installer")
	return Paths{
		LibraryRoot:      library,
		Generations:      filepath.Join(library, "generations"),
		CurrentLink:      filepath.Join(library, "current"),
		PreviousLink:     filepath.Join(library, "previous"),
		NodeBinaryLink:   join("/usr/local/bin/remnanode-lite"),
		ControlBinary:    join("/usr/local/sbin/rnlctl"),
		ConfigDirectory:  join("/etc/remnanode-lite"),
		EnvironmentFile:  join("/etc/remnanode-lite/node.env"),
		SecretFile:       join("/etc/remnanode-lite/secret.key"),
		InstallerState:   installerState,
		StateFile:        filepath.Join(installerState, "state.json"),
		JournalFile:      filepath.Join(installerState, "journal.json"),
		RetainedFile:     filepath.Join(installerState, "retained.json"),
		LockFile:         join("/run/remnanode-lite-installer/operation.lock"),
		BundleCache:      filepath.Join(installerState, "bundles"),
		ApplicationState: join("/var/lib/remnanode-lite"),
		LogDirectory:     join("/var/log/remnanode-lite"),
		RuntimeDirectory: join("/run/remnanode-lite"),
		SystemdUnit:      join("/usr/local/lib/systemd/system/remnanode-lite.service"),
		SystemdDropIn:    join("/etc/systemd/system/remnanode-lite.service.d/20-remnanode-lite-hardening.conf"),
		OpenRCUnit:       join("/etc/init.d/remnanode-lite"),
	}
}

// BundleInput identifies exactly one locally available release bundle.
type BundleInput struct {
	Root            string
	Archive         string
	SHA256          string
	ExpectedVersion string
}

type InstallRequest struct {
	Bundle      BundleInput
	Port        int
	SecretFile  string
	PrepareOnly bool
}

type ActivateRequest struct {
	SecretFile string
}

type UpgradeRequest struct {
	Bundle BundleInput
	To     string
}

type RollbackRequest struct {
	GenerationID string
}

type RepairRequest struct {
	Bundle BundleInput
}

type UninstallRequest struct {
	Purge bool
	Yes   bool
}

// Result describes a successful lifecycle mutation without exposing secret
// material or machine-specific transient paths.
type Result struct {
	Operation    string `json:"operation"`
	Changed      bool   `json:"changed"`
	Generation   string `json:"generation,omitempty"`
	Version      string `json:"version,omitempty"`
	PreparedOnly bool   `json:"preparedOnly,omitempty"`
}

// Status is the stable machine-readable lifecycle status contract.
type Status struct {
	SchemaVersion    int               `json:"schemaVersion"`
	Deployment       string            `json:"deployment"`
	Installed        bool              `json:"installed"`
	Prepared         bool              `json:"prepared"`
	Healthy          bool              `json:"healthy"`
	Version          string            `json:"version,omitempty"`
	Generation       string            `json:"generation,omitempty"`
	Previous         string            `json:"previous,omitempty"`
	CorePolicy       string            `json:"corePolicy,omitempty"`
	Service          ServiceStatus     `json:"service"`
	Pending          *PendingOperation `json:"pending,omitempty"`
	RepairCapability string            `json:"repairCapability,omitempty"`
	Problems         []string          `json:"problems,omitempty"`
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type DoctorReport struct {
	SchemaVersion int     `json:"schemaVersion"`
	Healthy       bool    `json:"healthy"`
	Checks        []Check `json:"checks"`
}

// Lifecycle is the command-facing contract. It deliberately contains no
// process or filesystem details, which keeps CLI parsing independently tested.
type Lifecycle interface {
	Install(context.Context, InstallRequest) (Result, error)
	Activate(context.Context, ActivateRequest) (Result, error)
	Upgrade(context.Context, UpgradeRequest) (Result, error)
	Rollback(context.Context, RollbackRequest) (Result, error)
	Repair(context.Context, RepairRequest) (Result, error)
	Uninstall(context.Context, UninstallRequest) (Result, error)
	Status(context.Context) (Status, error)
	Doctor(context.Context) (DoctorReport, error)
	Start(context.Context) (Result, error)
	Stop(context.Context) (Result, error)
	Restart(context.Context) (Result, error)
}

type ServiceStatus struct {
	Manager string `json:"manager,omitempty"`
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"`
}

// HostController owns account creation, service-definition installation, and
// service-manager mutations. Implementations must make every method idempotent.
type HostController interface {
	Preflight(context.Context, bool, Paths) error
	Prepare(context.Context, string, Paths) (ManagedAccount, error)
	RemoveService(context.Context, Paths) error
	PreflightRemoveAccount(context.Context, ManagedAccount) error
	RemoveAccount(context.Context, ManagedAccount) error
	ServiceStatus(context.Context) (ServiceStatus, error)
	SetEnabled(context.Context, bool) error
	SetActive(context.Context, bool) error
	Restart(context.Context) error
	ApplyOwnership(context.Context, Paths) error
	ValidateBinary(context.Context, string, string, string) error
	// WaitHealthy executes the bundled healthcheck against the managed Unix
	// socket. The socket path is explicit so inherited environment variables
	// cannot redirect a lifecycle operation to an unrelated process.
	WaitHealthy(context.Context, string, string, time.Duration) error
}

type ManagedAccount struct {
	UserCreated  bool   `json:"userCreated"`
	GroupCreated bool   `json:"groupCreated"`
	UID          string `json:"uid"`
	GID          string `json:"gid"`
	Home         string `json:"home"`
	Shell        string `json:"shell"`
}

// BundleResolver retrieves an exact, immutable release into destinationDir.
// It must verify repository checksums before returning the archive path.
type BundleResolver interface {
	Resolve(context.Context, string, string, string) (string, error)
}

// FailureInjector is used only to exercise recovery at durable transaction
// boundaries. Production options leave it nil.
type FailureInjector interface {
	Fail(string) error
}

// EngineOptions supplies lifecycle dependencies.
type EngineOptions struct {
	Paths        Paths
	Host         HostController
	Resolver     BundleResolver
	Architecture string
	RequireRoot  func() bool
	Failure      FailureInjector
}

var (
	ErrNotInstalled        = errors.New("remnanode-lite is not installed")
	ErrAlreadyInstalled    = errors.New("a different remnanode-lite generation is already installed; use upgrade")
	ErrConcurrentOperation = errors.New("another rnlctl lifecycle operation is running")
)
