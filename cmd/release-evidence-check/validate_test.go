package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	projectversion "github.com/luxiaba/remnanode-lite/internal/version"
)

var (
	testCandidateAt = time.Date(2026, 7, 20, 11, 50, 0, 0, time.UTC)
	testStartedAt   = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	testFinishedAt  = time.Date(2026, 7, 20, 12, 10, 0, 0, time.UTC)
	testAcceptedAt  = time.Date(2026, 7, 20, 12, 11, 0, 0, time.UTC)
	testFinalAt     = time.Date(2026, 7, 20, 12, 12, 0, 0, time.UTC)
	testNow         = time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
)

type releaseFixture struct {
	t                    *testing.T
	root                 string
	candidate            string
	candidateTree        string
	candidateImageDigest string
	manifestPath         string
}

func TestReleaseProfileMatchesProjectIdentity(t *testing.T) {
	if expectedReleaseVersion != projectversion.Version {
		t.Fatalf("release profile version = %q, project version = %q", expectedReleaseVersion, projectversion.Version)
	}
	if expectedOfficialNodeVersion != projectversion.ContractVersion {
		t.Fatalf("profile contract = %q, project contract = %q", expectedOfficialNodeVersion, projectversion.ContractVersion)
	}
	if expectedReleaseTag != "v"+projectversion.Version {
		t.Fatalf("release profile tag = %q", expectedReleaseTag)
	}
	if expectedAcceptanceProfile != "docker-production-smoke-v2" {
		t.Fatalf("acceptance profile = %q", expectedAcceptanceProfile)
	}
}

func TestReleaseFinalizationAllowlist(t *testing.T) {
	for _, path := range []string{
		"README.md",
		"README.ru.md",
		"README.zh-CN.md",
		"CHANGELOG.md",
		"docs/development/roadmap.md",
		"docs/i18n/zh-CN/development/roadmap.md",
		releaseNoteRepositoryPath,
		manifestRepositoryPath,
		acceptanceDirectory + "/docker-smoke.json",
	} {
		if !isAllowedPostCandidatePath(path) {
			t.Errorf("release finalization path %q is not allowed", path)
		}
	}
	for _, path := range []string{
		"cmd/remnanode-lite/main.go",
		acceptanceDirectory + "/systemd.json",
		acceptanceDirectory + "/compose.json",
		acceptanceDirectory + "/raw-output.json",
	} {
		if isAllowedPostCandidatePath(path) {
			t.Errorf("non-finalization path %q is allowed", path)
		}
	}
}

func TestValidateRisks(t *testing.T) {
	valid := releaseRisk{
		ID: "risk", Severity: "P3", Status: "open", Summary: "summary", Mitigation: "mitigation",
		ReleaseBlocking: boolPointer(false),
	}
	if err := validateRisks([]releaseRisk{valid}); err != nil {
		t.Fatalf("validateRisks() error = %v", err)
	}
	tests := []struct {
		name    string
		mutate  func(*releaseRisk)
		wantErr string
	}{
		{name: "empty id", mutate: func(risk *releaseRisk) { risk.ID = " " }, wantErr: "id must not be empty"},
		{name: "bad severity", mutate: func(risk *releaseRisk) { risk.Severity = "P0" }, wantErr: "unsupported severity"},
		{name: "bad status", mutate: func(risk *releaseRisk) { risk.Status = "waived" }, wantErr: "unsupported status"},
		{name: "missing blocking decision", mutate: func(risk *releaseRisk) { risk.ReleaseBlocking = nil }, wantErr: "releaseBlocking is required"},
		{name: "blocking", mutate: func(risk *releaseRisk) { risk.ReleaseBlocking = boolPointer(true) }, wantErr: "release-blocking"},
		{name: "open P2", mutate: func(risk *releaseRisk) { risk.Severity = "P2" }, wantErr: "unclosed P2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			risk := valid
			test.mutate(&risk)
			err := validateRisks([]releaseRisk{risk})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateRisks() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
	if err := validateRisks([]releaseRisk{valid, valid}); err == nil || !strings.Contains(err.Error(), "duplicate risk id") {
		t.Fatalf("validateRisks() duplicate error = %v", err)
	}
}

func TestValidateReleaseEvidence(t *testing.T) {
	fixture := newReleaseFixture(t)
	result, err := validateReleaseEvidenceAt(
		context.Background(), fixture.root, fixture.manifestPath, expectedReleaseTag, testNow,
	)
	if err != nil {
		t.Fatalf("validateReleaseEvidenceAt() error = %v", err)
	}
	if result.ReleaseTag != expectedReleaseTag || result.CandidateCommit != fixture.candidate ||
		result.CandidateImageDigest != fixture.candidateImageDigest {
		t.Fatalf("validationResult = %#v", result)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if result.NodeSHA256ByArch[arch] != testNodeSHA(arch) {
			t.Errorf("NodeSHA256ByArch[%s] = %q, want %q", arch, result.NodeSHA256ByArch[arch], testNodeSHA(arch))
		}
	}
	if fixture.isDirty() {
		t.Fatal("validation changed the fixture worktree")
	}
}

func TestValidateReleaseEvidenceRejectsWrongTag(t *testing.T) {
	fixture := newReleaseFixture(t)
	_, err := validateReleaseEvidenceAt(
		context.Background(), fixture.root, fixture.manifestPath, "v2.8.0-rnl.1", testNow,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("validateReleaseEvidenceAt() error = %v", err)
	}
}

func TestValidateReleaseEvidenceFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*releaseFixture)
		wantErr string
	}{
		{
			name: "wrong acceptance profile",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.AcceptanceProfile = "m8"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "acceptanceProfile",
		},
		{
			name: "invalid candidate image digest",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.CandidateImageDigest = "sha256:BAD"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "candidateImageDigest",
		},
		{
			name: "unknown candidate commit",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.CandidateCommit = strings.Repeat("0", 40)
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "verify candidate commit",
		},
		{
			name: "candidate is not ancestor",
			mutate: func(fixture *releaseFixture) {
				commit, tree := fixture.createNonAncestorCandidate()
				manifest := fixture.readManifest()
				manifest.CandidateCommit = commit
				manifest.CandidateTree = tree
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "is not an ancestor of HEAD",
		},
		{
			name: "invalid candidate node hash",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.CandidateNodeSHA256.ARM64 = "missing"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "candidateNodeSha256",
		},
		{
			name: "deferred validation incomplete",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.DeferredValidation = manifest.DeferredValidation[:len(manifest.DeferredValidation)-1]
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "deferredValidation",
		},
		{
			name: "deferred validation reordered",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.DeferredValidation[0], manifest.DeferredValidation[1] = manifest.DeferredValidation[1], manifest.DeferredValidation[0]
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "deferredValidation",
		},
		{
			name: "missing evidence reference",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Evidence = nil
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "evidence count=0",
		},
		{
			name: "wrong evidence kind",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Evidence[0].Kind = "compose"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "unsupported evidence kind",
		},
		{
			name: "wrong evidence path",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Evidence[0].Path = acceptanceDirectory + "/../docker-smoke.json"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "path=",
		},
		{
			name: "evidence status not pass",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Evidence[0].Status = "fail"
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "status=\"fail\"",
		},
		{
			name: "release blocking risk",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Risks = []releaseRisk{{
					ID: "risk", Severity: "P3", Status: "open", Summary: "summary", Mitigation: "mitigation",
					ReleaseBlocking: boolPointer(true),
				}}
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "release-blocking",
		},
		{
			name: "candidate tree mismatch",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.CandidateTree = strings.Repeat("0", 40)
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "candidate tree=",
		},
		{
			name: "evidence candidate mismatch",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.CandidateCommit = strings.Repeat("0", 40)
				})
			},
			wantErr: "candidateCommit=",
		},
		{
			name: "evidence digest mismatch",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.CandidateImageDigest = "sha256:" + strings.Repeat("0", 64)
				})
			},
			wantErr: "candidateImageDigest",
		},
		{
			name: "candidate compose hash mismatch",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.Source.SHA256 = strings.Repeat("0", 64)
				})
			},
			wantErr: "candidate Git object",
		},
		{
			name: "candidate node hash mismatch",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.Node.BinarySHA256 = strings.Repeat("0", 64)
				})
			},
			wantErr: "candidateNodeSha256.amd64",
		},
		{
			name: "unsafe command",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.Command = []string{"collector", "SECRET_KEY=value"}
				})
			},
			wantErr: "potentially sensitive",
		},
		{
			name: "short smoke",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.FinishedAt = testStartedAt.Add(minimumDockerSmokeDuration - time.Second).Format(time.RFC3339)
				})
			},
			wantErr: "wall-clock duration",
		},
		{
			name: "accepted before evidence completion",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.AcceptedAt = testFinishedAt.Add(-time.Second).Format(time.RFC3339)
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "before evidence completion",
		},
		{
			name: "accepted in future",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.AcceptedAt = testNow.Add(10 * time.Minute).Format(time.RFC3339)
				fixture.writeManifest(manifest)
				fixture.amendAll()
			},
			wantErr: "later than current time",
		},
		{
			name: "evidence predates candidate",
			mutate: func(fixture *releaseFixture) {
				fixture.mutateSmoke(func(evidence *dockerSmokeEvidence) {
					evidence.StartedAt = testCandidateAt.Add(-time.Second).Format(time.RFC3339)
				})
			},
			wantErr: "predates candidate",
		},
		{
			name: "unknown manifest field",
			mutate: func(fixture *releaseFixture) {
				raw := appendJSONField(fixture.readFile(manifestRepositoryPath), `"unexpected":true`)
				fixture.writeFile(manifestRepositoryPath, raw)
				fixture.amendAll()
			},
			wantErr: "unknown field",
		},
		{
			name: "duplicate manifest field",
			mutate: func(fixture *releaseFixture) {
				raw := appendJSONField(fixture.readFile(manifestRepositoryPath), `"releaseTag":"v2.8.0"`)
				fixture.writeFile(manifestRepositoryPath, raw)
				fixture.amendAll()
			},
			wantErr: "duplicate JSON field",
		},
		{
			name: "unknown smoke field",
			mutate: func(fixture *releaseFixture) {
				path := acceptanceDirectory + "/docker-smoke.json"
				raw := appendJSONField(fixture.readFile(path), `"unexpected":true`)
				fixture.writeFile(path, raw)
				fixture.updateEvidenceHash()
				fixture.amendAll()
			},
			wantErr: "unknown field",
		},
		{
			name: "evidence SHA mismatch",
			mutate: func(fixture *releaseFixture) {
				path := acceptanceDirectory + "/docker-smoke.json"
				raw := appendJSONField(fixture.readFile(path), `"unexpected":true`)
				fixture.writeFile(path, raw)
				fixture.amendAll()
			},
			wantErr: "SHA-256=",
		},
		{
			name: "dirty manifest",
			mutate: func(fixture *releaseFixture) {
				manifest := fixture.readManifest()
				manifest.Decision = "fail"
				fixture.writeManifest(manifest)
			},
			wantErr: "worktree bytes do not match HEAD blob",
		},
		{
			name: "executable evidence",
			mutate: func(fixture *releaseFixture) {
				path := filepath.Join(fixture.root, filepath.FromSlash(acceptanceDirectory+"/docker-smoke.json"))
				if err := os.Chmod(path, 0o755); err != nil {
					fixture.t.Fatal(err)
				}
				fixture.amendAll()
			},
			wantErr: "HEAD mode 100755",
		},
		{
			name: "symlink acceptance directory",
			mutate: func(fixture *releaseFixture) {
				fixture.replaceAcceptanceDirectoryWithSymlink()
			},
			wantErr: "symlink",
		},
		{
			name: "index conflict",
			mutate: func(fixture *releaseFixture) {
				fixture.makeIndexConflict(manifestRepositoryPath)
			},
			wantErr: "exactly one Git index entry",
		},
		{
			name: "extra acceptance file",
			mutate: func(fixture *releaseFixture) {
				fixture.writeFile(acceptanceDirectory+"/raw-output.json", []byte("{}\n"))
				fixture.amendAll()
			},
			wantErr: "acceptance files in HEAD must be exactly",
		},
		{
			name: "code changed during finalization",
			mutate: func(fixture *releaseFixture) {
				fixture.writeFile("code.txt", []byte("changed\n"))
				fixture.amendAll()
			},
			wantErr: "outside the release-finalization allowlist",
		},
		{
			name: "rename cannot hide code deletion",
			mutate: func(fixture *releaseFixture) {
				fixture.git("mv", "code.txt", "README.md")
				fixture.amendAll()
			},
			wantErr: "outside the release-finalization allowlist",
		},
		{
			name: "multiple finalization commits",
			mutate: func(fixture *releaseFixture) {
				fixture.writeFile("README.md", []byte("release\n"))
				fixture.commitAllAt("second finalization", testFinalAt.Add(time.Minute))
			},
			wantErr: "exactly one single-parent commit",
		},
		{
			name: "merge finalization",
			mutate: func(fixture *releaseFixture) {
				fixture.createAllowedMergeCommit()
			},
			wantErr: "merges are not allowed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReleaseFixture(t)
			test.mutate(fixture)
			_, err := validateReleaseEvidenceAt(
				context.Background(), fixture.root, fixture.manifestPath, expectedReleaseTag, testNow,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateReleaseEvidenceAt() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestRunRequiresFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runInRepository(t.TempDir(), nil, &stdout, &stderr); code != 2 {
		t.Fatalf("runInRepository() = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "-manifest and -tag are required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunValidatesReleaseArtifacts(t *testing.T) {
	fixture := newReleaseFixture(t)
	artifacts := t.TempDir()
	for _, arch := range []string{"amd64", "arm64"} {
		path := filepath.Join(artifacts, "remnanode-lite_linux_"+arch)
		if err := os.WriteFile(path, testNodeBinary(arch), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-manifest", fixture.manifestPath, "-tag", expectedReleaseTag, "-artifacts", artifacts}
	var stdout, stderr bytes.Buffer
	if code := runInRepository(fixture.root, args, &stdout, &stderr); code != 0 {
		t.Fatalf("runInRepository() = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), fixture.candidate) || !strings.Contains(stdout.String(), fixture.candidateImageDigest) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	if err := os.WriteFile(filepath.Join(artifacts, "remnanode-lite_linux_arm64"), []byte("wrong"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runInRepository(fixture.root, args, &stdout, &stderr); code != 1 {
		t.Fatalf("runInRepository() with wrong artifact = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "arm64 artifact SHA-256") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func newReleaseFixture(t *testing.T) *releaseFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for release evidence tests")
	}
	fixture := &releaseFixture{
		t:                    t,
		root:                 t.TempDir(),
		candidateImageDigest: "sha256:" + strings.Repeat("a", 64),
		manifestPath:         manifestRepositoryPath,
	}
	fixture.git("init", "-q")
	fixture.git("config", "user.name", "Release Test")
	fixture.git("config", "user.email", "release@example.invalid")
	fixture.git("config", "commit.gpgsign", "false")
	fixture.writeFile("code.txt", []byte("candidate code\n"))
	fixture.writeFile(expectedComposeSourcePath, []byte("services:\n  remnanode-lite:\n    image: candidate\n"))
	fixture.commitAllAt("candidate", testCandidateAt)
	fixture.candidate = fixture.git("rev-parse", "HEAD")
	fixture.candidateTree = fixture.git("rev-parse", fixture.candidate+"^{tree}")

	composeSHA := fixture.fileSHA(expectedComposeSourcePath)
	smoke := validDockerSmokeEvidence(fixture.candidate, fixture.candidateImageDigest, testNodeSHA("amd64"), composeSHA)
	smoke.StartedAt = testStartedAt.Format(time.RFC3339)
	smoke.FinishedAt = testFinishedAt.Format(time.RFC3339)
	smoke.Container.StartedAt = smoke.StartedAt
	fixture.writeJSON(acceptanceDirectory+"/docker-smoke.json", smoke)

	manifest := acceptanceManifest{
		SchemaVersion:        expectedSchemaVersion,
		AcceptanceProfile:    expectedAcceptanceProfile,
		ReleaseVersion:       expectedReleaseVersion,
		ReleaseTag:           expectedReleaseTag,
		CandidateCommit:      fixture.candidate,
		CandidateTree:        fixture.candidateTree,
		CandidateImageDigest: fixture.candidateImageDigest,
		CandidateNodeSHA256: architectureSHAs{
			AMD64: testNodeSHA("amd64"), ARM64: testNodeSHA("arm64"),
		},
		AcceptedAt: testAcceptedAt.Format(time.RFC3339),
		Decision:   "pass",
		OfficialNode: officialNodeTarget{
			Version: expectedOfficialNodeVersion, Commit: expectedOfficialNodeCommit,
		},
		PanelTarget: panelTarget{Version: expectedPanelVersion},
		RWCore: rwCoreTarget{
			Version: expectedRWCoreVersion,
			Commit:  expectedRWCoreCommit,
			SHA256: architectureSHAs{
				AMD64: expectedAMD64AssetSHA, ARM64: expectedARM64AssetSHA,
			},
		},
		DeferredValidation: append([]string(nil), expectedDeferredValidation...),
		Evidence: []evidenceReference{{
			Kind: "docker-production-smoke", Path: acceptanceDirectory + "/docker-smoke.json",
			SHA256: fixture.fileSHA(acceptanceDirectory + "/docker-smoke.json"), Status: "pass",
		}},
		Risks: []releaseRisk{},
	}
	fixture.writeManifest(manifest)
	fixture.commitAllAt("record Docker production smoke acceptance", testFinalAt)
	return fixture
}

func testNodeBinary(arch string) []byte {
	return []byte("remnanode-lite test binary " + arch + "\n")
}

func testNodeSHA(arch string) string {
	digest := sha256.Sum256(testNodeBinary(arch))
	return hex.EncodeToString(digest[:])
}

func (fixture *releaseFixture) mutateSmoke(mutate func(*dockerSmokeEvidence)) {
	fixture.t.Helper()
	path := acceptanceDirectory + "/docker-smoke.json"
	var evidence dockerSmokeEvidence
	fixture.readJSON(path, &evidence)
	mutate(&evidence)
	fixture.writeJSON(path, evidence)
	fixture.updateEvidenceHash()
	fixture.amendAll()
}

func (fixture *releaseFixture) updateEvidenceHash() {
	fixture.t.Helper()
	manifest := fixture.readManifest()
	manifest.Evidence[0].SHA256 = fixture.fileSHA(acceptanceDirectory + "/docker-smoke.json")
	fixture.writeManifest(manifest)
}

func (fixture *releaseFixture) readManifest() acceptanceManifest {
	fixture.t.Helper()
	var manifest acceptanceManifest
	fixture.readJSON(manifestRepositoryPath, &manifest)
	return manifest
}

func (fixture *releaseFixture) writeManifest(manifest acceptanceManifest) {
	fixture.t.Helper()
	fixture.writeJSON(manifestRepositoryPath, manifest)
}

func (fixture *releaseFixture) readJSON(path string, target any) {
	fixture.t.Helper()
	if err := json.Unmarshal(fixture.readFile(path), target); err != nil {
		fixture.t.Fatalf("decode %s: %v", path, err)
	}
}

func (fixture *releaseFixture) writeJSON(path string, value any) {
	fixture.t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fixture.t.Fatalf("encode %s: %v", path, err)
	}
	fixture.writeFile(path, append(raw, '\n'))
}

func (fixture *releaseFixture) readFile(path string) []byte {
	fixture.t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(path)))
	if err != nil {
		fixture.t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func (fixture *releaseFixture) writeFile(path string, raw []byte) {
	fixture.t.Helper()
	abs := filepath.Join(fixture.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		fixture.t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(abs, raw, 0o644); err != nil {
		fixture.t.Fatalf("write %s: %v", path, err)
	}
}

func (fixture *releaseFixture) fileSHA(path string) string {
	fixture.t.Helper()
	digest := sha256.Sum256(fixture.readFile(path))
	return hex.EncodeToString(digest[:])
}

func (fixture *releaseFixture) commitAllAt(message string, committedAt time.Time) {
	fixture.t.Helper()
	fixture.git("add", "--all")
	fixture.gitWithEnv(
		[]string{"GIT_AUTHOR_DATE=" + committedAt.Format(time.RFC3339), "GIT_COMMITTER_DATE=" + committedAt.Format(time.RFC3339)},
		"commit", "-qm", message,
	)
}

func (fixture *releaseFixture) amendAll() {
	fixture.t.Helper()
	fixture.git("add", "--all")
	fixture.gitWithEnv(
		[]string{"GIT_COMMITTER_DATE=" + testFinalAt.Format(time.RFC3339)},
		"commit", "-q", "--amend", "--no-edit",
	)
}

func (fixture *releaseFixture) makeIndexConflict(path string) {
	fixture.t.Helper()
	blob := fixture.git("rev-parse", "HEAD:"+path)
	fixture.git("update-index", "--force-remove", "--", path)
	fixture.gitWithInput(
		"100644 "+blob+" 1\t"+path+"\n100644 "+blob+" 2\t"+path+"\n",
		"update-index", "--index-info",
	)
}

func (fixture *releaseFixture) replaceAcceptanceDirectoryWithSymlink() {
	fixture.t.Helper()
	original := filepath.Join(fixture.root, filepath.FromSlash(acceptanceDirectory))
	destination := filepath.Join(fixture.t.TempDir(), filepath.Base(acceptanceDirectory))
	if err := os.Rename(original, destination); err != nil {
		fixture.t.Fatalf("move acceptance directory: %v", err)
	}
	if err := os.Symlink(destination, original); err != nil {
		fixture.t.Fatalf("symlink acceptance directory: %v", err)
	}
}

func (fixture *releaseFixture) createNonAncestorCandidate() (string, string) {
	fixture.t.Helper()
	baseBranch := fixture.git("branch", "--show-current")
	fixture.git("checkout", "-q", "--orphan", "unrelated-candidate")
	fixture.git("rm", "-qrf", ".")
	fixture.writeFile("unrelated.txt", []byte("unrelated candidate\n"))
	fixture.commitAllAt("unrelated candidate", testCandidateAt)
	commit := fixture.git("rev-parse", "HEAD")
	tree := fixture.git("rev-parse", "HEAD^{tree}")
	fixture.git("checkout", "-q", baseBranch)
	return commit, tree
}

func (fixture *releaseFixture) createAllowedMergeCommit() {
	fixture.t.Helper()
	baseBranch := fixture.git("branch", "--show-current")
	fixture.git("checkout", "-qb", "release-side")
	fixture.writeFile("README.md", []byte("side\n"))
	fixture.commitAllAt("allowed side", testFinalAt.Add(time.Minute))
	fixture.git("checkout", "-q", baseBranch)
	fixture.writeFile("README.zh-CN.md", []byte("base\n"))
	fixture.commitAllAt("allowed base", testFinalAt.Add(2*time.Minute))
	fixture.gitWithEnv(
		[]string{"GIT_AUTHOR_DATE=" + testFinalAt.Add(3*time.Minute).Format(time.RFC3339), "GIT_COMMITTER_DATE=" + testFinalAt.Add(3*time.Minute).Format(time.RFC3339)},
		"merge", "--no-ff", "-qm", "merge allowed finalization", "release-side",
	)
}

func (fixture *releaseFixture) isDirty() bool {
	return fixture.git("status", "--porcelain", "--untracked-files=all") != ""
}

func (fixture *releaseFixture) git(args ...string) string {
	fixture.t.Helper()
	return fixture.gitWithEnv(nil, args...)
}

func (fixture *releaseFixture) gitWithEnv(environment []string, args ...string) string {
	fixture.t.Helper()
	command := exec.Command("git", append([]string{"-C", fixture.root}, args...)...)
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *releaseFixture) gitWithInput(input string, args ...string) string {
	fixture.t.Helper()
	command := exec.Command("git", append([]string{"-C", fixture.root}, args...)...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func appendJSONField(raw []byte, field string) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		panic("JSON fixture is not an object")
	}
	result := append([]byte(nil), trimmed[:len(trimmed)-1]...)
	result = append(result, ',')
	result = append(result, field...)
	result = append(result, '}', '\n')
	return result
}

func boolPointer(value bool) *bool    { return &value }
func intPointer(value int) *int       { return &value }
func int64Pointer(value int64) *int64 { return &value }
