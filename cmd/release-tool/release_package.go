package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	projectversion "github.com/luxiaba/remnanode-lite/internal/version"
)

const releaseAssetMaxBytes = 512 << 20

type assembleOptions struct {
	projectRoot     string
	nativeDirectory string
	outputDirectory string
	version         string
}

type verifyPackageOptions struct {
	lockPath            string
	directory           string
	version             string
	contractVersion     string
	sourceRevision      string
	requireReleaseIndex bool
}

type finalizeReleaseOptions struct {
	lockPath        string
	directory       string
	version         string
	contractVersion string
	sourceRevision  string
	image           string
	indexDigest     string
}

type releaseIndex struct {
	SchemaVersion  int    `json:"schema_version"`
	Version        string `json:"version"`
	SourceRevision string `json:"source_revision"`
	Image          string `json:"image"`
	IndexDigest    string `json:"index_digest"`
}

const releaseIndexName = "release-index.json"

func baseReleasePayloadNames(version string) []string {
	return []string{
		"compose.yaml",
		"docker-compose.single-file.yaml",
		"install.sh",
		"remnanode-lite.env.example",
		"remnanode-lite_" + version + "_linux_amd64.tar.gz",
		"remnanode-lite_" + version + "_linux_arm64.tar.gz",
	}
}

func releasePayloadNames(version string) []string {
	names := append([]string(nil), baseReleasePayloadNames(version)...)
	return append(names, releaseIndexName)
}

func baseReleaseAssetNames(version string) []string {
	names := append([]string(nil), baseReleasePayloadNames(version)...)
	names = append(names, "SHA256SUMS")
	sort.Strings(names)
	return names
}

func releaseAssetNames(version string) []string {
	names := append([]string(nil), releasePayloadNames(version)...)
	names = append(names, "SHA256SUMS")
	sort.Strings(names)
	return names
}

func assembleReleasePackage(options assembleOptions) error {
	if err := validateProjectVersion(options.version); err != nil {
		return err
	}
	if options.projectRoot == "" || options.nativeDirectory == "" || options.outputDirectory == "" {
		return fmt.Errorf("assemble requires project root, Native directory, and output directory")
	}
	if _, err := os.Lstat(options.outputDirectory); err == nil {
		return fmt.Errorf("release output already exists: %s", options.outputDirectory)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect release output: %w", err)
	}
	parent := filepath.Dir(options.outputDirectory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create release output parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".release-package-")
	if err != nil {
		return fmt.Errorf("create temporary release output: %w", err)
	}
	defer os.RemoveAll(temporary)

	copyInputs := []struct {
		source string
		name   string
		mode   os.FileMode
	}{
		{source: filepath.Join(options.projectRoot, "release/native/install.sh"), name: "install.sh", mode: 0o755},
		{source: filepath.Join(options.projectRoot, "compose.yaml"), name: "compose.yaml", mode: 0o644},
		{source: filepath.Join(options.projectRoot, ".env.example"), name: "remnanode-lite.env.example", mode: 0o644},
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		name := fmt.Sprintf("remnanode-lite_%s_linux_%s.tar.gz", options.version, architecture)
		copyInputs = append(copyInputs, struct {
			source string
			name   string
			mode   os.FileMode
		}{source: filepath.Join(options.nativeDirectory, name), name: name, mode: 0o644})
	}
	for _, input := range copyInputs {
		data, readErr := readRegularInput(input.source, releaseAssetMaxBytes, input.mode&0o111 != 0)
		if readErr != nil {
			return fmt.Errorf("read release input %s: %w", input.source, readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(temporary, input.name), data, input.mode); writeErr != nil {
			return fmt.Errorf("write release asset %s: %w", input.name, writeErr)
		}
	}

	singleFilePath := filepath.Join(options.projectRoot, "deploy/compose.single-file.yaml")
	singleFile, err := readRegularInput(singleFilePath, maxTextInputBytes, false)
	if err != nil {
		return fmt.Errorf("read single-file Compose template: %w", err)
	}
	const movingImage = "ghcr.io/luxiaba/remnanode-lite:latest"
	exactImage := "ghcr.io/luxiaba/remnanode-lite:" + options.version
	if strings.Count(string(singleFile), movingImage) != 1 {
		return fmt.Errorf("single-file Compose template must contain exactly one %s default", movingImage)
	}
	singleFile = []byte(strings.Replace(string(singleFile), movingImage, exactImage, 1))
	if err := os.WriteFile(filepath.Join(temporary, "docker-compose.single-file.yaml"), singleFile, 0o644); err != nil {
		return fmt.Errorf("write single-file Compose asset: %w", err)
	}

	if err := verifyPackagedDefaults(temporary, options.version); err != nil {
		return err
	}
	checksums, err := buildChecksumFile(temporary, baseReleasePayloadNames(options.version))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(temporary, "SHA256SUMS"), checksums, 0o644); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}
	if err := os.Rename(temporary, options.outputDirectory); err != nil {
		return fmt.Errorf("publish assembled release package: %w", err)
	}
	return nil
}

func verifyPackagedDefaults(directory, version string) error {
	exactImage := "ghcr.io/luxiaba/remnanode-lite:" + version
	for _, name := range []string{"compose.yaml", "docker-compose.single-file.yaml", "remnanode-lite.env.example"} {
		data, err := readRegularInput(filepath.Join(directory, name), maxTextInputBytes, false)
		if err != nil {
			return fmt.Errorf("read packaged %s: %w", name, err)
		}
		if !strings.Contains(string(data), exactImage) {
			return fmt.Errorf("packaged %s does not reference exact image %s", name, exactImage)
		}
	}
	return nil
}

func buildChecksumFile(directory string, names []string) ([]byte, error) {
	names = append([]string(nil), names...)
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		digest, _, err := fileDigestAndSize(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("hash release asset %s: %w", name, err)
		}
		fmt.Fprintf(&checksums, "%s  %s\n", digest, name)
	}
	return []byte(checksums.String()), nil
}

func validReleaseImageName(image string) bool {
	parts := strings.Split(image, "/")
	if len(parts) != 3 || parts[0] != "ghcr.io" {
		return false
	}
	for _, part := range parts[1:] {
		if part == "" {
			return false
		}
		for _, character := range part {
			if !((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '.' ||
				character == '_' || character == '-') {
				return false
			}
		}
	}
	return true
}

func validateReleaseIndex(index releaseIndex) error {
	if index.SchemaVersion != 1 {
		return fmt.Errorf("release index schema_version is %d, want 1", index.SchemaVersion)
	}
	if err := validateProjectVersion(index.Version); err != nil {
		return fmt.Errorf("release index version: %w", err)
	}
	if !gitCommitPattern.MatchString(index.SourceRevision) {
		return fmt.Errorf("release index source_revision must be a 40-character lowercase Git commit")
	}
	if !validReleaseImageName(index.Image) {
		return fmt.Errorf("release index image must be a lowercase GHCR repository name")
	}
	if !validSHA256Digest(index.IndexDigest) {
		return fmt.Errorf("release index index_digest must be a sha256 OCI index digest")
	}
	return nil
}

func readReleaseIndex(path string) (releaseIndex, error) {
	data, err := readRegularInput(path, maxTextInputBytes, false)
	if err != nil {
		return releaseIndex{}, fmt.Errorf("read release index: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var index releaseIndex
	if err := decoder.Decode(&index); err != nil {
		return releaseIndex{}, fmt.Errorf("decode release index: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return releaseIndex{}, fmt.Errorf("release index contains trailing JSON data")
	}
	if err := validateReleaseIndex(index); err != nil {
		return releaseIndex{}, err
	}
	return index, nil
}

func writeReleaseIndex(path string, index releaseIndex) error {
	if err := validateReleaseIndex(index); err != nil {
		return err
	}
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode release index: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write release index: %w", err)
	}
	return nil
}

func finalizeReleasePackage(options finalizeReleaseOptions) error {
	if err := validateVersionPair(options.version, options.contractVersion); err != nil {
		return err
	}
	if !gitCommitPattern.MatchString(options.sourceRevision) {
		return fmt.Errorf("source revision must be a 40-character lowercase Git commit")
	}
	if !validReleaseImageName(options.image) {
		return fmt.Errorf("image must be a lowercase GHCR repository name")
	}
	if !validSHA256Digest(options.indexDigest) {
		return fmt.Errorf("index digest must be a sha256 OCI index digest")
	}
	indexPath := filepath.Join(options.directory, releaseIndexName)
	if _, err := os.Lstat(indexPath); err == nil {
		return fmt.Errorf("release index already exists: %s", indexPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect release index: %w", err)
	}
	if err := verifyReleasePackage(verifyPackageOptions{
		lockPath:        options.lockPath,
		directory:       options.directory,
		version:         options.version,
		contractVersion: options.contractVersion,
		sourceRevision:  options.sourceRevision,
	}); err != nil {
		return fmt.Errorf("verify staging release package: %w", err)
	}
	if err := writeReleaseIndex(indexPath, releaseIndex{
		SchemaVersion:  1,
		Version:        options.version,
		SourceRevision: options.sourceRevision,
		Image:          options.image,
		IndexDigest:    options.indexDigest,
	}); err != nil {
		return err
	}
	checksums, err := buildChecksumFile(options.directory, releasePayloadNames(options.version))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(options.directory, "SHA256SUMS"), checksums, 0o644); err != nil {
		return fmt.Errorf("write finalized release checksums: %w", err)
	}
	if err := verifyReleasePackage(verifyPackageOptions{
		lockPath:            options.lockPath,
		directory:           options.directory,
		version:             options.version,
		contractVersion:     options.contractVersion,
		sourceRevision:      options.sourceRevision,
		requireReleaseIndex: true,
	}); err != nil {
		return fmt.Errorf("verify finalized release package: %w", err)
	}
	return nil
}

func verifyReleasePackageLayout(options verifyPackageOptions) error {
	if err := validateVersionPair(options.version, options.contractVersion); err != nil {
		return err
	}
	if !gitCommitPattern.MatchString(options.sourceRevision) {
		return fmt.Errorf("source revision must be a 40-character lowercase Git commit")
	}
	entries, err := os.ReadDir(options.directory)
	if err != nil {
		return fmt.Errorf("read release package: %w", err)
	}
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect release package entry %q: %w", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release package contains non-regular entry %q", entry.Name())
		}
		actualNames = append(actualNames, entry.Name())
	}
	sort.Strings(actualNames)
	hasReleaseIndex := false
	for _, name := range actualNames {
		if name == releaseIndexName {
			hasReleaseIndex = true
			break
		}
	}
	if options.requireReleaseIndex && !hasReleaseIndex {
		return fmt.Errorf("release package is missing %s", releaseIndexName)
	}
	expectedNames := baseReleaseAssetNames(options.version)
	payloadNames := baseReleasePayloadNames(options.version)
	if hasReleaseIndex {
		expectedNames = releaseAssetNames(options.version)
		payloadNames = releasePayloadNames(options.version)
	}
	if strings.Join(actualNames, "\n") != strings.Join(expectedNames, "\n") {
		return fmt.Errorf("release package files do not match the required asset set: got %v, want %v", actualNames, expectedNames)
	}
	wantChecksums, err := buildChecksumFile(options.directory, payloadNames)
	if err != nil {
		return err
	}
	gotChecksums, err := readRegularInput(filepath.Join(options.directory, "SHA256SUMS"), maxTextInputBytes, false)
	if err != nil {
		return fmt.Errorf("read SHA256SUMS: %w", err)
	}
	if string(gotChecksums) != string(wantChecksums) {
		return fmt.Errorf("SHA256SUMS is not the canonical checksum set for the release package")
	}
	if err := verifyPackagedDefaults(options.directory, options.version); err != nil {
		return err
	}
	if hasReleaseIndex {
		index, indexErr := readReleaseIndex(filepath.Join(options.directory, releaseIndexName))
		if indexErr != nil {
			return indexErr
		}
		if index.Version != options.version || index.SourceRevision != options.sourceRevision {
			return fmt.Errorf("release index version or source revision does not match the package")
		}
	}
	return nil
}

func verifyReleasePackage(options verifyPackageOptions) error {
	if err := verifyReleasePackageLayout(options); err != nil {
		return err
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		archive := filepath.Join(options.directory,
			fmt.Sprintf("remnanode-lite_%s_linux_%s.tar.gz", options.version, architecture))
		if err := verifyBundle(verifyOptions{
			lockPath: options.lockPath, archivePath: archive, architecture: architecture,
			version: options.version, contractVersion: options.contractVersion,
			sourceRevision: options.sourceRevision,
		}); err != nil {
			return fmt.Errorf("verify %s release bundle: %w", architecture, err)
		}
	}
	return nil
}

type githubReleaseSnapshot struct {
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
	Immutable       bool   `json:"immutable"`
	Assets          []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
		State  string `json:"state"`
	} `json:"assets"`
}

type releaseImmutability string

const (
	releaseImmutabilityAny   releaseImmutability = "any"
	releaseImmutabilityFalse releaseImmutability = "false"
	releaseImmutabilityTrue  releaseImmutability = "true"
)

func parseReleaseImmutability(value string) (releaseImmutability, error) {
	switch releaseImmutability(value) {
	case releaseImmutabilityAny, releaseImmutabilityFalse, releaseImmutabilityTrue:
		return releaseImmutability(value), nil
	default:
		return "", fmt.Errorf("immutable must be true, false, or any")
	}
}

func verifyReleaseSnapshot(snapshotPath, directory, tag, commit string, draft, prerelease bool, immutable releaseImmutability) error {
	data, err := readRegularInput(snapshotPath, maxManifestBytes, false)
	if err != nil {
		return fmt.Errorf("read GitHub Release snapshot: %w", err)
	}
	var snapshot githubReleaseSnapshot
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("decode GitHub Release snapshot: %w", err)
	}
	if snapshot.TagName != tag || snapshot.TargetCommitish != commit || snapshot.Draft != draft ||
		snapshot.Prerelease != prerelease {
		return fmt.Errorf("GitHub Release identity or state does not match the requested publication")
	}
	if immutable != releaseImmutabilityAny && snapshot.Immutable != (immutable == releaseImmutabilityTrue) {
		return fmt.Errorf("GitHub Release immutable state is %t, want %s", snapshot.Immutable, immutable)
	}
	expected := make(map[string]struct {
		digest string
		size   int64
	}, len(releaseAssetNames(tag)))
	for _, name := range releaseAssetNames(tag) {
		digest, size, digestErr := fileDigestAndSize(filepath.Join(directory, name))
		if digestErr != nil {
			return fmt.Errorf("inspect local release asset %s: %w", name, digestErr)
		}
		expected[name] = struct {
			digest string
			size   int64
		}{digest: "sha256:" + digest, size: size}
	}
	if len(snapshot.Assets) != len(expected) {
		return fmt.Errorf("GitHub Release has %d assets, want %d", len(snapshot.Assets), len(expected))
	}
	seen := make(map[string]struct{}, len(snapshot.Assets))
	for _, asset := range snapshot.Assets {
		want, exists := expected[asset.Name]
		if !exists {
			return fmt.Errorf("GitHub Release contains unexpected asset %q", asset.Name)
		}
		if _, duplicate := seen[asset.Name]; duplicate {
			return fmt.Errorf("GitHub Release contains duplicate asset %q", asset.Name)
		}
		seen[asset.Name] = struct{}{}
		if asset.State != "uploaded" || asset.Digest != want.digest || asset.Size != want.size {
			return fmt.Errorf("GitHub Release asset %q does not match the local digest, size, or upload state", asset.Name)
		}
	}
	return nil
}

func runAssemble(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("assemble", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := assembleOptions{version: projectversion.Version}
	flags.StringVar(&options.projectRoot, "project-root", ".", "repository root")
	flags.StringVar(&options.nativeDirectory, "native-dir", "", "directory containing verified Native bundles")
	flags.StringVar(&options.outputDirectory, "out-dir", "", "new release package directory")
	flags.StringVar(&options.version, "version", options.version, "project version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("assemble does not accept positional arguments")
	}
	if err := assembleReleasePackage(options); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "assembled release package: %s\n", options.outputDirectory)
	return nil
}

func runFinalizeRelease(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("finalize-release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := finalizeReleaseOptions{
		lockPath: "release/runtime-assets.lock.json", version: projectversion.Version,
		contractVersion: projectversion.ContractVersion,
	}
	flags.StringVar(&options.lockPath, "lock", options.lockPath, "runtime asset lock path")
	flags.StringVar(&options.directory, "directory", "", "staging release package directory")
	flags.StringVar(&options.version, "version", options.version, "expected project version")
	flags.StringVar(&options.contractVersion, "contract-version", options.contractVersion, "expected contract version")
	flags.StringVar(&options.sourceRevision, "source-revision", "", "accepted 40-character source Git commit")
	flags.StringVar(&options.image, "image", "", "accepted lowercase GHCR image repository")
	flags.StringVar(&options.indexDigest, "index-digest", "", "accepted sha256 OCI index digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("finalize-release does not accept positional arguments")
	}
	if options.directory == "" || options.sourceRevision == "" || options.image == "" || options.indexDigest == "" {
		return fmt.Errorf("finalize-release requires directory, source revision, image, and index digest")
	}
	if err := finalizeReleasePackage(options); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "finalized release package: %s\n", options.directory)
	return nil
}

func runVerifyPackage(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify-package", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := verifyPackageOptions{
		lockPath: "release/runtime-assets.lock.json", version: projectversion.Version,
		contractVersion: projectversion.ContractVersion,
	}
	flags.StringVar(&options.lockPath, "lock", options.lockPath, "runtime asset lock path")
	flags.StringVar(&options.directory, "directory", "", "release package directory")
	flags.StringVar(&options.version, "version", options.version, "expected project version")
	flags.StringVar(&options.contractVersion, "contract-version", options.contractVersion, "expected contract version")
	flags.StringVar(&options.sourceRevision, "source-revision", "", "expected source Git commit")
	flags.BoolVar(&options.requireReleaseIndex, "require-release-index", false, "require the accepted OCI index release asset")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("verify-package does not accept positional arguments")
	}
	if options.directory == "" {
		return fmt.Errorf("verify-package requires --directory")
	}
	if err := verifyReleasePackage(options); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "verified release package: %s\n", options.directory)
	return nil
}

func runVerifyReleaseIndex(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify-release-index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("file", "", "release-index.json asset")
	tag := flags.String("tag", "", "expected release tag")
	image := flags.String("image", "", "expected lowercase GHCR image repository")
	sourceRevision := flags.String("source-revision", "", "optional expected 40-character source Git commit")
	indexDigest := flags.String("index-digest", "", "optional expected sha256 OCI index digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("verify-release-index does not accept positional arguments")
	}
	if *path == "" || *tag == "" || *image == "" {
		return fmt.Errorf("verify-release-index requires file, tag, and image")
	}
	version := *tag
	if err := validateProjectVersion(version); err != nil {
		return fmt.Errorf("invalid release tag %q", *tag)
	}
	index, err := readReleaseIndex(*path)
	if err != nil {
		return err
	}
	if index.Version != version || index.Image != *image {
		return fmt.Errorf("release index version or image does not match the requested Release")
	}
	if *sourceRevision != "" {
		if !gitCommitPattern.MatchString(*sourceRevision) {
			return fmt.Errorf("invalid expected source revision")
		}
		if index.SourceRevision != *sourceRevision {
			return fmt.Errorf("release index source revision does not match the requested Release")
		}
	}
	if *indexDigest != "" {
		if !validSHA256Digest(*indexDigest) {
			return fmt.Errorf("invalid expected OCI index digest")
		}
		if index.IndexDigest != *indexDigest {
			return fmt.Errorf("release index digest does not match the accepted OCI index")
		}
	}
	fmt.Fprintf(stdout, "source_revision=%s\nindex_digest=%s\n", index.SourceRevision, index.IndexDigest)
	return nil
}

func runVerifyRelease(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	snapshot := flags.String("snapshot", "", "GitHub Release API JSON snapshot")
	directory := flags.String("directory", "", "local release package directory")
	tag := flags.String("tag", "", "expected release tag")
	commit := flags.String("commit", "", "expected target commit")
	draft := flags.Bool("draft", false, "expect a draft release")
	prerelease := flags.Bool("prerelease", false, "expect a prerelease")
	immutableValue := flags.String("immutable", "", "expected GitHub release immutability: true, false, or any")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("verify-release does not accept positional arguments")
	}
	if *snapshot == "" || *directory == "" || *tag == "" || !gitCommitPattern.MatchString(*commit) {
		return fmt.Errorf("verify-release requires snapshot, directory, valid tag, and commit")
	}
	if err := validateProjectVersion(*tag); err != nil {
		return fmt.Errorf("invalid release tag %q", *tag)
	}
	immutable, err := parseReleaseImmutability(*immutableValue)
	if err != nil {
		return err
	}
	if err := verifyReleaseSnapshot(*snapshot, *directory, *tag, *commit, *draft, *prerelease, immutable); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "verified GitHub Release %s and every local asset\n", *tag)
	return nil
}
