package rnlctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxStateBytes = 1 << 20

var generationIDRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rnl\.[1-9][0-9]*)?-[0-9a-f]{16}$`)

type generationRecord struct {
	ID              string `json:"id"`
	Version         string `json:"version"`
	ContractVersion string `json:"contractVersion"`
	Architecture    string `json:"architecture"`
	SourceRevision  string `json:"sourceRevision"`
	Identity        string `json:"identity"`
	CacheFile       string `json:"cacheFile"`
	ArchiveSHA256   string `json:"archiveSHA256"`
	CacheKind       string `json:"cacheKind"`
	InstalledAt     string `json:"installedAt"`
}

type desiredServiceState struct {
	Enabled bool `json:"enabled"`
	Active  bool `json:"active"`
}

type persistentState struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Current       string                      `json:"current"`
	Previous      string                      `json:"previous,omitempty"`
	CorePolicy    string                      `json:"corePolicy"`
	Prepared      bool                        `json:"prepared"`
	Desired       desiredServiceState         `json:"desiredService"`
	Account       ManagedAccount              `json:"account"`
	Generations   map[string]generationRecord `json:"generations"`
}

type transactionJournal struct {
	SchemaVersion      int                 `json:"schemaVersion"`
	Operation          string              `json:"operation"`
	Phase              string              `json:"phase"`
	From               string              `json:"from,omitempty"`
	Previous           string              `json:"previous,omitempty"`
	Target             generationRecord    `json:"target"`
	Desired            desiredServiceState `json:"desiredService"`
	Prepared           bool                `json:"prepared"`
	Purge              bool                `json:"purge,omitempty"`
	RestartRequired    bool                `json:"restartRequired,omitempty"`
	Account            ManagedAccount      `json:"account,omitempty"`
	TransactionAccount *ManagedAccount     `json:"transactionAccount,omitempty"`
	StartedAt          string              `json:"startedAt"`
}

type retainedInstallation struct {
	SchemaVersion int            `json:"schemaVersion"`
	Account       ManagedAccount `json:"account"`
}

// PendingOperation is the non-sensitive portion of a durable journal exposed
// through status --json.
type PendingOperation struct {
	Operation string `json:"operation"`
	Phase     string `json:"phase"`
	From      string `json:"from,omitempty"`
	Target    string `json:"target,omitempty"`
}

func generationFromBundle(bundle *validatedBundle, cache cacheInfo) generationRecord {
	return generationRecord{
		ID: bundle.GenerationID, Version: bundle.Manifest.Version,
		ContractVersion: bundle.Manifest.ContractVersion,
		Architecture:    bundle.Manifest.Architecture,
		SourceRevision:  bundle.Manifest.SourceRevision,
		Identity:        bundle.Identity, CacheFile: cache.Path,
		ArchiveSHA256: cache.SHA256, CacheKind: cache.Kind,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func loadState(paths Paths) (*persistentState, error) {
	raw, err := readRegularFile(paths.StateFile, maxStateBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read lifecycle state: %w", err)
	}
	var state persistentState
	if err := decodeStrictJSON(raw, &state); err != nil {
		return nil, fmt.Errorf("decode lifecycle state: %w", err)
	}
	if err := validateState(state, paths); err != nil {
		return nil, fmt.Errorf("invalid lifecycle state: %w", err)
	}
	return &state, nil
}

func saveState(paths Paths, state persistentState) error {
	if err := validateState(state, paths); err != nil {
		return fmt.Errorf("refuse invalid lifecycle state: %w", err)
	}
	return atomicWriteJSON(paths.StateFile, state)
}

func validateState(state persistentState, paths Paths) error {
	if state.SchemaVersion != stateSchemaVersion {
		return fmt.Errorf("schemaVersion=%d", state.SchemaVersion)
	}
	if state.CorePolicy != managedCorePolicy {
		return fmt.Errorf("unsupported core policy %q", state.CorePolicy)
	}
	if state.Prepared && (state.Desired.Enabled || state.Desired.Active) {
		return fmt.Errorf("prepared deployment must be stopped and disabled")
	}
	if state.Account.UID == "" || state.Account.GID == "" || state.Account.Home == "" || state.Account.Shell == "" {
		return fmt.Errorf("managed account identity is incomplete")
	}
	if len(state.Generations) == 0 || len(state.Generations) > 2 {
		return fmt.Errorf("generation count must be one or two")
	}
	if _, exists := state.Generations[state.Current]; !exists {
		return fmt.Errorf("current generation %q is not recorded", state.Current)
	}
	if state.Previous != "" {
		if state.Previous == state.Current {
			return fmt.Errorf("current and previous generations are identical")
		}
		if _, exists := state.Generations[state.Previous]; !exists {
			return fmt.Errorf("previous generation %q is not recorded", state.Previous)
		}
	}
	for id, generation := range state.Generations {
		if id != generation.ID {
			return fmt.Errorf("generation map key %q does not match record", id)
		}
		if err := validateGenerationRecord(generation, paths); err != nil {
			return fmt.Errorf("generation %q: %w", id, err)
		}
	}
	return nil
}

func loadJournal(paths Paths) (*transactionJournal, error) {
	raw, err := readRegularFile(paths.JournalFile, maxStateBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read lifecycle journal: %w", err)
	}
	var journal transactionJournal
	if err := decodeStrictJSON(raw, &journal); err != nil {
		return nil, fmt.Errorf("decode lifecycle journal: %w", err)
	}
	if err := validateJournal(journal, paths); err != nil {
		return nil, fmt.Errorf("invalid lifecycle journal: %w", err)
	}
	return &journal, nil
}

func saveJournal(paths Paths, journal transactionJournal) error {
	if journal.SchemaVersion == 0 {
		journal.SchemaVersion = journalSchemaVersion
	}
	if journal.StartedAt == "" {
		journal.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := validateJournal(journal, paths); err != nil {
		return err
	}
	return atomicWriteJSON(paths.JournalFile, journal)
}

func validateJournal(journal transactionJournal, paths Paths) error {
	if journal.SchemaVersion != journalSchemaVersion {
		return fmt.Errorf("unsupported journal schema")
	}
	switch journal.Operation {
	case "install", "activate", "upgrade", "rollback", "repair", "uninstall", "start", "stop", "restart":
	default:
		return fmt.Errorf("unsupported operation %q", journal.Operation)
	}
	allowedPhases := map[string]map[string]bool{
		"install":   {"planned": true, "payload-ready": true, "service-prepared": true, "state-committed": true},
		"activate":  {"planned": true},
		"upgrade":   {"planned": true, "payload-ready": true, "service-restored": true, "state-committed": true},
		"rollback":  {"planned": true},
		"repair":    {"planned": true},
		"uninstall": {"planned": true},
		"start":     {"planned": true},
		"stop":      {"planned": true},
		"restart":   {"planned": true},
	}
	if !allowedPhases[journal.Operation][journal.Phase] {
		return fmt.Errorf("unsupported %s journal phase %q", journal.Operation, journal.Phase)
	}
	if journal.Purge && journal.Operation != "uninstall" {
		return fmt.Errorf("purge intent is valid only for uninstall")
	}
	if journal.RestartRequired && journal.Operation != "activate" {
		return fmt.Errorf("restart intent is valid only for activate")
	}
	if err := validateGenerationRecord(journal.Target, paths); err != nil {
		return fmt.Errorf("journal target: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, journal.StartedAt); err != nil {
		return fmt.Errorf("journal timestamp is invalid")
	}
	return nil
}

func validateGenerationRecord(generation generationRecord, paths Paths) error {
	if !generationIDRE.MatchString(generation.ID) ||
		!projectVersionRE.MatchString(generation.Version) ||
		!contractVersionRE.MatchString(generation.ContractVersion) ||
		!gitRevisionRE.MatchString(generation.SourceRevision) ||
		!hexDigestRE.MatchString(generation.Identity) {
		return fmt.Errorf("invalid identity metadata")
	}
	if !strings.Contains(generation.Version, "-rnl.") && generation.Version != generation.ContractVersion {
		return fmt.Errorf("stable generation version must equal contract version")
	}
	if generation.Architecture != "amd64" && generation.Architecture != "arm64" {
		return fmt.Errorf("invalid architecture")
	}
	if generation.CacheFile == "" || !pathWithin(paths.BundleCache, generation.CacheFile) || filepath.Dir(generation.CacheFile) != paths.BundleCache {
		return fmt.Errorf("unsafe cache path")
	}
	if !hexDigestRE.MatchString(generation.ArchiveSHA256) || (generation.CacheKind != "verified-archive" && generation.CacheKind != "root-snapshot") {
		return fmt.Errorf("invalid cache integrity metadata")
	}
	if _, err := time.Parse(time.RFC3339, generation.InstalledAt); err != nil {
		return fmt.Errorf("invalid installation time")
	}
	return nil
}

func clearJournal(paths Paths) error {
	err := removeAndSync(paths.JournalFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func pendingFromJournal(journal *transactionJournal) *PendingOperation {
	if journal == nil {
		return nil
	}
	return &PendingOperation{Operation: journal.Operation, Phase: journal.Phase, From: journal.From, Target: journal.Target.ID}
}

// transactionAccount returns only the account ownership created by the
// interrupted transaction. Older journals predate TransactionAccount and
// stored the persisted account directly, so retain that representation as a
// compatibility fallback.
func transactionAccount(journal transactionJournal) ManagedAccount {
	if journal.TransactionAccount != nil {
		return *journal.TransactionAccount
	}
	return journal.Account
}

func loadRetained(paths Paths) (*retainedInstallation, error) {
	raw, err := readRegularFile(paths.RetainedFile, maxStateBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var retained retainedInstallation
	if err := decodeStrictJSON(raw, &retained); err != nil {
		return nil, err
	}
	if retained.SchemaVersion != stateSchemaVersion || retained.Account.UID == "" || retained.Account.GID == "" || retained.Account.Home == "" || retained.Account.Shell == "" {
		return nil, fmt.Errorf("invalid retained installation metadata")
	}
	return &retained, nil
}

func saveRetained(paths Paths, account ManagedAccount) error {
	if account.UID == "" || account.GID == "" || account.Home == "" || account.Shell == "" {
		return fmt.Errorf("cannot retain incomplete account identity")
	}
	return atomicWriteJSON(paths.RetainedFile, retainedInstallation{SchemaVersion: stateSchemaVersion, Account: account})
}
