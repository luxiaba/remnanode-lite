package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateDockerSmokeEvidence(t *testing.T) {
	evidence := validDockerSmokeEvidence(
		strings.Repeat("a", 40),
		"sha256:"+strings.Repeat("b", 64),
		strings.Repeat("c", 64),
		strings.Repeat("d", 64),
	)
	timing := evidenceTiming{
		Started:  time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 7, 20, 12, 10, 0, 0, time.UTC),
	}
	if err := validateDockerSmokeEvidence(
		evidence,
		timing,
		evidence.CandidateImageDigest,
		evidence.Node.BinarySHA256,
		evidence.Source.SHA256,
	); err != nil {
		t.Fatalf("validateDockerSmokeEvidence() error = %v", err)
	}
}

func TestValidateDockerSmokeEvidenceFailures(t *testing.T) {
	candidateDigest := "sha256:" + strings.Repeat("b", 64)
	candidateNodeSHA := strings.Repeat("c", 64)
	candidateComposeSHA := strings.Repeat("d", 64)
	validTiming := evidenceTiming{
		Started:  time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
		Finished: time.Date(2026, 7, 20, 12, 10, 0, 0, time.UTC),
	}

	tests := []struct {
		name    string
		mutate  func(*dockerSmokeEvidence, *evidenceTiming)
		wantErr string
	}{
		{
			name: "candidate digest mismatch",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.CandidateImageDigest = "sha256:" + strings.Repeat("0", 64)
			},
			wantErr: "candidateImageDigest",
		},
		{
			name: "movable image reference",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.ImageReference = expectedCandidateImage + ":latest"
			},
			wantErr: "imageReference",
		},
		{
			name: "wrong source path",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Source.Path = "compose.yaml"
			},
			wantErr: "source path",
		},
		{
			name: "wrong source hash",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Source.SHA256 = strings.Repeat("0", 64)
			},
			wantErr: "candidate Git object",
		},
		{
			name: "single manifest platform",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.ManifestPlatforms = []string{"linux/amd64"}
			},
			wantErr: "platforms",
		},
		{
			name: "duplicate manifest platform",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.ManifestPlatforms = []string{"linux/amd64", "linux/amd64"}
			},
			wantErr: "platforms",
		},
		{
			name: "wrong architecture",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Environment.Arch = "arm64"
				evidence.Environment.UnameMachine = "aarch64"
			},
			wantErr: "amd64/x86_64",
		},
		{
			name: "empty kernel",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Environment.Kernel = " "
			},
			wantErr: "must not be empty",
		},
		{
			name: "missing host memory",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Host.MemoryTotalBytes = nil
			},
			wantErr: "host memoryTotalBytes",
		},
		{
			name: "host memory is not positive",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Host.MemoryTotalBytes = int64Pointer(0)
			},
			wantErr: "memoryTotalBytes must be positive",
		},
		{
			name: "host cpu is not positive",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Host.CPUCount = intPointer(0)
			},
			wantErr: "cpuCount must be positive",
		},
		{
			name: "host disk is not positive",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Host.DiskTotalBytes = int64Pointer(0)
			},
			wantErr: "diskTotalBytes must be positive",
		},
		{
			name: "host swap is negative",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Host.SwapTotalBytes = int64Pointer(-1)
			},
			wantErr: "swapTotalBytes must not be negative",
		},
		{
			name: "wrong node version",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Node.VersionOutput = "remnanode-lite 0.0.0"
			},
			wantErr: "versionOutput",
		},
		{
			name: "node binary differs from candidate",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Node.BinarySHA256 = strings.Repeat("0", 64)
			},
			wantErr: "candidateNodeSha256.amd64",
		},
		{
			name: "missing container state",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.OOMKilled = nil
			},
			wantErr: "fields are required",
		},
		{
			name: "wrong container name",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.Name = "other"
			},
			wantErr: "container name",
		},
		{
			name: "invalid container id",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.ID = "short"
			},
			wantErr: "container id",
		},
		{
			name: "container image differs from accepted digest",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.ImageReference = expectedCandidateImage + ":latest"
			},
			wantErr: "container imageReference",
		},
		{
			name: "container started after evidence start",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.StartedAt = "2026-07-20T12:00:01Z"
			},
			wantErr: "must exactly equal evidence startedAt",
		},
		{
			name: "container not healthy",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.HealthStatus = "none"
			},
			wantErr: "running and healthy",
		},
		{
			name: "health exit nonzero",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.HealthCheckExitCode = intPointer(1)
			},
			wantErr: "running and healthy",
		},
		{
			name: "no successful health check",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.ConsecutiveHealthSuccesses = 0
			},
			wantErr: "at least one",
		},
		{
			name: "oom killed",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.OOMKilled = boolPointer(true)
			},
			wantErr: "no OOM kill",
		},
		{
			name: "restarted",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.RestartCount = intPointer(1)
			},
			wantErr: "zero restarts",
		},
		{
			name: "wrong memory limit",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.MemoryLimitBytes = int64Pointer(expectedContainerMemoryBytes + 1)
			},
			wantErr: "container limits",
		},
		{
			name: "container swap is enabled",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.MemorySwapLimitBytes = int64Pointer(expectedContainerMemoryBytes + 1)
			},
			wantErr: "container limits",
		},
		{
			name: "wrong cpu limit",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.NanoCPUs = int64Pointer(expectedContainerNanoCPUs + 1)
			},
			wantErr: "container limits",
		},
		{
			name: "wrong pids limit",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.PIDsLimit = int64Pointer(expectedContainerPIDsLimit - 1)
			},
			wantErr: "container limits",
		},
		{
			name: "wrong network mode",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.NetworkMode = "bridge"
			},
			wantErr: "networkMode/restartPolicy",
		},
		{
			name: "writable root filesystem",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.ReadOnlyRootFS = boolPointer(false)
			},
			wantErr: "read-only rootfs",
		},
		{
			name: "wrong init process",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.InitProcess = "sh"
			},
			wantErr: "initProcess",
		},
		{
			name: "extra capability",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.CapAdd = append(evidence.Container.CapAdd, "SYS_ADMIN")
			},
			wantErr: "capabilities",
		},
		{
			name: "wrong tmpfs mode",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.Tmpfs[0].Mode = "0755"
			},
			wantErr: "tmpfs /run/remnanode",
		},
		{
			name: "wrong healthcheck interval",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.Healthcheck.IntervalSeconds = intPointer(60)
			},
			wantErr: "healthcheck must use",
		},
		{
			name: "extra logging option",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.Logging.Options["compress"] = "true"
			},
			wantErr: "logging must use",
		},
		{
			name: "wrong nofile limit",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.Nofile.Soft = int64Pointer(1024)
			},
			wantErr: "nofile",
		},
		{
			name: "wrong stop grace period",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Container.StopGracePeriodSeconds = intPointer(10)
			},
			wantErr: "stopGracePeriodSeconds",
		},
		{
			name: "short observation",
			mutate: func(_ *dockerSmokeEvidence, timing *evidenceTiming) {
				timing.Finished = timing.Started.Add(minimumDockerSmokeDuration - time.Second)
			},
			wantErr: "wall-clock duration",
		},
		{
			name: "missing resource metric",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Resources.MemoryPeakBytes = nil
			},
			wantErr: "metrics are required",
		},
		{
			name: "memory current above peak",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Resources.MemoryCurrentBytes = int64Pointer(100)
				evidence.Resources.MemoryPeakBytes = int64Pointer(99)
			},
			wantErr: "0 < current <= peak",
		},
		{
			name: "memory peak above limit",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Resources.MemoryPeakBytes = int64Pointer(expectedContainerMemoryBytes + 1)
			},
			wantErr: "0 < current <= peak",
		},
		{
			name: "pids current above peak",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Resources.PIDsCurrent = int64Pointer(20)
				evidence.Resources.PIDsPeak = int64Pointer(19)
			},
			wantErr: "PIDs",
		},
		{
			name: "low memory check failed",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Checks.LowMemoryEnabled = false
			},
			wantErr: "checks must pass",
		},
		{
			name: "wrong panel version",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Panel.Version = "2.8.0"
			},
			wantErr: "Panel must be",
		},
		{
			name: "panel disconnected",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Panel.Connected = false
			},
			wantErr: "connected",
		},
		{
			name: "no real traffic",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Panel.RealTrafficPassed = false
			},
			wantErr: "real proxy traffic",
		},
		{
			name: "invalid raw bundle hash",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.RawBundleSHA256 = "missing"
			},
			wantErr: "rawBundleSha256",
		},
		{
			name: "wrong signoff operator",
			mutate: func(evidence *dockerSmokeEvidence, _ *evidenceTiming) {
				evidence.Signoff.Operator = "someone"
			},
			wantErr: "signoff",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validDockerSmokeEvidence(strings.Repeat("a", 40), candidateDigest, candidateNodeSHA, candidateComposeSHA)
			timing := validTiming
			test.mutate(&evidence, &timing)
			err := validateDockerSmokeEvidence(evidence, timing, candidateDigest, candidateNodeSHA, candidateComposeSHA)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateDockerSmokeEvidence() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestIsSafeEvidenceCommandArgument(t *testing.T) {
	for _, safe := range []string{"scripts/collect-docker-smoke.sh", "--container", "remnanode-lite"} {
		if !isSafeEvidenceCommandArgument(safe) {
			t.Errorf("isSafeEvidenceCommandArgument(%q) = false", safe)
		}
	}
	for _, unsafe := range []string{
		"SECRET_KEY=value",
		"--token=value",
		"https://panel.example",
		"line\nbreak",
	} {
		if isSafeEvidenceCommandArgument(unsafe) {
			t.Errorf("isSafeEvidenceCommandArgument(%q) = true", unsafe)
		}
	}
}

func validDockerSmokeEvidence(candidate, candidateImageDigest, candidateNodeSHA, sourceSHA string) dockerSmokeEvidence {
	return dockerSmokeEvidence{
		evidenceCommon: evidenceCommon{
			SchemaVersion:   expectedSchemaVersion,
			Kind:            "docker-production-smoke",
			CandidateCommit: candidate,
			Status:          "pass",
			StartedAt:       "2026-07-20T12:00:00Z",
			FinishedAt:      "2026-07-20T12:10:00Z",
			Command:         []string{"scripts/collect-docker-smoke.sh", "--container", expectedContainerName},
		},
		CandidateImageDigest: candidateImageDigest,
		ImageReference:       expectedCandidateImage + "@" + candidateImageDigest,
		Source: dockerSmokeSource{
			Path: expectedComposeSourcePath, SHA256: sourceSHA,
		},
		ManifestPlatforms: []string{"linux/amd64", "linux/arm64"},
		Environment: dockerSmokeEnvironment{
			Arch: "amd64", UnameMachine: "x86_64", Kernel: "6.8.0",
			DockerEngineVersion: "28.3.2", DockerComposeVersion: "2.38.2",
		},
		Host: dockerSmokeHost{
			MemoryTotalBytes: int64Pointer(2_061_541_376),
			CPUCount:         intPointer(2),
			DiskTotalBytes:   int64Pointer(21_474_836_480),
			SwapTotalBytes:   int64Pointer(1_073_737_728),
		},
		Node: dockerSmokeNode{
			VersionOutput: expectedVersionOutput, BinarySHA256: candidateNodeSHA,
		},
		Container: dockerSmokeContainer{
			ID: strings.Repeat("f", 64), Name: expectedContainerName,
			ImageReference: expectedCandidateImage + "@" + candidateImageDigest,
			StartedAt:      "2026-07-20T12:00:00Z",
			Status:         "running", HealthStatus: "healthy",
			HealthCheckExitCode: intPointer(0), ConsecutiveHealthSuccesses: 3,
			OOMKilled: boolPointer(false), RestartCount: intPointer(0),
			NetworkMode: "host", RestartPolicy: "unless-stopped",
			ReadOnlyRootFS: boolPointer(true), NoNewPrivileges: boolPointer(true),
			InitEnabled: boolPointer(true), InitProcess: "docker-init",
			CapDrop: []string{"ALL"}, CapAdd: []string{"NET_ADMIN", "NET_BIND_SERVICE"},
			Tmpfs: []dockerSmokeTmpfsMount{
				{Target: "/run/remnanode", SizeBytes: int64Pointer(4 * 1024 * 1024), Mode: "0700", Writable: boolPointer(true), NoExec: boolPointer(true), NoSUID: boolPointer(true), NoDev: boolPointer(true)},
				{Target: "/tmp", SizeBytes: int64Pointer(16 * 1024 * 1024), Mode: "1777", Writable: boolPointer(true), NoExec: boolPointer(true), NoSUID: boolPointer(true), NoDev: boolPointer(true)},
				{Target: "/var/log/remnanode", SizeBytes: int64Pointer(28 * 1024 * 1024), Mode: "0750", Writable: boolPointer(true), NoExec: boolPointer(true), NoSUID: boolPointer(true), NoDev: boolPointer(true)},
			},
			Healthcheck: dockerSmokeHealthcheck{
				Test:            []string{"CMD", "/usr/local/bin/remnanode-lite", "healthcheck"},
				IntervalSeconds: intPointer(30), TimeoutSeconds: intPointer(5),
				StartPeriodSeconds: intPointer(10), Retries: intPointer(3),
			},
			Logging: dockerSmokeLogging{
				Driver: "json-file", Options: map[string]string{"max-size": "2m", "max-file": "2"},
			},
			Nofile: dockerSmokeNofile{
				Soft: int64Pointer(1_048_576), Hard: int64Pointer(1_048_576),
			},
			StopGracePeriodSeconds: intPointer(35),
			MemoryLimitBytes:       int64Pointer(expectedContainerMemoryBytes),
			MemorySwapLimitBytes:   int64Pointer(expectedContainerMemoryBytes),
			NanoCPUs:               int64Pointer(expectedContainerNanoCPUs),
			PIDsLimit:              int64Pointer(expectedContainerPIDsLimit),
		},
		Resources: dockerSmokeResources{
			MemoryCurrentBytes: int64Pointer(28 * 1024 * 1024),
			MemoryPeakBytes:    int64Pointer(30 * 1024 * 1024),
			PIDsCurrent:        int64Pointer(19),
			PIDsPeak:           int64Pointer(23),
		},
		Checks: dockerSmokeChecks{
			LowMemoryEnabled: true, ASNDatabaseLoaded: true,
			InternalSocketReady: true, ListenerReady: true,
		},
		Panel: dockerSmokePanel{
			Version: expectedPanelVersion, Connected: true, RealTrafficPassed: true,
		},
		RawBundleSHA256: strings.Repeat("e", 64),
		Signoff: dockerSmokeSignoff{
			Operator: "luxiaba", Role: "release-owner", Decision: "accept",
		},
	}
}
