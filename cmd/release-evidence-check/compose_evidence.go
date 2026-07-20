package main

import (
	"errors"
	"fmt"
	"strings"
)

const (
	expectedComposeSourcePath              = "deploy/compose.single-file.yaml"
	expectedContainerMemoryBytes           = int64(expectedServiceMemoryMaxMiB * 1024 * 1024)
	expectedContainerNanoCPUs              = int64(expectedCPUCount * 1_000_000_000)
	expectedContainerPIDsLimit             = int64(256)
	expectedContainerLogSizeBytes          = int64(2 * 1024 * 1024)
	expectedContainerLogFiles              = 2
	maximumContainerLogPeakBytes           = expectedContainerLogSizeBytes * int64(expectedContainerLogFiles+1)
	minimumComposeHostMemoryBytes          = int64(480 * 1024 * 1024)
	minimumComposeHostDiskBytes            = int64(1792 * 1024 * 1024)
	minimumComposeDiskAvailableAtPeakBytes = int64(256 * 1024 * 1024)
	maximumComposeProjectDiskPeakMiB       = 1024
	expectedOfficialRollbackRepository     = "docker.io/remnawave/node"
	expectedProjectRollbackRepository      = "ghcr.io/luxiaba/remnanode-lite"
)

var expectedContainerCapabilities = []string{"NET_ADMIN", "NET_BIND_SERVICE"}

var expectedContainerTmpfs = map[string]int64{
	"/run/remnanode":     4 * 1024 * 1024,
	"/tmp":               16 * 1024 * 1024,
	"/var/log/remnanode": 28 * 1024 * 1024,
}

type composeEvidence struct {
	evidenceCommon
	CandidateImageDigest string        `json:"candidateImageDigest"`
	Source               composeSource `json:"source"`
	ManifestPlatforms    []string      `json:"manifestPlatforms"`
	Runs                 []composeRun  `json:"runs"`
}

type composeSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type composeEnvironment struct {
	DockerEngineVersion  string `json:"dockerEngineVersion"`
	DockerComposeVersion string `json:"dockerComposeVersion"`
	Arch                 string `json:"arch"`
}

type composeRun struct {
	Environment   composeEnvironment   `json:"environment"`
	HostResources composeHostResources `json:"hostResources"`
	Limits        composeLimits        `json:"limits"`
	Isolation     composeIsolation     `json:"isolation"`
	Health        composeHealth        `json:"health"`
	Lifecycle     composeLifecycle     `json:"lifecycle"`
	Logs          composeLogs          `json:"logs"`
	Storage       composeStorage       `json:"storage"`
}

type composeHostResources struct {
	MemoryTotalBytes         *int64 `json:"memoryTotalBytes"`
	CPUCount                 *int   `json:"cpuCount"`
	DiskTotalBytes           *int64 `json:"diskTotalBytes"`
	DiskAvailableAtPeakBytes *int64 `json:"diskAvailableAtPeakBytes"`
	SwapTotalBytes           *int64 `json:"swapTotalBytes"`
}

type composeLimits struct {
	MemoryLimitBytes     int64 `json:"memoryLimitBytes"`
	MemorySwapLimitBytes int64 `json:"memorySwapLimitBytes"`
	NanoCPUs             int64 `json:"nanoCPUs"`
	PIDsLimit            int64 `json:"pidsLimit"`
}

type composeIsolation struct {
	ReadOnlyRootFS        bool                `json:"readOnlyRootfs"`
	NoNewPrivileges       bool                `json:"noNewPrivileges"`
	InitEnabled           bool                `json:"initEnabled"`
	InitPID               int                 `json:"initPid"`
	InitProcess           string              `json:"initProcess"`
	OrphanReapingPassed   bool                `json:"orphanReapingPassed"`
	CapDrop               []string            `json:"capDrop"`
	CapAdd                []string            `json:"capAdd"`
	EffectiveCapabilities []string            `json:"effectiveCapabilities"`
	Tmpfs                 []composeTmpfsMount `json:"tmpfs"`
}

type composeTmpfsMount struct {
	Target    string `json:"target"`
	SizeBytes int64  `json:"sizeBytes"`
	Writable  bool   `json:"writable"`
	NoExec    bool   `json:"noexec"`
	NoSUID    bool   `json:"nosuid"`
	NoDev     bool   `json:"nodev"`
}

type composeHealth struct {
	Status               string `json:"status"`
	CheckExitCode        *int   `json:"checkExitCode"`
	ConsecutiveSuccesses int    `json:"consecutiveSuccesses"`
}

type composeLifecycle struct {
	GracefulStop         bool  `json:"gracefulStop"`
	ForcedKill           *bool `json:"forcedKill"`
	ExitCode             *int  `json:"exitCode"`
	PIDsBaseline         int   `json:"pidsBaseline"`
	PIDsPeak             int   `json:"pidsPeak"`
	PIDsAfterRecovery    int   `json:"pidsAfterRecovery"`
	PIDsAfterStop        *int  `json:"pidsAfterStop"`
	ZombiesAfterRecovery *int  `json:"zombiesAfterRecovery"`
}

type composeLogs struct {
	Driver           string `json:"driver"`
	MaxSizeBytes     int64  `json:"maxSizeBytes"`
	MaxFiles         int    `json:"maxFiles"`
	RotationObserved bool   `json:"rotationObserved"`
	PeakBytes        int64  `json:"peakBytes"`
}

type composeStorage struct {
	RollbackImageRepository    string `json:"rollbackImageRepository"`
	RollbackImageDigest        string `json:"rollbackImageDigest"`
	RollbackImagePulled        bool   `json:"rollbackImagePulled"`
	RollbackImageStarted       bool   `json:"rollbackImageStarted"`
	RollbackImageHealthy       bool   `json:"rollbackImageHealthy"`
	RollbackImagePresentAtPeak bool   `json:"rollbackImagePresentAtPeak"`
	ProjectDiskPeakMiB         int    `json:"projectDiskPeakMiB"`
}

func validateComposeEvidence(
	evidence composeEvidence,
	policy acceptancePolicy,
	candidateImageDigest, candidateComposeSHA string,
) error {
	if evidence.CandidateImageDigest != candidateImageDigest {
		return fmt.Errorf(
			"candidateImageDigest=%q, want manifest candidateImageDigest %s",
			evidence.CandidateImageDigest,
			candidateImageDigest,
		)
	}
	if evidence.Source.Path != expectedComposeSourcePath {
		return fmt.Errorf("source path=%q, want %q", evidence.Source.Path, expectedComposeSourcePath)
	}
	if evidence.Source.SHA256 != candidateComposeSHA {
		return fmt.Errorf("source SHA-256=%q, want candidate Git object SHA-256 %s", evidence.Source.SHA256, candidateComposeSHA)
	}
	if !exactStringSliceSet(evidence.ManifestPlatforms, []string{"linux/amd64", "linux/arm64"}) {
		return fmt.Errorf("candidate image platforms must be linux/amd64 and linux/arm64, got %v", evidence.ManifestPlatforms)
	}
	if len(evidence.Runs) != len(expectedAssetSHAByArch) {
		return fmt.Errorf("Compose runs count=%d, want one amd64 run and one arm64 run", len(evidence.Runs))
	}

	seenArchitectures := make(map[string]struct{}, len(evidence.Runs))
	rollbackRepository := ""
	rollbackDigest := ""
	for index, run := range evidence.Runs {
		arch := run.Environment.Arch
		if _, ok := expectedAssetSHAByArch[arch]; !ok {
			return fmt.Errorf("Compose run %d has unsupported architecture %q", index, arch)
		}
		if _, duplicate := seenArchitectures[arch]; duplicate {
			return fmt.Errorf("Compose architecture %q is duplicated; runs must cover amd64 and arm64 exactly once", arch)
		}
		seenArchitectures[arch] = struct{}{}
		if err := validateComposeRun(run, policy, candidateImageDigest); err != nil {
			return fmt.Errorf("Compose run %q: %w", arch, err)
		}
		if index == 0 {
			rollbackRepository = run.Storage.RollbackImageRepository
			rollbackDigest = run.Storage.RollbackImageDigest
			continue
		}
		if run.Storage.RollbackImageRepository != rollbackRepository ||
			run.Storage.RollbackImageDigest != rollbackDigest {
			return errors.New("Compose runs must use the same rollback image repository and manifest digest")
		}
	}
	return nil
}

func validateComposeRun(run composeRun, policy acceptancePolicy, candidateImageDigest string) error {
	if strings.TrimSpace(run.Environment.DockerEngineVersion) == "" ||
		strings.TrimSpace(run.Environment.DockerComposeVersion) == "" {
		return errors.New("Docker Engine and Docker Compose versions must not be empty")
	}
	if err := validateComposeHostResources(run.HostResources, policy); err != nil {
		return err
	}
	if err := validateComposeLimits(run.Limits); err != nil {
		return err
	}
	if err := validateComposeIsolation(run.Isolation); err != nil {
		return err
	}
	if run.Health.CheckExitCode == nil {
		return errors.New("Compose health checkExitCode is required")
	}
	if run.Health.Status != "healthy" || *run.Health.CheckExitCode != 0 || run.Health.ConsecutiveSuccesses < 1 {
		return errors.New("Compose health must be healthy with exit code 0 and at least one successful check")
	}
	if err := validateComposeLifecycle(run.Lifecycle); err != nil {
		return err
	}
	if err := validateComposeLogs(run.Logs); err != nil {
		return err
	}
	if err := validateComposeStorage(run.Storage, run.HostResources, candidateImageDigest, policy); err != nil {
		return err
	}
	return nil
}

func validateComposeHostResources(resources composeHostResources, policy acceptancePolicy) error {
	if resources.MemoryTotalBytes == nil {
		return errors.New("Compose hostResources memoryTotalBytes is required")
	}
	if resources.CPUCount == nil {
		return errors.New("Compose hostResources cpuCount is required")
	}
	if resources.DiskTotalBytes == nil {
		return errors.New("Compose hostResources diskTotalBytes is required")
	}
	if resources.DiskAvailableAtPeakBytes == nil {
		return errors.New("Compose hostResources diskAvailableAtPeakBytes is required")
	}
	if resources.SwapTotalBytes == nil {
		return errors.New("Compose hostResources swapTotalBytes is required")
	}
	if *resources.MemoryTotalBytes < minimumComposeHostMemoryBytes ||
		*resources.MemoryTotalBytes > int64(policy.WholeMachineMemoryMiB)*1024*1024 {
		return fmt.Errorf(
			"Compose host memoryTotalBytes must be in %d..%d for the %d MiB target",
			minimumComposeHostMemoryBytes,
			int64(policy.WholeMachineMemoryMiB)*1024*1024,
			policy.WholeMachineMemoryMiB,
		)
	}
	if *resources.CPUCount != policy.CPUCount {
		return fmt.Errorf("Compose host cpuCount=%d, want %d", *resources.CPUCount, policy.CPUCount)
	}
	if *resources.DiskTotalBytes < minimumComposeHostDiskBytes ||
		*resources.DiskTotalBytes > int64(policy.DiskMiB)*1024*1024 {
		return fmt.Errorf(
			"Compose host diskTotalBytes must be in %d..%d for the %d MiB target",
			minimumComposeHostDiskBytes,
			int64(policy.DiskMiB)*1024*1024,
			policy.DiskMiB,
		)
	}
	if *resources.DiskAvailableAtPeakBytes < minimumComposeDiskAvailableAtPeakBytes ||
		*resources.DiskAvailableAtPeakBytes > *resources.DiskTotalBytes {
		return fmt.Errorf(
			"Compose host diskAvailableAtPeakBytes must be in %d..diskTotalBytes",
			minimumComposeDiskAvailableAtPeakBytes,
		)
	}
	if *resources.SwapTotalBytes != 0 {
		return errors.New("Compose host swapTotalBytes must be 0")
	}
	return nil
}

func validateComposeLimits(limits composeLimits) error {
	if limits.MemoryLimitBytes != expectedContainerMemoryBytes ||
		limits.MemorySwapLimitBytes != expectedContainerMemoryBytes ||
		limits.NanoCPUs != expectedContainerNanoCPUs ||
		limits.PIDsLimit != expectedContainerPIDsLimit {
		return fmt.Errorf(
			"actual Compose limits must be memory=%d bytes, memory+swap=%d bytes, nanoCPUs=%d, and pids=%d",
			expectedContainerMemoryBytes,
			expectedContainerMemoryBytes,
			expectedContainerNanoCPUs,
			expectedContainerPIDsLimit,
		)
	}
	return nil
}

func validateComposeIsolation(isolation composeIsolation) error {
	if !isolation.ReadOnlyRootFS || !isolation.NoNewPrivileges || !isolation.InitEnabled ||
		isolation.InitPID != 1 || !isolation.OrphanReapingPassed {
		return errors.New("Compose isolation must prove read-only rootfs, no-new-privileges, PID 1 init, and orphan reaping")
	}
	if !isSupportedComposeInitProcess(isolation.InitProcess) {
		return fmt.Errorf("Compose initProcess=%q, want docker-init or tini", isolation.InitProcess)
	}
	if !exactStringSliceSet(isolation.CapDrop, []string{"ALL"}) ||
		!exactStringSliceSet(isolation.CapAdd, expectedContainerCapabilities) ||
		!exactStringSliceSet(isolation.EffectiveCapabilities, expectedContainerCapabilities) {
		return fmt.Errorf("Compose capabilities must drop ALL and add/effect only %v", expectedContainerCapabilities)
	}
	if len(isolation.Tmpfs) != len(expectedContainerTmpfs) {
		return fmt.Errorf("Compose tmpfs mount count=%d, want %d", len(isolation.Tmpfs), len(expectedContainerTmpfs))
	}
	seen := make(map[string]struct{}, len(isolation.Tmpfs))
	for _, mount := range isolation.Tmpfs {
		expectedSize, ok := expectedContainerTmpfs[mount.Target]
		if !ok {
			return fmt.Errorf("Compose tmpfs has unsupported target %q", mount.Target)
		}
		if _, duplicate := seen[mount.Target]; duplicate {
			return fmt.Errorf("Compose tmpfs target %q is duplicated", mount.Target)
		}
		seen[mount.Target] = struct{}{}
		if mount.SizeBytes != expectedSize || !mount.Writable || !mount.NoExec || !mount.NoSUID || !mount.NoDev {
			return fmt.Errorf("Compose tmpfs %s must be writable, noexec, nosuid, nodev, and %d bytes", mount.Target, expectedSize)
		}
	}
	return nil
}

func isSupportedComposeInitProcess(process string) bool {
	return process == "docker-init" || process == "tini"
}

func validateComposeLifecycle(lifecycle composeLifecycle) error {
	if lifecycle.ForcedKill == nil || lifecycle.ExitCode == nil || lifecycle.PIDsAfterStop == nil || lifecycle.ZombiesAfterRecovery == nil {
		return errors.New("Compose forcedKill, exitCode, pidsAfterStop, and zombiesAfterRecovery are required")
	}
	if !lifecycle.GracefulStop || *lifecycle.ForcedKill || *lifecycle.ExitCode != 0 || *lifecycle.PIDsAfterStop != 0 {
		return errors.New("Compose stop must be graceful with exit code 0, no forced kill, and zero remaining PIDs")
	}
	if lifecycle.PIDsBaseline <= 0 || lifecycle.PIDsPeak <= lifecycle.PIDsBaseline ||
		lifecycle.PIDsPeak > int(expectedContainerPIDsLimit) || lifecycle.PIDsAfterRecovery <= 0 ||
		lifecycle.PIDsAfterRecovery >= lifecycle.PIDsPeak || *lifecycle.ZombiesAfterRecovery != 0 {
		return fmt.Errorf(
			"Compose process metrics must show positive baseline, a higher peak within %d, recovery below peak, and zero zombies",
			expectedContainerPIDsLimit,
		)
	}
	return nil
}

func validateComposeLogs(logs composeLogs) error {
	if logs.Driver != "json-file" || logs.MaxSizeBytes != expectedContainerLogSizeBytes ||
		logs.MaxFiles != expectedContainerLogFiles || !logs.RotationObserved ||
		logs.PeakBytes <= 0 || logs.PeakBytes > maximumContainerLogPeakBytes {
		return fmt.Errorf(
			"Compose logs must use json-file %d bytes x %d with observed rotation and peak bytes in 1..%d",
			expectedContainerLogSizeBytes,
			expectedContainerLogFiles,
			maximumContainerLogPeakBytes,
		)
	}
	return nil
}

func validateComposeStorage(
	storage composeStorage,
	resources composeHostResources,
	candidateImageDigest string,
	policy acceptancePolicy,
) error {
	if !isSupportedRollbackImageRepository(storage.RollbackImageRepository) {
		return fmt.Errorf(
			"Compose rollbackImageRepository=%q, want %q or %q",
			storage.RollbackImageRepository,
			expectedOfficialRollbackRepository,
			expectedProjectRollbackRepository,
		)
	}
	if !isSHA256Digest(storage.RollbackImageDigest) || storage.RollbackImageDigest == candidateImageDigest {
		return errors.New("Compose rollbackImageDigest must be a valid digest different from the candidate image")
	}
	if !storage.RollbackImagePulled {
		return errors.New("Compose rollback image must be pulled on each architecture")
	}
	if !storage.RollbackImageStarted {
		return errors.New("Compose rollback image must be started on each architecture")
	}
	if !storage.RollbackImageHealthy {
		return errors.New("Compose rollback image must be healthy on each architecture")
	}
	maximumProjectDiskPeakMiB := min(policy.DiskMiB, maximumComposeProjectDiskPeakMiB)
	if !storage.RollbackImagePresentAtPeak || storage.ProjectDiskPeakMiB <= 0 ||
		storage.ProjectDiskPeakMiB > maximumProjectDiskPeakMiB {
		return fmt.Errorf(
			"Compose project disk peak must include one rollback image and be in 1..%d MiB",
			maximumProjectDiskPeakMiB,
		)
	}
	projectDiskPeakBytes := int64(storage.ProjectDiskPeakMiB) * 1024 * 1024
	if projectDiskPeakBytes+*resources.DiskAvailableAtPeakBytes > *resources.DiskTotalBytes {
		return errors.New("Compose project disk peak plus host available bytes cannot exceed host diskTotalBytes")
	}
	return nil
}

func isSupportedRollbackImageRepository(repository string) bool {
	return repository == expectedOfficialRollbackRepository || repository == expectedProjectRollbackRepository
}
