package main

import (
	"slices"
	"strings"
	"testing"
)

func TestValidateComposeEvidence(t *testing.T) {
	candidateDigest := "sha256:" + strings.Repeat("a", 64)
	candidateComposeSHA := strings.Repeat("c", 64)
	policy := validComposePolicy()

	tests := []struct {
		name   string
		mutate func(*composeEvidence)
	}{
		{name: "docker-init"},
		{
			name: "tini",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Isolation.InitProcess = "tini"
			},
		},
		{
			name: "architecture order is irrelevant",
			mutate: func(evidence *composeEvidence) {
				slices.Reverse(evidence.Runs)
			},
		},
		{
			name: "project rollback repository",
			mutate: func(evidence *composeEvidence) {
				for index := range evidence.Runs {
					evidence.Runs[index].Storage.RollbackImageRepository = "ghcr.io/luxiaba/remnanode-lite"
				}
			},
		},
		{
			name: "host resource range boundaries",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.MemoryTotalBytes = int64Pointer(minimumComposeHostMemoryBytes)
				evidence.Runs[0].HostResources.DiskTotalBytes = int64Pointer(minimumComposeHostDiskBytes)
				evidence.Runs[0].HostResources.DiskAvailableAtPeakBytes = int64Pointer(minimumComposeDiskAvailableAtPeakBytes)
				evidence.Runs[1].HostResources.MemoryTotalBytes = int64Pointer(maximumComposeHostMemoryBytesForPolicy(policy))
				evidence.Runs[1].HostResources.DiskTotalBytes = int64Pointer(maximumComposeHostDiskBytesForPolicy(policy))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validComposeEvidence(strings.Repeat("d", 40), candidateDigest, candidateComposeSHA)
			if test.mutate != nil {
				test.mutate(&evidence)
			}
			if err := validateComposeEvidence(evidence, policy, candidateDigest, candidateComposeSHA); err != nil {
				t.Fatalf("validateComposeEvidence() error = %v", err)
			}
		})
	}
}

func TestValidateComposeEvidenceFailures(t *testing.T) {
	candidateDigest := "sha256:" + strings.Repeat("a", 64)
	candidateComposeSHA := strings.Repeat("c", 64)
	policy := validComposePolicy()

	tests := []struct {
		name    string
		mutate  func(*composeEvidence)
		wantErr string
	}{
		{
			name: "missing manifest platform",
			mutate: func(evidence *composeEvidence) {
				evidence.ManifestPlatforms = []string{"linux/amd64"}
			},
			wantErr: "platforms must be linux/amd64 and linux/arm64",
		},
		{
			name: "only one architecture run",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs = evidence.Runs[:1]
			},
			wantErr: "want one amd64 run and one arm64 run",
		},
		{
			name: "duplicate architecture run",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Environment.Arch = "amd64"
			},
			wantErr: "architecture \"amd64\" is duplicated",
		},
		{
			name: "unsupported architecture run",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Environment.Arch = "riscv64"
			},
			wantErr: "unsupported architecture \"riscv64\"",
		},
		{
			name: "missing Engine version on arm64 run",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Environment.DockerEngineVersion = ""
			},
			wantErr: "Compose run \"arm64\": Docker Engine and Docker Compose versions must not be empty",
		},
		{
			name: "missing host memory observation",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.MemoryTotalBytes = nil
			},
			wantErr: "hostResources memoryTotalBytes",
		},
		{
			name: "missing host CPU observation",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.CPUCount = nil
			},
			wantErr: "hostResources cpuCount is required",
		},
		{
			name: "missing host disk observation",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskTotalBytes = nil
			},
			wantErr: "hostResources diskTotalBytes is required",
		},
		{
			name: "missing host available disk observation",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskAvailableAtPeakBytes = nil
			},
			wantErr: "hostResources diskAvailableAtPeakBytes is required",
		},
		{
			name: "missing required zero host swap observation",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.SwapTotalBytes = nil
			},
			wantErr: "hostResources swapTotalBytes is required",
		},
		{
			name: "host memory below target tolerance",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.MemoryTotalBytes = int64Pointer(minimumComposeHostMemoryBytes - 1)
			},
			wantErr: "host memoryTotalBytes must be in",
		},
		{
			name: "host memory above target",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.MemoryTotalBytes = int64Pointer(maximumComposeHostMemoryBytesForPolicy(policy) + 1)
			},
			wantErr: "host memoryTotalBytes must be in",
		},
		{
			name: "host CPU mismatch",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.CPUCount = intPointer(2)
			},
			wantErr: "host cpuCount=2, want 1",
		},
		{
			name: "host disk below target tolerance",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskTotalBytes = int64Pointer(minimumComposeHostDiskBytes - 1)
			},
			wantErr: "host diskTotalBytes must be in",
		},
		{
			name: "host disk above target",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskTotalBytes = int64Pointer(maximumComposeHostDiskBytesForPolicy(policy) + 1)
			},
			wantErr: "host diskTotalBytes must be in",
		},
		{
			name: "host disk reserve too small",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskAvailableAtPeakBytes = int64Pointer(minimumComposeDiskAvailableAtPeakBytes - 1)
			},
			wantErr: "host diskAvailableAtPeakBytes must be in",
		},
		{
			name: "host swap present",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.SwapTotalBytes = int64Pointer(1)
			},
			wantErr: "host swapTotalBytes must be 0",
		},
		{
			name: "container memory limit mismatch",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Limits.MemoryLimitBytes--
			},
			wantErr: "actual Compose limits must be",
		},
		{
			name: "unexpected effective capability",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Isolation.EffectiveCapabilities = append(
					evidence.Runs[0].Isolation.EffectiveCapabilities,
					"SYS_ADMIN",
				)
			},
			wantErr: "add/effect only",
		},
		{
			name: "unsupported init identity",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Isolation.InitProcess = "/usr/bin/tini"
			},
			wantErr: "initProcess=\"/usr/bin/tini\", want docker-init or tini",
		},
		{
			name: "tmpfs flags mismatch",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Isolation.Tmpfs[0].NoExec = false
			},
			wantErr: "must be writable, noexec, nosuid, nodev",
		},
		{
			name: "health result missing on arm64 run",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Health.CheckExitCode = nil
			},
			wantErr: "Compose run \"arm64\": Compose health checkExitCode is required",
		},
		{
			name: "forced stop",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Lifecycle.ForcedKill = boolPointer(true)
			},
			wantErr: "stop must be graceful",
		},
		{
			name: "PIDs did not decline from peak",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Lifecycle.PIDsAfterRecovery = evidence.Runs[0].Lifecycle.PIDsPeak
			},
			wantErr: "recovery below peak",
		},
		{
			name: "log rotation not observed",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Logs.RotationObserved = false
			},
			wantErr: "observed rotation",
		},
		{
			name: "rollback image is candidate",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Storage.RollbackImageDigest = candidateDigest
			},
			wantErr: "different from the candidate image",
		},
		{
			name: "unsupported rollback repository",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Storage.RollbackImageRepository = "example.invalid/remnanode-lite"
			},
			wantErr: "Compose run \"arm64\": Compose rollbackImageRepository=",
		},
		{
			name: "different rollback image across architectures",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Storage.RollbackImageDigest = "sha256:" + strings.Repeat("e", 64)
			},
			wantErr: "runs must use the same rollback image repository and manifest digest",
		},
		{
			name: "rollback image not pulled on arm64",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[1].Storage.RollbackImagePulled = false
			},
			wantErr: "Compose run \"arm64\": Compose rollback image must be pulled on each architecture",
		},
		{
			name: "rollback image not started",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Storage.RollbackImageStarted = false
			},
			wantErr: "rollback image must be started on each architecture",
		},
		{
			name: "rollback image not healthy",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Storage.RollbackImageHealthy = false
			},
			wantErr: "rollback image must be healthy on each architecture",
		},
		{
			name: "rollback image absent at disk peak",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Storage.RollbackImagePresentAtPeak = false
			},
			wantErr: "disk peak must include one rollback image",
		},
		{
			name: "project disk peak consumes reserved host budget",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].Storage.ProjectDiskPeakMiB = maximumComposeProjectDiskPeakMiB + 1
			},
			wantErr: "disk peak must include one rollback image and be in 1..1024 MiB",
		},
		{
			name: "project and available disk observations are inconsistent",
			mutate: func(evidence *composeEvidence) {
				evidence.Runs[0].HostResources.DiskTotalBytes = int64Pointer(minimumComposeHostDiskBytes)
				evidence.Runs[0].HostResources.DiskAvailableAtPeakBytes = int64Pointer(1024 * 1024 * 1024)
				evidence.Runs[0].Storage.ProjectDiskPeakMiB = 1024
			},
			wantErr: "project disk peak plus host available bytes cannot exceed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validComposeEvidence(strings.Repeat("d", 40), candidateDigest, candidateComposeSHA)
			test.mutate(&evidence)
			err := validateComposeEvidence(evidence, policy, candidateDigest, candidateComposeSHA)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateComposeEvidence() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func validComposePolicy() acceptancePolicy {
	return acceptancePolicy{
		WholeMachineMemoryMiB: expectedWholeMachineMemoryMiB,
		ServiceMemoryMaxMiB:   expectedServiceMemoryMaxMiB,
		CPUCount:              expectedCPUCount,
		DiskMiB:               expectedDiskMiB,
	}
}

func maximumComposeHostMemoryBytesForPolicy(policy acceptancePolicy) int64 {
	return int64(policy.WholeMachineMemoryMiB) * 1024 * 1024
}

func maximumComposeHostDiskBytesForPolicy(policy acceptancePolicy) int64 {
	return int64(policy.DiskMiB) * 1024 * 1024
}
