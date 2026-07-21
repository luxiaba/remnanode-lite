package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	expectedSchemaVersion     = 2
	expectedAcceptanceProfile = "docker-production-smoke-v2"

	expectedOfficialNodeVersion = "2.8.0"
	expectedOfficialNodeCommit  = "596f015a5c8f876dc9a9d61b6cb78d35bd8e379b"
	expectedPanelVersion        = "2.8.1"
	expectedReleaseVersion      = "2.8.0"
	expectedReleaseTag          = "v" + expectedReleaseVersion
	expectedVersionOutput       = "remnanode-lite " + expectedReleaseVersion + " (contract " + expectedOfficialNodeVersion + ")"

	expectedRWCoreVersion = "v26.6.27"
	expectedRWCoreCommit  = "45cf2898ab12e97a55dd8f1f3d78d903340bdc9e"
	expectedAMD64AssetSHA = "b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf"
	expectedARM64AssetSHA = "13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931"

	maximumClockSkew           = 5 * time.Minute
	maximumAcceptanceFileBytes = 1 << 20

	acceptanceDirectory       = "docs/development/acceptance/v" + expectedReleaseVersion
	manifestRepositoryPath    = acceptanceDirectory + "/manifest.json"
	releaseNoteRepositoryPath = "docs/releases/v" + expectedReleaseVersion + ".md"
)

var expectedDeferredValidation = []string{
	"whole-host-512mib-runtime",
	"arm64-production-runtime",
	"native-systemd-install",
	"native-openrc-install",
	"50000-user-load",
	"24h-soak",
	"fault-and-rollback-injection",
}

var expectedAcceptancePaths = []string{
	manifestRepositoryPath,
	acceptanceDirectory + "/docker-smoke.json",
}

type validationResult struct {
	ReleaseTag           string
	CandidateCommit      string
	CandidateImageDigest string
	NodeSHA256ByArch     map[string]string
}

type acceptanceManifest struct {
	SchemaVersion        int                 `json:"schemaVersion"`
	AcceptanceProfile    string              `json:"acceptanceProfile"`
	ReleaseVersion       string              `json:"releaseVersion"`
	ReleaseTag           string              `json:"releaseTag"`
	CandidateCommit      string              `json:"candidateCommit"`
	CandidateTree        string              `json:"candidateTree"`
	CandidateImageDigest string              `json:"candidateImageDigest"`
	CandidateNodeSHA256  architectureSHAs    `json:"candidateNodeSha256"`
	AcceptedAt           string              `json:"acceptedAt"`
	Decision             string              `json:"decision"`
	OfficialNode         officialNodeTarget  `json:"officialNode"`
	PanelTarget          panelTarget         `json:"panelTarget"`
	RWCore               rwCoreTarget        `json:"rwCore"`
	DeferredValidation   []string            `json:"deferredValidation"`
	Evidence             []evidenceReference `json:"evidence"`
	Risks                []releaseRisk       `json:"risks"`
}

type officialNodeTarget struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type panelTarget struct {
	Version string `json:"version"`
}

type rwCoreTarget struct {
	Version string           `json:"version"`
	Commit  string           `json:"commit"`
	SHA256  architectureSHAs `json:"sha256"`
}

type architectureSHAs struct {
	AMD64 string `json:"amd64"`
	ARM64 string `json:"arm64"`
}

type evidenceReference struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
}

type releaseRisk struct {
	ID              string `json:"id"`
	Severity        string `json:"severity"`
	Status          string `json:"status"`
	Summary         string `json:"summary"`
	Mitigation      string `json:"mitigation"`
	ReleaseBlocking *bool  `json:"releaseBlocking"`
}

type evidenceCommon struct {
	SchemaVersion   int      `json:"schemaVersion"`
	Kind            string   `json:"kind"`
	CandidateCommit string   `json:"candidateCommit"`
	Status          string   `json:"status"`
	StartedAt       string   `json:"startedAt"`
	FinishedAt      string   `json:"finishedAt"`
	Command         []string `json:"command"`
}

type gitRepository struct {
	root string
}

type evidenceTiming struct {
	Started  time.Time
	Finished time.Time
}

type validatedEvidence struct {
	Timing evidenceTiming
	Smoke  *dockerSmokeEvidence
}

func validateReleaseEvidence(ctx context.Context, repoDir, manifestPath, releaseTag string) (validationResult, error) {
	return validateReleaseEvidenceAt(ctx, repoDir, manifestPath, releaseTag, time.Now().UTC())
}

func validateReleaseEvidenceAt(
	ctx context.Context,
	repoDir, manifestPath, releaseTag string,
	now time.Time,
) (validationResult, error) {
	if releaseTag != expectedReleaseTag {
		return validationResult{}, fmt.Errorf("tag %q does not match %s", releaseTag, expectedReleaseTag)
	}

	repo, err := openGitRepository(ctx, repoDir)
	if err != nil {
		return validationResult{}, err
	}
	if err := repo.validateAcceptanceFileSet(ctx); err != nil {
		return validationResult{}, err
	}
	manifestAbs, manifestRel, err := repo.resolveRepositoryFile(manifestPath)
	if err != nil {
		return validationResult{}, fmt.Errorf("manifest: %w", err)
	}
	if manifestRel != manifestRepositoryPath {
		return validationResult{}, fmt.Errorf("manifest path is %q, want %q", manifestRel, manifestRepositoryPath)
	}
	manifestRaw, err := repo.requireTrackedRegularFile(ctx, manifestAbs, manifestRel)
	if err != nil {
		return validationResult{}, fmt.Errorf("manifest: %w", err)
	}
	var manifest acceptanceManifest
	if err := decodeStrictJSON(manifestRaw, &manifest); err != nil {
		return validationResult{}, fmt.Errorf("decode manifest: %w", err)
	}
	headTime, err := repo.commitTime(ctx, "HEAD")
	if err != nil {
		return validationResult{}, fmt.Errorf("read HEAD commit time: %w", err)
	}
	acceptedAt, err := validateManifest(&manifest, releaseTag, now, headTime)
	if err != nil {
		return validationResult{}, err
	}

	candidateTime, err := repo.validateCandidate(ctx, manifest.CandidateCommit, manifest.CandidateTree)
	if err != nil {
		return validationResult{}, err
	}
	postCandidateCommits, err := repo.validatePostCandidateChanges(ctx, manifest.CandidateCommit)
	if err != nil {
		return validationResult{}, err
	}

	evidenceByKind := make(map[string]validatedEvidence, len(manifest.Evidence))
	latestEvidenceFinish := time.Time{}
	seenKinds := make(map[string]struct{}, len(manifest.Evidence))
	for _, reference := range manifest.Evidence {
		if _, duplicate := seenKinds[reference.Kind]; duplicate {
			return validationResult{}, fmt.Errorf("duplicate evidence kind %q", reference.Kind)
		}
		seenKinds[reference.Kind] = struct{}{}
		validated, err := repo.validateEvidence(
			ctx,
			reference,
			manifest.CandidateCommit,
			manifest.CandidateImageDigest,
			manifest.CandidateNodeSHA256,
			candidateTime,
			now,
			headTime,
		)
		if err != nil {
			return validationResult{}, fmt.Errorf("evidence %s: %w", reference.Kind, err)
		}
		evidenceByKind[reference.Kind] = validated
		if validated.Timing.Finished.After(latestEvidenceFinish) {
			latestEvidenceFinish = validated.Timing.Finished
		}
	}

	if evidenceByKind["docker-production-smoke"].Smoke == nil {
		return validationResult{}, errors.New("missing validated docker-production-smoke evidence")
	}
	if acceptedAt.Before(latestEvidenceFinish) {
		return validationResult{}, fmt.Errorf("acceptedAt %s is before evidence completion %s", acceptedAt.Format(time.RFC3339), latestEvidenceFinish.Format(time.RFC3339))
	}
	if postCandidateCommits != 1 {
		return validationResult{}, fmt.Errorf("release finalization must contain exactly one single-parent commit after the candidate, found %d", postCandidateCommits)
	}

	nodeSHAByArch := map[string]string{
		"amd64": manifest.CandidateNodeSHA256.AMD64,
		"arm64": manifest.CandidateNodeSHA256.ARM64,
	}
	return validationResult{
		ReleaseTag:           releaseTag,
		CandidateCommit:      manifest.CandidateCommit,
		CandidateImageDigest: manifest.CandidateImageDigest,
		NodeSHA256ByArch:     nodeSHAByArch,
	}, nil
}

func validateReleaseArtifacts(directory string, result validationResult) error {
	for _, arch := range []string{"amd64", "arm64"} {
		expectedSHA, ok := result.NodeSHA256ByArch[arch]
		if !ok {
			return fmt.Errorf("evidence has no Node SHA-256 for %s", arch)
		}
		path := filepath.Join(directory, "remnanode-lite_linux_"+arch)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat %s artifact: %w", arch, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s artifact %s is not a regular file", arch, path)
		}
		actualSHA, err := sha256File(path)
		if err != nil {
			return fmt.Errorf("hash %s artifact: %w", arch, err)
		}
		if actualSHA != expectedSHA {
			return fmt.Errorf("%s artifact SHA-256=%s, want %s", arch, actualSHA, expectedSHA)
		}
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validateManifest(manifest *acceptanceManifest, releaseTag string, now, headTime time.Time) (time.Time, error) {
	if manifest.SchemaVersion != expectedSchemaVersion {
		return time.Time{}, fmt.Errorf("manifest schemaVersion=%d, want %d", manifest.SchemaVersion, expectedSchemaVersion)
	}
	if manifest.AcceptanceProfile != expectedAcceptanceProfile {
		return time.Time{}, fmt.Errorf("manifest acceptanceProfile=%q, want %q", manifest.AcceptanceProfile, expectedAcceptanceProfile)
	}
	if manifest.ReleaseVersion != expectedReleaseVersion || manifest.ReleaseTag != releaseTag {
		return time.Time{}, fmt.Errorf("manifest release=%q tag=%q, want %s and %s", manifest.ReleaseVersion, manifest.ReleaseTag, expectedReleaseVersion, releaseTag)
	}
	if !isLowerHex(manifest.CandidateCommit, 40) {
		return time.Time{}, errors.New("manifest candidateCommit must be 40 lowercase hexadecimal characters")
	}
	if !isLowerHex(manifest.CandidateTree, 40) {
		return time.Time{}, errors.New("manifest candidateTree must be 40 lowercase hexadecimal characters")
	}
	if !isSHA256Digest(manifest.CandidateImageDigest) {
		return time.Time{}, errors.New("manifest candidateImageDigest must be sha256: followed by 64 lowercase hexadecimal characters")
	}
	if !isLowerHex(manifest.CandidateNodeSHA256.AMD64, 64) || !isLowerHex(manifest.CandidateNodeSHA256.ARM64, 64) {
		return time.Time{}, errors.New("manifest candidateNodeSha256 must contain amd64 and arm64 64-character lowercase hexadecimal digests")
	}
	acceptedAt, err := parseRFC3339("manifest acceptedAt", manifest.AcceptedAt)
	if err != nil {
		return time.Time{}, err
	}
	if err := validateRecordedTime("manifest acceptedAt", acceptedAt, now, headTime); err != nil {
		return time.Time{}, err
	}
	if manifest.Decision != "pass" {
		return time.Time{}, fmt.Errorf("manifest decision=%q, want pass", manifest.Decision)
	}
	if manifest.OfficialNode.Version != expectedOfficialNodeVersion || manifest.OfficialNode.Commit != expectedOfficialNodeCommit {
		return time.Time{}, fmt.Errorf("official Node must be %s@%s", expectedOfficialNodeVersion, expectedOfficialNodeCommit)
	}
	if manifest.PanelTarget.Version != expectedPanelVersion {
		return time.Time{}, fmt.Errorf("Panel target=%q, want %s", manifest.PanelTarget.Version, expectedPanelVersion)
	}
	if manifest.RWCore.Version != expectedRWCoreVersion || manifest.RWCore.Commit != expectedRWCoreCommit {
		return time.Time{}, fmt.Errorf("rw-core must be %s@%s", expectedRWCoreVersion, expectedRWCoreCommit)
	}
	if manifest.RWCore.SHA256.AMD64 != expectedAMD64AssetSHA || manifest.RWCore.SHA256.ARM64 != expectedARM64AssetSHA {
		return time.Time{}, errors.New("rw-core architecture SHA-256 pins do not match the audited assets")
	}
	if !reflect.DeepEqual(manifest.DeferredValidation, expectedDeferredValidation) {
		return time.Time{}, fmt.Errorf("manifest deferredValidation=%v, want %v", manifest.DeferredValidation, expectedDeferredValidation)
	}
	if len(manifest.Evidence) != 1 {
		return time.Time{}, fmt.Errorf("manifest evidence count=%d, want 1", len(manifest.Evidence))
	}
	if manifest.Risks == nil {
		return time.Time{}, errors.New("manifest risks must be an array, use [] when there are no risks")
	}
	for _, reference := range manifest.Evidence {
		if reference.Kind != "docker-production-smoke" {
			return time.Time{}, fmt.Errorf("unsupported evidence kind %q", reference.Kind)
		}
		if reference.Status != "pass" {
			return time.Time{}, fmt.Errorf("evidence %s status=%q, want pass", reference.Kind, reference.Status)
		}
		expectedPath := acceptanceDirectory + "/docker-smoke.json"
		if reference.Path != expectedPath {
			return time.Time{}, fmt.Errorf("evidence %s path=%q, want %q", reference.Kind, reference.Path, expectedPath)
		}
		if !isLowerHex(reference.SHA256, 64) {
			return time.Time{}, fmt.Errorf("evidence %s SHA-256 must be 64 lowercase hexadecimal characters", reference.Kind)
		}
	}
	if err := validateRisks(manifest.Risks); err != nil {
		return time.Time{}, err
	}
	return acceptedAt, nil
}

func validateRisks(risks []releaseRisk) error {
	seen := make(map[string]struct{}, len(risks))
	for _, risk := range risks {
		if strings.TrimSpace(risk.ID) == "" {
			return errors.New("risk id must not be empty")
		}
		if _, duplicate := seen[risk.ID]; duplicate {
			return fmt.Errorf("duplicate risk id %q", risk.ID)
		}
		seen[risk.ID] = struct{}{}
		if risk.Severity != "P1" && risk.Severity != "P2" && risk.Severity != "P3" {
			return fmt.Errorf("risk %s has unsupported severity %q", risk.ID, risk.Severity)
		}
		if risk.Status != "open" && risk.Status != "closed" {
			return fmt.Errorf("risk %s has unsupported status %q", risk.ID, risk.Status)
		}
		if strings.TrimSpace(risk.Summary) == "" || strings.TrimSpace(risk.Mitigation) == "" {
			return fmt.Errorf("risk %s summary and mitigation must not be empty", risk.ID)
		}
		if risk.ReleaseBlocking == nil {
			return fmt.Errorf("risk %s releaseBlocking is required", risk.ID)
		}
		if *risk.ReleaseBlocking {
			return fmt.Errorf("risk %s is release-blocking", risk.ID)
		}
		if (risk.Severity == "P1" || risk.Severity == "P2") && risk.Status != "closed" {
			return fmt.Errorf("risk %s is an unclosed %s", risk.ID, risk.Severity)
		}
	}
	return nil
}

func (repo gitRepository) validateEvidence(
	ctx context.Context,
	reference evidenceReference,
	candidateCommit, candidateImageDigest string,
	candidateNodeSHA256 architectureSHAs,
	candidateTime time.Time,
	now time.Time,
	headTime time.Time,
) (validatedEvidence, error) {
	abs, rel, err := repo.resolveRepositoryFile(reference.Path)
	if err != nil {
		return validatedEvidence{}, err
	}
	if rel != reference.Path {
		return validatedEvidence{}, fmt.Errorf("path resolves to %q", rel)
	}
	raw, err := repo.requireTrackedRegularFile(ctx, abs, rel)
	if err != nil {
		return validatedEvidence{}, err
	}
	digest := sha256.Sum256(raw)
	if got := hex.EncodeToString(digest[:]); got != reference.SHA256 {
		return validatedEvidence{}, fmt.Errorf("SHA-256=%s, want %s", got, reference.SHA256)
	}

	var discriminator struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &discriminator); err != nil {
		return validatedEvidence{}, fmt.Errorf("decode kind: %w", err)
	}
	if discriminator.Kind != reference.Kind {
		return validatedEvidence{}, fmt.Errorf("kind=%q, want %q", discriminator.Kind, reference.Kind)
	}

	switch reference.Kind {
	case "docker-production-smoke":
		var evidence dockerSmokeEvidence
		if err := decodeStrictJSON(raw, &evidence); err != nil {
			return validatedEvidence{}, fmt.Errorf("decode: %w", err)
		}
		timing, err := validateCommonEvidence(evidence.evidenceCommon, reference.Kind, candidateCommit, candidateTime, now, headTime)
		if err != nil {
			return validatedEvidence{}, err
		}
		candidateComposeSHA, err := repo.candidateFileSHA256(ctx, candidateCommit, expectedComposeSourcePath)
		if err != nil {
			return validatedEvidence{}, err
		}
		if err := validateDockerSmokeEvidence(
			evidence,
			timing,
			candidateImageDigest,
			candidateNodeSHA256.AMD64,
			candidateComposeSHA,
		); err != nil {
			return validatedEvidence{}, err
		}
		return validatedEvidence{Timing: timing, Smoke: &evidence}, nil
	default:
		return validatedEvidence{}, fmt.Errorf("unsupported kind %q", reference.Kind)
	}
}

func validateCommonEvidence(
	common evidenceCommon,
	kind, candidateCommit string,
	candidateTime, now, headTime time.Time,
) (evidenceTiming, error) {
	if common.SchemaVersion != expectedSchemaVersion {
		return evidenceTiming{}, fmt.Errorf("schemaVersion=%d, want %d", common.SchemaVersion, expectedSchemaVersion)
	}
	if common.Kind != kind {
		return evidenceTiming{}, fmt.Errorf("kind=%q, want %q", common.Kind, kind)
	}
	if common.CandidateCommit != candidateCommit {
		return evidenceTiming{}, fmt.Errorf("candidateCommit=%q, want %s", common.CandidateCommit, candidateCommit)
	}
	if common.Status != "pass" {
		return evidenceTiming{}, fmt.Errorf("status=%q, want pass", common.Status)
	}
	started, err := parseRFC3339(kind+" startedAt", common.StartedAt)
	if err != nil {
		return evidenceTiming{}, err
	}
	if started.Before(candidateTime) {
		return evidenceTiming{}, fmt.Errorf("startedAt predates candidate commit: %s < %s", started.Format(time.RFC3339), candidateTime.Format(time.RFC3339))
	}
	if err := validateRecordedTime(kind+" startedAt", started, now, headTime); err != nil {
		return evidenceTiming{}, err
	}
	finished, err := parseRFC3339(kind+" finishedAt", common.FinishedAt)
	if err != nil {
		return evidenceTiming{}, err
	}
	if finished.Before(started) {
		return evidenceTiming{}, fmt.Errorf("finishedAt %s is before startedAt %s", common.FinishedAt, common.StartedAt)
	}
	if err := validateRecordedTime(kind+" finishedAt", finished, now, headTime); err != nil {
		return evidenceTiming{}, err
	}
	if len(common.Command) == 0 {
		return evidenceTiming{}, errors.New("command must not be empty")
	}
	for _, argument := range common.Command {
		if strings.TrimSpace(argument) == "" {
			return evidenceTiming{}, errors.New("command arguments must not be empty")
		}
		if !isSafeEvidenceCommandArgument(argument) {
			return evidenceTiming{}, errors.New("command contains a potentially sensitive argument")
		}
	}
	return evidenceTiming{Started: started, Finished: finished}, nil
}

func openGitRepository(ctx context.Context, repoDir string) (gitRepository, error) {
	if repoDir == "" {
		repoDir = "."
	}
	command := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--show-toplevel")
	output, err := command.CombinedOutput()
	if err != nil {
		return gitRepository{}, fmt.Errorf("find Git repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return gitRepository{}, errors.New("find Git repository: empty root")
	}
	return gitRepository{root: root}, nil
}

func (repo gitRepository) validateAcceptanceFileSet(ctx context.Context) error {
	raw, err := repo.gitBytes(ctx, "ls-tree", "-r", "-z", "--name-only", "HEAD", "--", acceptanceDirectory)
	if err != nil {
		return fmt.Errorf("list acceptance files in HEAD: %w", err)
	}
	actual := splitNUL(raw)
	actualSet := make(map[string]struct{}, len(actual))
	for _, path := range actual {
		actualSet[path] = struct{}{}
	}
	if !sameStringSet(actualSet, expectedAcceptancePaths) {
		sort.Strings(actual)
		return fmt.Errorf("acceptance files in HEAD must be exactly %v, got %v", expectedAcceptancePaths, actual)
	}
	return nil
}

func (repo gitRepository) validateCandidate(ctx context.Context, candidateCommit, candidateTree string) (time.Time, error) {
	commit, err := repo.gitOutput(ctx, "rev-parse", "--verify", candidateCommit+"^{commit}")
	if err != nil {
		return time.Time{}, fmt.Errorf("verify candidate commit: %w", err)
	}
	if commit != candidateCommit {
		return time.Time{}, fmt.Errorf("candidate commit resolves to %s, want %s", commit, candidateCommit)
	}
	tree, err := repo.gitOutput(ctx, "rev-parse", candidateCommit+"^{tree}")
	if err != nil {
		return time.Time{}, fmt.Errorf("read candidate tree: %w", err)
	}
	if tree != candidateTree {
		return time.Time{}, fmt.Errorf("candidate tree=%s, want %s", tree, candidateTree)
	}
	if _, err := repo.gitOutput(ctx, "merge-base", "--is-ancestor", candidateCommit, "HEAD"); err != nil {
		return time.Time{}, fmt.Errorf("candidate %s is not an ancestor of HEAD: %w", candidateCommit, err)
	}
	committedAt, err := repo.commitTime(ctx, candidateCommit)
	if err != nil {
		return time.Time{}, fmt.Errorf("read candidate commit time: %w", err)
	}
	return committedAt, nil
}

func (repo gitRepository) candidateFileSHA256(ctx context.Context, candidateCommit, path string) (string, error) {
	raw, err := repo.gitBytes(ctx, "show", candidateCommit+":"+path)
	if err != nil {
		return "", fmt.Errorf("read %s from candidate commit: %w", path, err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (repo gitRepository) validatePostCandidateChanges(ctx context.Context, candidateCommit string) (int, error) {
	commitsRaw, err := repo.gitOutput(ctx, "rev-list", "--reverse", candidateCommit+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("list post-candidate commits: %w", err)
	}
	commits := strings.Fields(commitsRaw)
	for _, commit := range commits {
		parentsRaw, err := repo.gitOutput(ctx, "show", "-s", "--format=%P", commit)
		if err != nil {
			return 0, fmt.Errorf("read parents for post-candidate commit %s: %w", commit, err)
		}
		parents := strings.Fields(parentsRaw)
		if len(parents) != 1 {
			return 0, fmt.Errorf("post-candidate commit %s is a merge with %d parents; merges are not allowed during release finalization", commit, len(parents))
		}
		changedRaw, err := repo.gitBytes(
			ctx,
			"diff",
			"--no-renames",
			"--name-only",
			"-z",
			parents[0],
			commit,
			"--",
		)
		if err != nil {
			return 0, fmt.Errorf("list changes for post-candidate commit %s: %w", commit, err)
		}
		for _, path := range splitNUL(changedRaw) {
			if !isAllowedPostCandidatePath(path) {
				return 0, fmt.Errorf("post-candidate commit %s changes %q outside the release-finalization allowlist", commit, path)
			}
		}
	}
	return len(commits), nil
}

func isAllowedPostCandidatePath(path string) bool {
	switch path {
	case "README.md",
		"README.ru.md",
		"README.zh-CN.md",
		"CHANGELOG.md",
		"docs/development/roadmap.md",
		"docs/i18n/zh-CN/development/roadmap.md",
		releaseNoteRepositoryPath,
		manifestRepositoryPath,
		acceptanceDirectory + "/docker-smoke.json":
		return true
	default:
		return false
	}
}

func (repo gitRepository) resolveRepositoryFile(path string) (string, string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(repo.root, filepath.FromSlash(path))
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(repo.root, abs)
	if err != nil {
		return "", "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("path %q is outside repository", path)
	}
	return abs, rel, nil
}

func (repo gitRepository) requireTrackedRegularFile(ctx context.Context, abs, rel string) ([]byte, error) {
	if err := repo.requireRegularPathWithoutSymlinks(abs, rel); err != nil {
		return nil, err
	}

	headRaw, err := repo.gitBytes(ctx, "ls-tree", "-z", "HEAD", "--", rel)
	if err != nil {
		return nil, fmt.Errorf("read %s from HEAD tree: %w", rel, err)
	}
	headEntry, err := parseHEADTreeEntry(headRaw, rel)
	if err != nil {
		return nil, err
	}
	if headEntry.Mode != "100644" {
		return nil, fmt.Errorf("%s has HEAD mode %s, want 100644", rel, headEntry.Mode)
	}
	if headEntry.Type != "blob" {
		return nil, fmt.Errorf("%s has HEAD type %s, want blob", rel, headEntry.Type)
	}

	indexRaw, err := repo.gitBytes(ctx, "ls-files", "--stage", "-z", "--", rel)
	if err != nil {
		return nil, fmt.Errorf("read %s from Git index: %w", rel, err)
	}
	indexEntry, err := parseIndexEntry(indexRaw, rel)
	if err != nil {
		return nil, err
	}
	if indexEntry.Stage != "0" {
		return nil, fmt.Errorf("%s has non-stage-0 index entry %s", rel, indexEntry.Stage)
	}
	if strings.Trim(indexEntry.Object, "0") == "" {
		return nil, fmt.Errorf("%s is intent-to-add in the Git index", rel)
	}
	if indexEntry.Mode != headEntry.Mode || indexEntry.Object != headEntry.Object {
		return nil, fmt.Errorf("%s index entry does not match HEAD", rel)
	}

	worktreeInfo, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s worktree file: %w", rel, err)
	}
	if worktreeInfo.Mode().Perm()&0o111 != 0 {
		return nil, fmt.Errorf("%s worktree mode is executable, want non-executable JSON", rel)
	}
	if worktreeInfo.Size() > maximumAcceptanceFileBytes {
		return nil, fmt.Errorf("%s worktree size %d exceeds %d bytes", rel, worktreeInfo.Size(), maximumAcceptanceFileBytes)
	}
	headSizeRaw, err := repo.gitOutput(ctx, "cat-file", "-s", headEntry.Object)
	if err != nil {
		return nil, fmt.Errorf("read %s HEAD blob size: %w", rel, err)
	}
	headSize, err := strconv.ParseInt(headSizeRaw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse %s HEAD blob size %q: %w", rel, headSizeRaw, err)
	}
	if headSize > maximumAcceptanceFileBytes {
		return nil, fmt.Errorf("%s HEAD blob size %d exceeds %d bytes", rel, headSize, maximumAcceptanceFileBytes)
	}

	worktreeRaw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s from worktree: %w", rel, err)
	}
	headBlob, err := repo.gitBytes(ctx, "cat-file", "blob", headEntry.Object)
	if err != nil {
		return nil, fmt.Errorf("read %s HEAD blob: %w", rel, err)
	}
	if !bytes.Equal(worktreeRaw, headBlob) {
		return nil, fmt.Errorf("%s worktree bytes do not match HEAD blob", rel)
	}
	return worktreeRaw, nil
}

type headTreeEntry struct {
	Mode   string
	Type   string
	Object string
}

type indexEntry struct {
	Mode   string
	Object string
	Stage  string
}

func (repo gitRepository) requireRegularPathWithoutSymlinks(abs, rel string) error {
	expectedAbs, err := filepath.Abs(filepath.Join(repo.root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if filepath.Clean(abs) != expectedAbs {
		return fmt.Errorf("path %q resolves to unexpected location %q", rel, abs)
	}

	current := repo.root
	parts := strings.Split(filepath.FromSlash(rel), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s contains symlink component %s", rel, filepath.ToSlash(current))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("%s ancestor %s is not a directory", rel, filepath.ToSlash(current))
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
	}
	return nil
}

func parseHEADTreeEntry(raw []byte, rel string) (headTreeEntry, error) {
	entries := splitNUL(raw)
	if len(entries) != 1 {
		return headTreeEntry{}, fmt.Errorf("%s must have exactly one entry in HEAD, got %d", rel, len(entries))
	}
	metadata, path, ok := strings.Cut(entries[0], "\t")
	if !ok || path != rel {
		return headTreeEntry{}, fmt.Errorf("malformed HEAD tree entry for %s", rel)
	}
	fields := strings.Fields(metadata)
	if len(fields) != 3 {
		return headTreeEntry{}, fmt.Errorf("malformed HEAD tree metadata for %s", rel)
	}
	return headTreeEntry{Mode: fields[0], Type: fields[1], Object: fields[2]}, nil
}

func parseIndexEntry(raw []byte, rel string) (indexEntry, error) {
	entries := splitNUL(raw)
	if len(entries) != 1 {
		return indexEntry{}, fmt.Errorf("%s must have exactly one Git index entry, got %d", rel, len(entries))
	}
	metadata, path, ok := strings.Cut(entries[0], "\t")
	if !ok || path != rel {
		return indexEntry{}, fmt.Errorf("malformed Git index entry for %s", rel)
	}
	fields := strings.Fields(metadata)
	if len(fields) != 3 {
		return indexEntry{}, fmt.Errorf("malformed Git index metadata for %s", rel)
	}
	return indexEntry{Mode: fields[0], Object: fields[1], Stage: fields[2]}, nil
}

func (repo gitRepository) commitTime(ctx context.Context, revision string) (time.Time, error) {
	raw, err := repo.gitOutput(ctx, "show", "-s", "--format=%cI", revision)
	if err != nil {
		return time.Time{}, err
	}
	return parseRFC3339(revision+" commit time", raw)
}

func (repo gitRepository) gitBytes(ctx context.Context, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", repo.root}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, detail)
	}
	return output, nil
}

func (repo gitRepository) gitOutput(ctx context.Context, args ...string) (string, error) {
	output, err := repo.gitBytes(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func decodeStrictJSON(raw []byte, target any) error {
	if err := rejectDuplicateJSONFields(raw); err != nil {
		return err
	}
	if err := validateExactJSONKeys(raw, target); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func validateExactJSONKeys(raw []byte, target any) error {
	targetType := reflect.TypeOf(target)
	if targetType == nil || targetType.Kind() != reflect.Pointer {
		return errors.New("strict JSON target must be a non-nil pointer")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return validateExactJSONValue(value, targetType.Elem(), "$")
}

func validateExactJSONValue(value any, targetType reflect.Type, path string) error {
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	if value == nil {
		return nil
	}

	switch targetType.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := exactJSONStructFields(targetType)
		for key, member := range object {
			fieldType, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown field %q at %s", key, path)
			}
			if err := validateExactJSONValue(member, fieldType, path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return nil
		}
		for index, member := range array {
			if err := validateExactJSONValue(member, targetType.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for key, member := range object {
			if err := validateExactJSONValue(member, targetType.Elem(), path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func exactJSONStructFields(targetType reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for index := 0; index < targetType.NumField(); index++ {
		field := targetType.Field(index)
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			embeddedType := field.Type
			for embeddedType.Kind() == reflect.Pointer {
				embeddedType = embeddedType.Elem()
			}
			if embeddedType.Kind() == reflect.Struct {
				for embeddedName, embeddedFieldType := range exactJSONStructFields(embeddedType) {
					fields[embeddedName] = embeddedFieldType
				}
				continue
			}
		}
		if field.PkgPath != "" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func rejectDuplicateJSONFields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("trailing JSON: %w", err)
		}
		return fmt.Errorf("multiple JSON values are not allowed, found %v", token)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON field %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object at %s ended with %v", path, closing)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array at %s ended with %v", path, closing)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
	return nil
}

func parseRFC3339(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return parsed, nil
}

func validateRecordedTime(field string, value, now, headTime time.Time) error {
	if value.After(now.Add(maximumClockSkew)) {
		return fmt.Errorf("%s %s is later than current time plus %s", field, value.Format(time.RFC3339), maximumClockSkew)
	}
	if value.After(headTime.Add(maximumClockSkew)) {
		return fmt.Errorf("%s %s is later than HEAD commit time plus %s", field, value.Format(time.RFC3339), maximumClockSkew)
	}
	return nil
}

func isLowerHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isSHA256Digest(value string) bool {
	const prefix = "sha256:"
	return strings.HasPrefix(value, prefix) && isLowerHex(value[len(prefix):], 64)
}

func exactStringSliceSet(values, expected []string) bool {
	if len(values) != len(expected) {
		return false
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, duplicate := set[value]; duplicate {
			return false
		}
		set[value] = struct{}{}
	}
	return sameStringSet(set, expected)
}

func sameStringSet(values map[string]struct{}, expected []string) bool {
	if len(values) != len(expected) {
		return false
	}
	for _, value := range expected {
		if _, ok := values[value]; !ok {
			return false
		}
	}
	return true
}

func splitNUL(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
