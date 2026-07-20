// Package sourceoracle verifies contract evidence against immutable Git objects
// from the pinned official Remnawave Node commit.
package sourceoracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Luxiaba/remnanode-lite/internal/executil"
)

const (
	manifestSchemaVersion = 1
	maxOfficialBlobBytes  = 4 << 20
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Expectation is the locally implemented contract that must agree with the
// independently extracted official source evidence.
type Expectation struct {
	Repository          string
	PackageName         string
	Version             string
	Commit              string
	Files               []string
	Routes              []ExpectedRoute
	ExcludedControllers []ExcludedController
}

// ExpectedRoute is one route distilled into the local executable contract.
type ExpectedRoute struct {
	Method           string
	Path             string
	ControllerSource string
}

// ExcludedController identifies an official controller that is deliberately
// outside the global /node API prefix.
type ExcludedController struct {
	Source                   string
	RequiredPrefixExclusions []string
}

type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Repository    string          `json:"repository"`
	PackageName   string          `json:"packageName"`
	Version       string          `json:"version"`
	Commit        string          `json:"commit"`
	Files         []ManifestFile  `json:"files"`
	Routes        []ManifestRoute `json:"routes"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ManifestRoute struct {
	Method           string `json:"method"`
	Path             string `json:"path"`
	ControllerSource string `json:"controllerSource"`
	Decorator        string `json:"decorator"`
}

// Generate reads only blobs addressed by expectation.Commit. Neither the Git
// index nor the checked-out worktree participates in the generated evidence.
func Generate(ctx context.Context, repository string, expectation Expectation) ([]byte, error) {
	if err := validateExpectation(expectation); err != nil {
		return nil, err
	}
	reader := gitObjectReader{repository: repository, commit: expectation.Commit}
	if err := reader.verifyCommit(ctx); err != nil {
		return nil, err
	}

	packageRaw, err := reader.readBlob(ctx, "package.json")
	if err != nil {
		return nil, err
	}
	var packageIdentity struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageRaw, &packageIdentity); err != nil {
		return nil, fmt.Errorf("decode official package.json: %w", err)
	}
	if packageIdentity.Name != expectation.PackageName || packageIdentity.Version != expectation.Version {
		return nil, fmt.Errorf(
			"official package is %s@%s, want %s@%s",
			packageIdentity.Name,
			packageIdentity.Version,
			expectation.PackageName,
			expectation.Version,
		)
	}

	files := make([]ManifestFile, 0, len(expectation.Files))
	for _, sourcePath := range sortedUnique(expectation.Files) {
		blob, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(blob)
		files = append(files, ManifestFile{
			Path:   sourcePath,
			SHA256: hex.EncodeToString(digest[:]),
		})
	}

	controllerSources := make([]string, 0, len(expectation.Routes))
	for _, route := range expectation.Routes {
		controllerSources = append(controllerSources, route.ControllerSource)
	}
	routes, err := extractRoutes(
		ctx,
		reader,
		sortedUnique(controllerSources),
		expectation.ExcludedControllers,
		expectation.Files,
	)
	if err != nil {
		return nil, fmt.Errorf("extract official routes: %w", err)
	}

	manifest := Manifest{
		SchemaVersion: manifestSchemaVersion,
		Repository:    expectation.Repository,
		PackageName:   expectation.PackageName,
		Version:       expectation.Version,
		Commit:        expectation.Commit,
		Files:         files,
		Routes:        routes,
	}
	return marshalCanonical(manifest)
}

// Validate checks the checked-in manifest against the local contract without
// needing an official source checkout. Source blob contents are verified by
// VerifySource.
func Validate(raw []byte, expectation Expectation) error {
	if err := validateExpectation(expectation); err != nil {
		return err
	}
	manifest, err := decodeCanonical(raw)
	if err != nil {
		return err
	}
	if manifest.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("source manifest schemaVersion=%d, want %d", manifest.SchemaVersion, manifestSchemaVersion)
	}
	if manifest.Repository != expectation.Repository ||
		manifest.PackageName != expectation.PackageName ||
		manifest.Version != expectation.Version ||
		manifest.Commit != expectation.Commit {
		return fmt.Errorf(
			"source manifest identity is %s %s@%s commit %s, want %s %s@%s commit %s",
			manifest.Repository,
			manifest.PackageName,
			manifest.Version,
			manifest.Commit,
			expectation.Repository,
			expectation.PackageName,
			expectation.Version,
			expectation.Commit,
		)
	}

	wantFiles := sortedUnique(expectation.Files)
	gotFiles := make([]string, 0, len(manifest.Files))
	seenFiles := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if err := validateSourcePath(file.Path); err != nil {
			return fmt.Errorf("source manifest file: %w", err)
		}
		if !digestPattern.MatchString(file.SHA256) {
			return fmt.Errorf("source manifest file %s has invalid SHA-256 %q", file.Path, file.SHA256)
		}
		if _, exists := seenFiles[file.Path]; exists {
			return fmt.Errorf("source manifest repeats file %s", file.Path)
		}
		seenFiles[file.Path] = struct{}{}
		gotFiles = append(gotFiles, file.Path)
	}
	if difference := compareStrings(gotFiles, wantFiles); difference != "" {
		return fmt.Errorf("source manifest file inventory differs from the contract: %s", difference)
	}

	wantRoutes := make([]routeIdentity, 0, len(expectation.Routes))
	for _, route := range expectation.Routes {
		wantRoutes = append(wantRoutes, routeIdentity{
			Method:           route.Method,
			Path:             route.Path,
			ControllerSource: route.ControllerSource,
		})
	}
	gotRoutes := make([]routeIdentity, 0, len(manifest.Routes))
	seenRoutes := make(map[string]struct{}, len(manifest.Routes))
	for _, route := range manifest.Routes {
		if route.Decorator == "" {
			return fmt.Errorf("source manifest route %s %s has no decorator evidence", route.Method, route.Path)
		}
		identity := routeIdentity{
			Method:           route.Method,
			Path:             route.Path,
			ControllerSource: route.ControllerSource,
		}
		key := identity.key()
		if _, exists := seenRoutes[key]; exists {
			return fmt.Errorf("source manifest repeats route %s", key)
		}
		seenRoutes[key] = struct{}{}
		gotRoutes = append(gotRoutes, identity)
	}
	if difference := compareRouteIdentities(gotRoutes, wantRoutes); difference != "" {
		return fmt.Errorf("machine-extracted official routes differ from the local contract: %s", difference)
	}
	return nil
}

// VerifySource regenerates the manifest from the pinned commit and requires an
// exact canonical match. A dirty worktree therefore cannot change the oracle.
func VerifySource(ctx context.Context, repository string, raw []byte, expectation Expectation) error {
	if err := Validate(raw, expectation); err != nil {
		return err
	}
	generated, err := Generate(ctx, repository, expectation)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, generated) {
		return describeManifestDrift(raw, generated)
	}
	return nil
}

func validateExpectation(expectation Expectation) error {
	if expectation.Repository == "" || expectation.PackageName == "" || expectation.Version == "" {
		return fmt.Errorf("official source expectation identity is incomplete")
	}
	if !commitPattern.MatchString(expectation.Commit) {
		return fmt.Errorf("official source commit %q must be 40 lowercase hexadecimal characters", expectation.Commit)
	}
	if len(expectation.Files) == 0 || len(expectation.Routes) == 0 {
		return fmt.Errorf("official source expectation must contain files and routes")
	}
	seenFiles := make(map[string]struct{}, len(expectation.Files))
	for _, sourcePath := range expectation.Files {
		if err := validateSourcePath(sourcePath); err != nil {
			return err
		}
		if _, exists := seenFiles[sourcePath]; exists {
			return fmt.Errorf("official source expectation repeats file %s", sourcePath)
		}
		seenFiles[sourcePath] = struct{}{}
	}
	seenRoutes := make(map[string]struct{}, len(expectation.Routes))
	for _, route := range expectation.Routes {
		if route.Method == "" || route.Path == "" || route.ControllerSource == "" {
			return fmt.Errorf("official source expectation contains an incomplete route: %#v", route)
		}
		if _, exists := seenFiles[route.ControllerSource]; !exists {
			return fmt.Errorf("route %s %s uses unregistered controller source %s", route.Method, route.Path, route.ControllerSource)
		}
		key := (routeIdentity{
			Method:           route.Method,
			Path:             route.Path,
			ControllerSource: route.ControllerSource,
		}).key()
		if _, exists := seenRoutes[key]; exists {
			return fmt.Errorf("official source expectation repeats route %s", key)
		}
		seenRoutes[key] = struct{}{}
	}
	seenExcluded := make(map[string]struct{}, len(expectation.ExcludedControllers))
	for _, controller := range expectation.ExcludedControllers {
		if err := validateSourcePath(controller.Source); err != nil {
			return fmt.Errorf("excluded controller: %w", err)
		}
		if _, exists := seenFiles[controller.Source]; !exists {
			return fmt.Errorf("excluded controller source %s is not registered as evidence", controller.Source)
		}
		if _, exists := seenExcluded[controller.Source]; exists {
			return fmt.Errorf("excluded controller source %s is repeated", controller.Source)
		}
		seenExcluded[controller.Source] = struct{}{}
		if len(controller.RequiredPrefixExclusions) == 0 {
			return fmt.Errorf("excluded controller %s has no required global-prefix exclusions", controller.Source)
		}
		for _, symbol := range controller.RequiredPrefixExclusions {
			if !symbolPattern.MatchString(symbol) {
				return fmt.Errorf("excluded controller %s has invalid exclusion symbol %q", controller.Source, symbol)
			}
		}
	}
	return nil
}

func validateSourcePath(sourcePath string) error {
	if sourcePath == "" || strings.Contains(sourcePath, "\\") || strings.Contains(sourcePath, ":") ||
		path.IsAbs(sourcePath) || path.Clean(sourcePath) != sourcePath || sourcePath == "." || strings.HasPrefix(sourcePath, "../") {
		return fmt.Errorf("invalid repository-relative source path %q", sourcePath)
	}
	return nil
}

func marshalCanonical(manifest Manifest) ([]byte, error) {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode official source manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

func decodeCanonical(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode official source manifest: %w", err)
	}
	if decoder.More() {
		return Manifest{}, fmt.Errorf("decode official source manifest: trailing JSON value")
	}
	canonical, err := marshalCanonical(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return Manifest{}, fmt.Errorf("official source manifest is not canonical; regenerate it with contract-source-check -write")
	}
	return manifest, nil
}

func describeManifestDrift(currentRaw, generatedRaw []byte) error {
	current, currentErr := decodeCanonical(currentRaw)
	generated, generatedErr := decodeCanonical(generatedRaw)
	if currentErr != nil || generatedErr != nil {
		return fmt.Errorf("official source manifest differs from pinned Git objects; regenerate it with contract-source-check -write")
	}
	generatedFiles := make(map[string]string, len(generated.Files))
	for _, file := range generated.Files {
		generatedFiles[file.Path] = file.SHA256
	}
	for _, file := range current.Files {
		if want := generatedFiles[file.Path]; want != "" && want != file.SHA256 {
			return fmt.Errorf("official source blob %s has SHA-256 %s, manifest records %s", file.Path, want, file.SHA256)
		}
	}
	return fmt.Errorf("official source manifest differs from pinned Git objects; regenerate it with contract-source-check -write")
}

type routeIdentity struct {
	Method           string
	Path             string
	ControllerSource string
}

func (route routeIdentity) key() string {
	return route.Method + " " + route.Path + " [" + route.ControllerSource + "]"
}

func compareRouteIdentities(got, want []routeIdentity) string {
	gotKeys := make([]string, 0, len(got))
	wantKeys := make([]string, 0, len(want))
	for _, route := range got {
		gotKeys = append(gotKeys, route.key())
	}
	for _, route := range want {
		wantKeys = append(wantKeys, route.key())
	}
	return compareStrings(gotKeys, wantKeys)
}

func compareStrings(got, want []string) string {
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	gotSet := make(map[string]struct{}, len(got))
	wantSet := make(map[string]struct{}, len(want))
	for _, value := range got {
		gotSet[value] = struct{}{}
	}
	for _, value := range want {
		wantSet[value] = struct{}{}
	}
	var missing, unexpected []string
	for _, value := range want {
		if _, exists := gotSet[value]; !exists {
			missing = append(missing, value)
		}
	}
	for _, value := range got {
		if _, exists := wantSet[value]; !exists {
			unexpected = append(unexpected, value)
		}
	}
	if len(missing) == 0 && len(unexpected) == 0 && len(got) == len(want) {
		return ""
	}
	return fmt.Sprintf("missing=%q unexpected=%q", missing, unexpected)
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) == 0 {
		return result
	}
	write := 1
	for read := 1; read < len(result); read++ {
		if result[read] == result[write-1] {
			continue
		}
		result[write] = result[read]
		write++
	}
	return result[:write]
}

type gitObjectReader struct {
	repository string
	commit     string
}

func (reader gitObjectReader) verifyCommit(ctx context.Context) error {
	if strings.TrimSpace(reader.repository) == "" {
		return fmt.Errorf("official Git repository path is empty")
	}
	result, err := executil.Run(ctx, nil, 1024, "git", "--no-replace-objects", "-C", reader.repository, "cat-file", "-t", reader.commit)
	if err != nil {
		return fmt.Errorf("read official Git commit %s: %w: %s", reader.commit, err, strings.TrimSpace(string(result.DiagnosticOutput())))
	}
	if result.AnyTruncated() || strings.TrimSpace(string(result.Stdout)) != "commit" {
		return fmt.Errorf("official Git object %s is not a commit", reader.commit)
	}
	return nil
}

func (reader gitObjectReader) readBlob(ctx context.Context, sourcePath string) ([]byte, error) {
	if err := validateSourcePath(sourcePath); err != nil {
		return nil, err
	}
	object := reader.commit + ":" + sourcePath
	result, err := executil.Run(ctx, nil, maxOfficialBlobBytes, "git", "--no-replace-objects", "-C", reader.repository, "cat-file", "blob", object)
	if err != nil {
		return nil, fmt.Errorf("read official Git blob %s: %w: %s", object, err, strings.TrimSpace(string(result.DiagnosticOutput())))
	}
	if result.AnyTruncated() {
		return nil, fmt.Errorf("official Git blob %s exceeds %d bytes", object, maxOfficialBlobBytes)
	}
	if len(result.Stdout) == 0 {
		return nil, fmt.Errorf("official Git blob %s is empty", object)
	}
	return result.Stdout, nil
}

func (reader gitObjectReader) listFiles(ctx context.Context, prefix string) ([]string, error) {
	if err := validateSourcePath(prefix); err != nil {
		return nil, err
	}
	result, err := executil.Run(
		ctx,
		nil,
		maxOfficialBlobBytes,
		"git",
		"--no-replace-objects",
		"-C",
		reader.repository,
		"ls-tree",
		"-r",
		"--name-only",
		reader.commit,
		"--",
		prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("list official Git tree %s:%s: %w: %s", reader.commit, prefix, err, strings.TrimSpace(string(result.DiagnosticOutput())))
	}
	if result.AnyTruncated() {
		return nil, fmt.Errorf("official Git tree listing %s:%s exceeds %d bytes", reader.commit, prefix, maxOfficialBlobBytes)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		if line == "" {
			continue
		}
		if err := validateSourcePath(line); err != nil {
			return nil, fmt.Errorf("official Git tree returned %w", err)
		}
		files = append(files, line)
	}
	return files, nil
}
