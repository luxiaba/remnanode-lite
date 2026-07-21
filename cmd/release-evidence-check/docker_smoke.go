package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	expectedComposeSourcePath = "deploy/compose.single-file.yaml"
	expectedCandidateImage    = "ghcr.io/luxiaba/remnanode-lite"
	expectedContainerName     = "remnanode"

	minimumDockerSmokeDuration = 600 * time.Second

	minimumHostMemoryBytes = int64(480 * 1024 * 1024)
	maximumHostMemoryBytes = int64(512 * 1024 * 1024)
	minimumHostDiskBytes   = int64(1792 * 1024 * 1024)
	maximumHostDiskBytes   = int64(2048 * 1024 * 1024)

	expectedContainerMemoryBytes = int64(448 * 1024 * 1024)
	expectedContainerNanoCPUs    = int64(1_000_000_000)
	expectedContainerPIDsLimit   = int64(256)
	expectedContainerNofile      = int64(1_048_576)
	expectedStopGracePeriod      = 35
)

type expectedTmpfsMount struct {
	sizeBytes int64
	mode      string
}

var expectedContainerTmpfs = map[string]expectedTmpfsMount{
	"/run/remnanode":     {sizeBytes: 4 * 1024 * 1024, mode: "0700"},
	"/tmp":               {sizeBytes: 16 * 1024 * 1024, mode: "1777"},
	"/var/log/remnanode": {sizeBytes: 28 * 1024 * 1024, mode: "0750"},
}

type dockerSmokeEvidence struct {
	evidenceCommon
	CandidateImageDigest string                 `json:"candidateImageDigest"`
	ImageReference       string                 `json:"imageReference"`
	Source               dockerSmokeSource      `json:"source"`
	ManifestPlatforms    []string               `json:"manifestPlatforms"`
	Environment          dockerSmokeEnvironment `json:"environment"`
	Host                 dockerSmokeHost        `json:"host"`
	Node                 dockerSmokeNode        `json:"node"`
	Container            dockerSmokeContainer   `json:"container"`
	Resources            dockerSmokeResources   `json:"resources"`
	Checks               dockerSmokeChecks      `json:"checks"`
	Panel                dockerSmokePanel       `json:"panel"`
	RawBundleSHA256      string                 `json:"rawBundleSha256"`
	Signoff              dockerSmokeSignoff     `json:"signoff"`
}

type dockerSmokeSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type dockerSmokeEnvironment struct {
	Arch                 string `json:"arch"`
	UnameMachine         string `json:"unameMachine"`
	Kernel               string `json:"kernel"`
	DockerEngineVersion  string `json:"dockerEngineVersion"`
	DockerComposeVersion string `json:"dockerComposeVersion"`
}

type dockerSmokeHost struct {
	MemoryTotalBytes *int64 `json:"memoryTotalBytes"`
	CPUCount         *int   `json:"cpuCount"`
	DiskTotalBytes   *int64 `json:"diskTotalBytes"`
	SwapTotalBytes   *int64 `json:"swapTotalBytes"`
}

type dockerSmokeNode struct {
	VersionOutput string `json:"versionOutput"`
	BinarySHA256  string `json:"binarySha256"`
}

type dockerSmokeContainer struct {
	ID                         string                  `json:"id"`
	Name                       string                  `json:"name"`
	ImageReference             string                  `json:"imageReference"`
	StartedAt                  string                  `json:"startedAt"`
	Status                     string                  `json:"status"`
	HealthStatus               string                  `json:"healthStatus"`
	HealthCheckExitCode        *int                    `json:"healthCheckExitCode"`
	ConsecutiveHealthSuccesses int                     `json:"consecutiveHealthSuccesses"`
	OOMKilled                  *bool                   `json:"oomKilled"`
	RestartCount               *int                    `json:"restartCount"`
	NetworkMode                string                  `json:"networkMode"`
	RestartPolicy              string                  `json:"restartPolicy"`
	ReadOnlyRootFS             *bool                   `json:"readOnlyRootfs"`
	NoNewPrivileges            *bool                   `json:"noNewPrivileges"`
	InitEnabled                *bool                   `json:"initEnabled"`
	InitProcess                string                  `json:"initProcess"`
	CapDrop                    []string                `json:"capDrop"`
	CapAdd                     []string                `json:"capAdd"`
	Tmpfs                      []dockerSmokeTmpfsMount `json:"tmpfs"`
	Healthcheck                dockerSmokeHealthcheck  `json:"healthcheck"`
	Logging                    dockerSmokeLogging      `json:"logging"`
	Nofile                     dockerSmokeNofile       `json:"nofile"`
	StopGracePeriodSeconds     *int                    `json:"stopGracePeriodSeconds"`
	MemoryLimitBytes           *int64                  `json:"memoryLimitBytes"`
	MemorySwapLimitBytes       *int64                  `json:"memorySwapLimitBytes"`
	NanoCPUs                   *int64                  `json:"nanoCPUs"`
	PIDsLimit                  *int64                  `json:"pidsLimit"`
}

type dockerSmokeTmpfsMount struct {
	Target    string `json:"target"`
	SizeBytes *int64 `json:"sizeBytes"`
	Mode      string `json:"mode"`
	Writable  *bool  `json:"writable"`
	NoExec    *bool  `json:"noexec"`
	NoSUID    *bool  `json:"nosuid"`
	NoDev     *bool  `json:"nodev"`
}

type dockerSmokeHealthcheck struct {
	Test               []string `json:"test"`
	IntervalSeconds    *int     `json:"intervalSeconds"`
	TimeoutSeconds     *int     `json:"timeoutSeconds"`
	StartPeriodSeconds *int     `json:"startPeriodSeconds"`
	Retries            *int     `json:"retries"`
}

type dockerSmokeLogging struct {
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options"`
}

type dockerSmokeNofile struct {
	Soft *int64 `json:"soft"`
	Hard *int64 `json:"hard"`
}

type dockerSmokeResources struct {
	MemoryCurrentBytes *int64 `json:"memoryCurrentBytes"`
	MemoryPeakBytes    *int64 `json:"memoryPeakBytes"`
	PIDsCurrent        *int64 `json:"pidsCurrent"`
	PIDsPeak           *int64 `json:"pidsPeak"`
}

type dockerSmokeChecks struct {
	LowMemoryEnabled    bool `json:"lowMemoryEnabled"`
	ASNDatabaseLoaded   bool `json:"asnDatabaseLoaded"`
	InternalSocketReady bool `json:"internalSocketReady"`
	ListenerReady       bool `json:"listenerReady"`
}

type dockerSmokePanel struct {
	Version           string `json:"version"`
	Connected         bool   `json:"connected"`
	RealTrafficPassed bool   `json:"realTrafficPassed"`
}

type dockerSmokeSignoff struct {
	Operator string `json:"operator"`
	Role     string `json:"role"`
	Decision string `json:"decision"`
}

func validateDockerSmokeEvidence(
	evidence dockerSmokeEvidence,
	timing evidenceTiming,
	candidateImageDigest, candidateAMD64NodeSHA, candidateComposeSHA string,
) error {
	if evidence.CandidateImageDigest != candidateImageDigest {
		return fmt.Errorf(
			"candidateImageDigest=%q, want manifest candidateImageDigest %s",
			evidence.CandidateImageDigest,
			candidateImageDigest,
		)
	}
	wantImageReference := expectedCandidateImage + "@" + candidateImageDigest
	if evidence.ImageReference != wantImageReference {
		return fmt.Errorf("imageReference=%q, want %q", evidence.ImageReference, wantImageReference)
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
	if err := validateDockerSmokeEnvironment(evidence.Environment); err != nil {
		return err
	}
	if err := validateDockerSmokeHost(evidence.Host); err != nil {
		return err
	}
	if evidence.Node.VersionOutput != expectedVersionOutput {
		return fmt.Errorf("node versionOutput=%q, want %q", evidence.Node.VersionOutput, expectedVersionOutput)
	}
	if evidence.Node.BinarySHA256 != candidateAMD64NodeSHA {
		return fmt.Errorf("node binarySha256=%q, want manifest candidateNodeSha256.amd64 %s", evidence.Node.BinarySHA256, candidateAMD64NodeSHA)
	}
	containerStartedAt, err := parseRFC3339("Docker smoke container startedAt", evidence.Container.StartedAt)
	if err != nil {
		return err
	}
	if evidence.Container.StartedAt != evidence.StartedAt || !containerStartedAt.Equal(timing.Started) {
		return fmt.Errorf(
			"Docker smoke container startedAt=%q must exactly equal evidence startedAt=%q",
			evidence.Container.StartedAt,
			evidence.StartedAt,
		)
	}
	if err := validateDockerSmokeContainer(evidence.Container, wantImageReference); err != nil {
		return err
	}
	if timing.Finished.Sub(containerStartedAt) < minimumDockerSmokeDuration {
		return fmt.Errorf(
			"Docker smoke wall-clock duration=%s, want at least %s",
			timing.Finished.Sub(containerStartedAt),
			minimumDockerSmokeDuration,
		)
	}
	if err := validateDockerSmokeResources(evidence.Resources); err != nil {
		return err
	}
	if !evidence.Checks.LowMemoryEnabled || !evidence.Checks.ASNDatabaseLoaded ||
		!evidence.Checks.InternalSocketReady || !evidence.Checks.ListenerReady {
		return errors.New("Docker smoke low-memory, ASN database, internal socket, and listener checks must pass")
	}
	if evidence.Panel.Version != expectedPanelVersion || !evidence.Panel.Connected || !evidence.Panel.RealTrafficPassed {
		return fmt.Errorf("Docker smoke Panel must be %s, connected, and pass real proxy traffic", expectedPanelVersion)
	}
	if !isLowerHex(evidence.RawBundleSHA256, 64) {
		return errors.New("rawBundleSha256 must be 64 lowercase hexadecimal characters")
	}
	if evidence.Signoff.Operator != "luxiaba" || evidence.Signoff.Role != "release-owner" || evidence.Signoff.Decision != "accept" {
		return errors.New("Docker smoke signoff must be operator luxiaba, role release-owner, decision accept")
	}
	return nil
}

func validateDockerSmokeEnvironment(environment dockerSmokeEnvironment) error {
	if environment.Arch != "amd64" || environment.UnameMachine != "x86_64" {
		return fmt.Errorf("Docker smoke environment must be amd64/x86_64, got %s/%s", environment.Arch, environment.UnameMachine)
	}
	if strings.TrimSpace(environment.Kernel) == "" ||
		strings.TrimSpace(environment.DockerEngineVersion) == "" ||
		strings.TrimSpace(environment.DockerComposeVersion) == "" {
		return errors.New("Docker smoke kernel, Docker Engine, and Docker Compose versions must not be empty")
	}
	return nil
}

func validateDockerSmokeHost(host dockerSmokeHost) error {
	if host.MemoryTotalBytes == nil || host.CPUCount == nil || host.DiskTotalBytes == nil || host.SwapTotalBytes == nil {
		return errors.New("Docker smoke host memoryTotalBytes, cpuCount, diskTotalBytes, and swapTotalBytes are required")
	}
	if *host.MemoryTotalBytes < minimumHostMemoryBytes || *host.MemoryTotalBytes > maximumHostMemoryBytes {
		return fmt.Errorf("Docker smoke host memoryTotalBytes must be in %d..%d", minimumHostMemoryBytes, maximumHostMemoryBytes)
	}
	if *host.CPUCount != 1 {
		return fmt.Errorf("Docker smoke host cpuCount=%d, want 1", *host.CPUCount)
	}
	if *host.DiskTotalBytes < minimumHostDiskBytes || *host.DiskTotalBytes > maximumHostDiskBytes {
		return fmt.Errorf("Docker smoke host diskTotalBytes must be in %d..%d", minimumHostDiskBytes, maximumHostDiskBytes)
	}
	if *host.SwapTotalBytes != 0 {
		return errors.New("Docker smoke host swapTotalBytes must be 0")
	}
	return nil
}

func validateDockerSmokeContainer(container dockerSmokeContainer, wantImageReference string) error {
	if container.HealthCheckExitCode == nil || container.OOMKilled == nil || container.RestartCount == nil ||
		container.MemoryLimitBytes == nil || container.MemorySwapLimitBytes == nil ||
		container.NanoCPUs == nil || container.PIDsLimit == nil {
		return errors.New("Docker smoke container health, OOM, restart, and limit fields are required")
	}
	if container.Name != expectedContainerName {
		return fmt.Errorf("Docker smoke container name=%q, want %q", container.Name, expectedContainerName)
	}
	if !isLowerHex(container.ID, 64) {
		return errors.New("Docker smoke container id must be 64 lowercase hexadecimal characters")
	}
	if container.ImageReference != wantImageReference {
		return fmt.Errorf("Docker smoke container imageReference=%q, want %q", container.ImageReference, wantImageReference)
	}
	if container.Status != "running" || container.HealthStatus != "healthy" ||
		*container.HealthCheckExitCode != 0 || container.ConsecutiveHealthSuccesses < 1 {
		return errors.New("Docker smoke container must be running and healthy with exit code 0 and at least one successful health check")
	}
	if *container.OOMKilled || *container.RestartCount != 0 {
		return errors.New("Docker smoke container must have no OOM kill and zero restarts")
	}
	if *container.MemoryLimitBytes != expectedContainerMemoryBytes ||
		*container.MemorySwapLimitBytes != expectedContainerMemoryBytes ||
		*container.NanoCPUs != expectedContainerNanoCPUs ||
		*container.PIDsLimit != expectedContainerPIDsLimit {
		return fmt.Errorf(
			"Docker smoke container limits must be memory=%d, memory+swap=%d, nanoCPUs=%d, pids=%d",
			expectedContainerMemoryBytes,
			expectedContainerMemoryBytes,
			expectedContainerNanoCPUs,
			expectedContainerPIDsLimit,
		)
	}
	if err := validateDockerSmokeInspectConfig(container); err != nil {
		return err
	}
	return nil
}

func validateDockerSmokeInspectConfig(container dockerSmokeContainer) error {
	if container.ReadOnlyRootFS == nil || container.NoNewPrivileges == nil || container.InitEnabled == nil ||
		container.StopGracePeriodSeconds == nil {
		return errors.New("Docker smoke readOnlyRootfs, noNewPrivileges, initEnabled, and stopGracePeriodSeconds are required")
	}
	if container.NetworkMode != "host" || container.RestartPolicy != "unless-stopped" {
		return fmt.Errorf(
			"Docker smoke networkMode/restartPolicy=%s/%s, want host/unless-stopped",
			container.NetworkMode,
			container.RestartPolicy,
		)
	}
	if !*container.ReadOnlyRootFS || !*container.NoNewPrivileges || !*container.InitEnabled {
		return errors.New("Docker smoke must use a read-only rootfs, no-new-privileges, and container init")
	}
	if container.InitProcess != "docker-init" && container.InitProcess != "tini" {
		return fmt.Errorf("Docker smoke initProcess=%q, want docker-init or tini", container.InitProcess)
	}
	if !exactStringSliceSet(container.CapDrop, []string{"ALL"}) ||
		!exactStringSliceSet(container.CapAdd, []string{"NET_ADMIN", "NET_BIND_SERVICE"}) {
		return errors.New("Docker smoke capabilities must drop exactly ALL and add exactly NET_ADMIN and NET_BIND_SERVICE")
	}
	if err := validateDockerSmokeTmpfs(container.Tmpfs); err != nil {
		return err
	}
	if err := validateDockerSmokeHealthcheck(container.Healthcheck); err != nil {
		return err
	}
	if err := validateDockerSmokeLogging(container.Logging); err != nil {
		return err
	}
	if container.Nofile.Soft == nil || container.Nofile.Hard == nil {
		return errors.New("Docker smoke nofile soft and hard limits are required")
	}
	if *container.Nofile.Soft != expectedContainerNofile || *container.Nofile.Hard != expectedContainerNofile {
		return fmt.Errorf("Docker smoke nofile soft/hard must both be %d", expectedContainerNofile)
	}
	if *container.StopGracePeriodSeconds != expectedStopGracePeriod {
		return fmt.Errorf("Docker smoke stopGracePeriodSeconds=%d, want %d", *container.StopGracePeriodSeconds, expectedStopGracePeriod)
	}
	return nil
}

func validateDockerSmokeTmpfs(mounts []dockerSmokeTmpfsMount) error {
	if len(mounts) != len(expectedContainerTmpfs) {
		return fmt.Errorf("Docker smoke tmpfs mount count=%d, want %d", len(mounts), len(expectedContainerTmpfs))
	}
	seen := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		expected, ok := expectedContainerTmpfs[mount.Target]
		if !ok {
			return fmt.Errorf("Docker smoke tmpfs has unsupported target %q", mount.Target)
		}
		if _, duplicate := seen[mount.Target]; duplicate {
			return fmt.Errorf("Docker smoke tmpfs target %q is duplicated", mount.Target)
		}
		seen[mount.Target] = struct{}{}
		if mount.SizeBytes == nil || mount.Writable == nil || mount.NoExec == nil || mount.NoSUID == nil || mount.NoDev == nil {
			return fmt.Errorf("Docker smoke tmpfs %s size and mount flags are required", mount.Target)
		}
		if *mount.SizeBytes != expected.sizeBytes || mount.Mode != expected.mode ||
			!*mount.Writable || !*mount.NoExec || !*mount.NoSUID || !*mount.NoDev {
			return fmt.Errorf(
				"Docker smoke tmpfs %s must be %d bytes, mode %s, and writable,noexec,nosuid,nodev",
				mount.Target,
				expected.sizeBytes,
				expected.mode,
			)
		}
	}
	return nil
}

func validateDockerSmokeHealthcheck(healthcheck dockerSmokeHealthcheck) error {
	if healthcheck.IntervalSeconds == nil || healthcheck.TimeoutSeconds == nil ||
		healthcheck.StartPeriodSeconds == nil || healthcheck.Retries == nil {
		return errors.New("Docker smoke healthcheck interval, timeout, start period, and retries are required")
	}
	expectedTest := []string{"CMD", "/usr/local/bin/remnanode-lite", "healthcheck"}
	if !slices.Equal(healthcheck.Test, expectedTest) ||
		*healthcheck.IntervalSeconds != 30 || *healthcheck.TimeoutSeconds != 5 ||
		*healthcheck.StartPeriodSeconds != 10 || *healthcheck.Retries != 3 {
		return errors.New("Docker smoke healthcheck must use CMD remnanode-lite healthcheck with 30s interval, 5s timeout, 10s start period, and 3 retries")
	}
	return nil
}

func validateDockerSmokeLogging(logging dockerSmokeLogging) error {
	if logging.Driver != "json-file" || len(logging.Options) != 2 ||
		logging.Options["max-size"] != "2m" || logging.Options["max-file"] != "2" {
		return errors.New("Docker smoke logging must use json-file with exactly max-size=2m and max-file=2")
	}
	return nil
}

func validateDockerSmokeResources(resources dockerSmokeResources) error {
	if resources.MemoryCurrentBytes == nil || resources.MemoryPeakBytes == nil ||
		resources.PIDsCurrent == nil || resources.PIDsPeak == nil {
		return errors.New("Docker smoke memory and PID current/peak metrics are required")
	}
	if *resources.MemoryCurrentBytes <= 0 || *resources.MemoryPeakBytes < *resources.MemoryCurrentBytes ||
		*resources.MemoryPeakBytes > expectedContainerMemoryBytes {
		return fmt.Errorf("Docker smoke memory must have 0 < current <= peak <= %d", expectedContainerMemoryBytes)
	}
	if *resources.PIDsCurrent <= 0 || *resources.PIDsPeak < *resources.PIDsCurrent ||
		*resources.PIDsPeak > expectedContainerPIDsLimit {
		return fmt.Errorf("Docker smoke PIDs must have 0 < current <= peak <= %d", expectedContainerPIDsLimit)
	}
	return nil
}

func isSafeEvidenceCommandArgument(argument string) bool {
	for _, char := range argument {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	lower := strings.ToLower(argument)
	for _, fragment := range []string{
		"secret",
		"token",
		"jwt",
		"authorization",
		"password",
		"api-key",
		"apikey",
		"panel-url",
		"panel_url",
		"://",
	} {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	return true
}
