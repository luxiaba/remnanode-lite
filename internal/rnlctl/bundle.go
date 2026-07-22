package rnlctl

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	bundleTopDirectory  = "remnanode-lite"
	maxManifestBytes    = 4 << 20
	maxBundleFiles      = 512
	maxBundleEntries    = 512
	maxBundleFileBytes  = 256 << 20
	maxBundleTotalBytes = 512 << 20
	maxBundleArchive    = 512 << 20
)

var (
	projectVersionRE  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rnl\.[1-9][0-9]*)?$`)
	contractVersionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	gitRevisionRE     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hexDigestRE       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type releaseManifest struct {
	SchemaVersion          int                   `json:"schemaVersion"`
	Name                   string                `json:"name"`
	Version                string                `json:"version"`
	ContractVersion        string                `json:"contractVersion"`
	OS                     string                `json:"os"`
	Architecture           string                `json:"architecture"`
	SourceRevision         string                `json:"sourceRevision"`
	SourceDateEpoch        int64                 `json:"sourceDateEpoch"`
	RuntimeAssetLockSHA256 string                `json:"runtimeAssetLockSHA256"`
	RuntimeAssets          manifestRuntimeAssets `json:"runtimeAssets"`
	Files                  []manifestFile        `json:"files"`
}

type manifestFile struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Role    string `json:"role"`
	License string `json:"license"`
}

type manifestRuntimeAssets struct {
	Xray    manifestXrayRuntime `json:"xray"`
	GeoIP   manifestGeoRuntime  `json:"geoIP"`
	GeoSite manifestGeoRuntime  `json:"geoSite"`
	ASN     manifestASNRuntime  `json:"asn"`
}

type manifestXrayRuntime struct {
	Version   string                 `json:"version"`
	Commit    string                 `json:"commit"`
	SourceURL string                 `json:"sourceURL"`
	Archive   manifestArtifact       `json:"archive"`
	Core      manifestRuntimePayload `json:"core"`
}

type manifestGeoRuntime struct {
	Version          string           `json:"version"`
	Commit           string           `json:"commit"`
	SourceURL        string           `json:"sourceURL"`
	Artifact         manifestArtifact `json:"artifact"`
	License          string           `json:"license"`
	LicenseRationale string           `json:"licenseRationale,omitempty"`
}

type manifestASNRuntime struct {
	Commit string                 `json:"commit"`
	Source manifestArtifact       `json:"source"`
	Output manifestRuntimePayload `json:"output"`
}

type manifestArtifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type manifestRuntimePayload struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	License string `json:"license"`
}

type validatedBundle struct {
	Root          string
	Manifest      releaseManifest
	ManifestRaw   []byte
	Identity      string
	GenerationID  string
	Archive       string
	ArchiveSHA256 string
	CacheKind     string
	cleanup       func()
}

func (bundle *validatedBundle) Close() {
	if bundle != nil && bundle.cleanup != nil {
		bundle.cleanup()
		bundle.cleanup = nil
	}
}

func openBundle(input BundleInput, architecture string) (*validatedBundle, error) {
	if (input.Root == "") == (input.Archive == "") {
		return nil, fmt.Errorf("exactly one of --bundle-root or --bundle is required")
	}
	if input.Root != "" {
		if input.SHA256 != "" {
			return nil, fmt.Errorf("--sha256 is valid only with --bundle")
		}
		root, err := locateBundleRoot(input.Root)
		if err != nil {
			return nil, err
		}
		bundle, err := validateBundleRoot(root, architecture)
		if err != nil {
			return nil, err
		}
		if err := bindExpectedVersion(bundle, input.ExpectedVersion); err != nil {
			return nil, err
		}
		return bundle, nil
	}
	if !hexDigestRE.MatchString(input.SHA256) {
		return nil, fmt.Errorf("--bundle requires a lowercase 64-character --sha256")
	}
	temporary, err := createNativeTemporaryDirectory("remnanode-native-bundle-*")
	if err != nil {
		return nil, fmt.Errorf("create bundle workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	snapshot := filepath.Join(temporary, "bundle.tar.gz")
	if err := snapshotVerifiedArchive(input.Archive, snapshot, input.SHA256); err != nil {
		cleanup()
		return nil, err
	}
	root, err := extractBundleArchive(snapshot, temporary)
	if err != nil {
		cleanup()
		return nil, err
	}
	bundle, err := validateBundleRoot(root, architecture)
	if err != nil {
		cleanup()
		return nil, err
	}
	bundle.Archive = snapshot
	bundle.ArchiveSHA256 = input.SHA256
	bundle.CacheKind = "verified-archive"
	bundle.cleanup = cleanup
	if err := bindExpectedVersion(bundle, input.ExpectedVersion); err != nil {
		bundle.Close()
		return nil, err
	}
	return bundle, nil
}

func bindExpectedVersion(bundle *validatedBundle, expected string) error {
	if expected == "" {
		return nil
	}
	if !projectVersionRE.MatchString(expected) {
		return fmt.Errorf("invalid expected version %q", expected)
	}
	if bundle.Manifest.Version != expected {
		return fmt.Errorf("bundle version %q does not match expected version %q", bundle.Manifest.Version, expected)
	}
	return nil
}

func snapshotVerifiedArchive(source, destination, expected string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open bundle archive: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBundleArchive {
		return fmt.Errorf("bundle archive must be a regular file no larger than %d bytes", maxBundleArchive)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hasher), io.LimitReader(input, maxBundleArchive+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.Join(copyErr, syncErr, closeErr)
	}
	if written != info.Size() || written > maxBundleArchive {
		_ = os.Remove(destination)
		return fmt.Errorf("bundle archive changed while being read or exceeds size limit")
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		_ = os.Remove(destination)
		return fmt.Errorf("bundle archive SHA-256 mismatch: got %s, want %s", actual, expected)
	}
	return syncDirectory(filepath.Dir(destination))
}

func locateBundleRoot(candidate string) (string, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if err := requireRealDirectory(abs); err != nil {
		return "", fmt.Errorf("inspect bundle root: %w", err)
	}
	if _, err := os.Lstat(filepath.Join(abs, "release-manifest.json")); err == nil {
		return abs, nil
	}
	nested := filepath.Join(abs, bundleTopDirectory)
	if _, err := os.Lstat(filepath.Join(nested, "release-manifest.json")); err == nil {
		if err := requireRealDirectory(nested); err != nil {
			return "", err
		}
		return nested, nil
	}
	return "", fmt.Errorf("%s does not contain release-manifest.json", candidate)
}

func validateBundleRoot(root, architecture string) (*validatedBundle, error) {
	if err := requireRealDirectory(root); err != nil {
		return nil, fmt.Errorf("inspect bundle root: %w", err)
	}
	manifestPath := filepath.Join(root, "release-manifest.json")
	raw, err := readRegularFile(manifestPath, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read release manifest: %w", err)
	}
	var manifest releaseManifest
	if err := decodeStrictJSON(raw, &manifest); err != nil {
		return nil, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := validateManifestIdentity(manifest, architecture); err != nil {
		return nil, err
	}
	if err := validateManifestPayload(root, manifest); err != nil {
		return nil, err
	}
	identity := digestBytes(raw)
	return &validatedBundle{
		Root: root, Manifest: manifest, ManifestRaw: raw, Identity: identity,
		GenerationID: manifest.Version + "-" + identity[:16],
	}, nil
}

func validateManifestIdentity(manifest releaseManifest, architecture string) error {
	if manifest.SchemaVersion != manifestSchema || manifest.Name != bundleTopDirectory || manifest.OS != "linux" {
		return fmt.Errorf("release manifest has unsupported schema or identity")
	}
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	if architecture != "amd64" && architecture != "arm64" {
		return fmt.Errorf("unsupported host architecture %q", architecture)
	}
	if manifest.Architecture != architecture {
		return fmt.Errorf("bundle architecture %q does not match host architecture %q", manifest.Architecture, architecture)
	}
	if !projectVersionRE.MatchString(manifest.Version) || !contractVersionRE.MatchString(manifest.ContractVersion) {
		return fmt.Errorf("release manifest contains an invalid version")
	}
	if !strings.Contains(manifest.Version, "-rnl.") && manifest.Version != manifest.ContractVersion {
		return fmt.Errorf("stable release version %q must equal contract version %q", manifest.Version, manifest.ContractVersion)
	}
	if !gitRevisionRE.MatchString(manifest.SourceRevision) || manifest.SourceDateEpoch <= 0 {
		return fmt.Errorf("release manifest contains invalid source metadata")
	}
	if err := validateRuntimeAssets(manifest.RuntimeAssets); err != nil {
		return fmt.Errorf("release manifest runtime assets: %w", err)
	}
	if !hexDigestRE.MatchString(manifest.RuntimeAssetLockSHA256) {
		return fmt.Errorf("release manifest has invalid runtime asset lock digest")
	}
	return nil
}

func validateRuntimeAssets(assets manifestRuntimeAssets) error {
	if assets.Xray.Version == "" || !gitRevisionRE.MatchString(assets.Xray.Commit) || assets.Xray.SourceURL == "" {
		return fmt.Errorf("invalid Xray provenance")
	}
	if err := validateManifestArtifact(assets.Xray.Archive); err != nil {
		return fmt.Errorf("Xray archive: %w", err)
	}
	if err := validateRuntimePayload(assets.Xray.Core); err != nil {
		return fmt.Errorf("Xray core: %w", err)
	}
	for name, geo := range map[string]manifestGeoRuntime{"geoIP": assets.GeoIP, "geoSite": assets.GeoSite} {
		if geo.Version == "" || !gitRevisionRE.MatchString(geo.Commit) || geo.SourceURL == "" || geo.License == "" {
			return fmt.Errorf("invalid %s provenance", name)
		}
		if err := validateManifestArtifact(geo.Artifact); err != nil {
			return fmt.Errorf("%s artifact: %w", name, err)
		}
	}
	if !gitRevisionRE.MatchString(assets.ASN.Commit) {
		return fmt.Errorf("invalid ASN provenance")
	}
	if err := validateManifestArtifact(assets.ASN.Source); err != nil {
		return fmt.Errorf("ASN source: %w", err)
	}
	if err := validateRuntimePayload(assets.ASN.Output); err != nil {
		return fmt.Errorf("ASN output: %w", err)
	}
	return nil
}

func validateManifestArtifact(artifact manifestArtifact) error {
	if artifact.URL == "" || !hexDigestRE.MatchString(artifact.SHA256) || artifact.Size <= 0 {
		return fmt.Errorf("invalid artifact metadata")
	}
	return nil
}

func validateRuntimePayload(payload manifestRuntimePayload) error {
	if !hexDigestRE.MatchString(payload.SHA256) || payload.Size <= 0 || payload.License == "" {
		return fmt.Errorf("invalid runtime payload metadata")
	}
	return nil
}

func validateManifestPayload(root string, manifest releaseManifest) error {
	required := map[string]string{
		"LICENSE":                               "0644",
		"SOURCE-OFFER.md":                       "0644",
		"THIRD_PARTY_NOTICES.md":                "0644",
		"SBOM.spdx.json":                        "0644",
		"install.sh":                            "0755",
		"bin/remnanode-lite":                    "0755",
		"bin/rnlctl":                            "0755",
		"lib/rw-core":                           "0755",
		"share/asn/asn-prefixes.bin":            "0644",
		"share/xray/geoip.dat":                  "0644",
		"share/xray/geosite.dat":                "0644",
		"support/deploy/remnanode-lite.service": "0644",
		"support/deploy/remnanode-lite-hardening.conf": "0644",
		"support/deploy/remnanode-lite.openrc":         "0755",
		"support/deploy/node.env.example":              "0644",
		"runtime-assets.lock.json":                     "0644",
		"licenses/MPL-2.0.txt":                         "0644",
		"licenses/GPL-3.0-only.txt":                    "0644",
		"licenses/CC-BY-SA-4.0.txt":                    "0644",
		"licenses/CC0-1.0.txt":                         "0644",
	}
	seen := make(map[string]manifestFile, len(manifest.Files))
	last := ""
	var total int64
	for _, file := range manifest.Files {
		if err := validateBundleRelativePath(file.Path); err != nil {
			return fmt.Errorf("manifest path %q: %w", file.Path, err)
		}
		if file.Path <= last {
			return fmt.Errorf("manifest file paths are not in strict order at %q", file.Path)
		}
		last = file.Path
		if file.Path == "release-manifest.json" {
			return fmt.Errorf("release manifest cannot checksum itself")
		}
		if file.Mode != "0644" && file.Mode != "0755" {
			return fmt.Errorf("manifest file %q has invalid mode %q", file.Path, file.Mode)
		}
		if file.Size <= 0 || file.Size > maxBundleFileBytes || total > maxBundleTotalBytes-file.Size {
			return fmt.Errorf("manifest file %q exceeds bundle size limits", file.Path)
		}
		total += file.Size
		if !hexDigestRE.MatchString(file.SHA256) || file.Role == "" || file.License == "" {
			return fmt.Errorf("manifest file %q has incomplete metadata", file.Path)
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return fmt.Errorf("manifest repeats file %q", file.Path)
		}
		seen[file.Path] = file
	}
	if len(seen) == 0 || len(seen) > maxBundleFiles {
		return fmt.Errorf("manifest file count is outside supported limits")
	}
	for name, mode := range required {
		file, exists := seen[name]
		if !exists {
			return fmt.Errorf("manifest is missing required file %q", name)
		}
		if file.Mode != mode {
			return fmt.Errorf("required file %q has mode %s, want %s", name, file.Mode, mode)
		}
	}

	actual := make(map[string]struct{}, len(seen))
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundle path %q is a symlink", relative)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("bundle path %q is not a regular file", relative)
		}
		if relative == "release-manifest.json" {
			return nil
		}
		metadata, exists := seen[relative]
		if !exists {
			return fmt.Errorf("bundle contains unmanifested file %q", relative)
		}
		mode, _ := strconv.ParseUint(metadata.Mode, 8, 32)
		if info.Mode().Perm() != fs.FileMode(mode) || info.Size() != metadata.Size {
			return fmt.Errorf("bundle file %q does not match manifest mode or size", relative)
		}
		digest, size, err := digestFile(current, maxBundleFileBytes)
		if err != nil {
			return err
		}
		if size != metadata.Size || digest != metadata.SHA256 {
			return fmt.Errorf("bundle file %q does not match manifest SHA-256", relative)
		}
		actual[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(actual) != len(seen) {
		for name := range seen {
			if _, exists := actual[name]; !exists {
				return fmt.Errorf("manifest file %q is missing from bundle", name)
			}
		}
	}
	lockDigest, _, err := digestFile(filepath.Join(root, "runtime-assets.lock.json"), maxManifestBytes)
	if err != nil {
		return fmt.Errorf("hash runtime asset lock: %w", err)
	}
	if lockDigest != manifest.RuntimeAssetLockSHA256 {
		return fmt.Errorf("runtime asset lock does not match manifest SHA-256")
	}
	return nil
}

func validateBundleRelativePath(value string) error {
	if value == "" || len(value) > 512 || strings.ContainsRune(value, '\x00') || strings.Contains(value, `\`) {
		return fmt.Errorf("unsafe path")
	}
	if path.IsAbs(value) || path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("path is not a canonical relative path")
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("path contains an unsafe component")
		}
	}
	return nil
}

func extractBundleArchive(archivePath, destination string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open bundle archive: %w", err)
	}
	defer file.Close()
	zipper, err := gzip.NewReader(io.LimitReader(file, maxBundleArchive+1))
	if err != nil {
		return "", fmt.Errorf("open bundle gzip stream: %w", err)
	}
	defer zipper.Close()
	archive := tar.NewReader(zipper)
	root := filepath.Join(destination, bundleTopDirectory)
	seen := make(map[string]struct{})
	files := 0
	entries := 0
	var total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read bundle archive: %w", err)
		}
		entries++
		if entries > maxBundleEntries {
			return "", fmt.Errorf("bundle archive exceeds %d entries", maxBundleEntries)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == bundleTopDirectory && header.Typeflag == tar.TypeDir {
			if err := ensureDirectory(root, 0o755); err != nil {
				return "", err
			}
			continue
		}
		prefix := bundleTopDirectory + "/"
		if !strings.HasPrefix(name, prefix) {
			return "", fmt.Errorf("unsafe bundle archive entry %q", header.Name)
		}
		relative := strings.TrimPrefix(name, prefix)
		if err := validateBundleRelativePath(relative); err != nil {
			return "", fmt.Errorf("unsafe bundle archive entry %q: %w", header.Name, err)
		}
		if _, duplicate := seen[relative]; duplicate {
			return "", fmt.Errorf("bundle archive repeats entry %q", header.Name)
		}
		seen[relative] = struct{}{}
		target := filepath.Join(root, filepath.FromSlash(relative))
		if !pathWithin(root, target) {
			return "", fmt.Errorf("unsafe bundle archive entry %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Mode&0o777 != 0o755 {
				return "", fmt.Errorf("bundle directory %q has unsupported mode", header.Name)
			}
			if err := ensureDirectory(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			files++
			if files > maxBundleFiles || header.Size <= 0 || header.Size > maxBundleFileBytes || total > maxBundleTotalBytes-header.Size {
				return "", fmt.Errorf("bundle archive exceeds file or size limits")
			}
			total += header.Size
			mode := fs.FileMode(header.Mode & 0o777)
			if mode != 0o644 && mode != 0o755 {
				return "", fmt.Errorf("bundle file %q has unsupported mode %04o", header.Name, mode)
			}
			if err := ensureDirectory(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return "", err
			}
			written, copyErr := io.CopyN(output, archive, header.Size)
			syncErr := output.Sync()
			closeErr := output.Close()
			if copyErr != nil || written != header.Size || syncErr != nil || closeErr != nil {
				return "", errors.Join(copyErr, syncErr, closeErr)
			}
		default:
			return "", fmt.Errorf("bundle archive entry %q has unsupported type", header.Name)
		}
	}
	if err := requireRealDirectory(root); err != nil {
		return "", fmt.Errorf("bundle archive is missing %s root: %w", bundleTopDirectory, err)
	}
	return root, nil
}

func copyBundleToGeneration(bundle *validatedBundle, generations string) (string, bool, error) {
	destination := filepath.Join(generations, bundle.GenerationID)
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", false, fmt.Errorf("generation path %s is unsafe", destination)
		}
		existing, err := validateBundleRoot(destination, bundle.Manifest.Architecture)
		if err != nil || existing.Identity != bundle.Identity {
			return "", false, fmt.Errorf("existing generation %s does not match bundle identity", bundle.GenerationID)
		}
		return destination, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if err := ensureDirectory(generations, 0o755); err != nil {
		return "", false, err
	}
	stage, err := os.MkdirTemp(generations, ".stage-*")
	if err != nil {
		return "", false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
		}
	}()
	for _, file := range bundle.Manifest.Files {
		mode, _ := strconv.ParseUint(file.Mode, 8, 32)
		if err := copyBundleFile(bundle.Root, stage, file.Path, fs.FileMode(mode)); err != nil {
			return "", false, err
		}
	}
	if err := copyBundleFile(bundle.Root, stage, "release-manifest.json", 0o644); err != nil {
		return "", false, err
	}
	// The caller may have supplied a mutable bundle-root directory. Revalidate
	// the completed private generation before making it visible; this closes the
	// validate-then-copy window without requiring the caller to own the source.
	if copied, err := validateBundleRoot(stage, bundle.Manifest.Architecture); err != nil || copied.Identity != bundle.Identity {
		if err != nil {
			return "", false, fmt.Errorf("revalidate copied generation: %w", err)
		}
		return "", false, fmt.Errorf("revalidate copied generation: identity mismatch")
	}
	if err := syncTreeDirectories(stage); err != nil {
		return "", false, err
	}
	if err := os.Rename(stage, destination); err != nil {
		return "", false, err
	}
	committed = true
	if err := syncDirectory(generations); err != nil {
		return "", false, err
	}
	return destination, true, nil
}

func copyBundleFile(sourceRoot, destinationRoot, relative string, mode fs.FileMode) error {
	if err := validateBundleRelativePath(relative); err != nil {
		return err
	}
	source := filepath.Join(sourceRoot, filepath.FromSlash(relative))
	destination := filepath.Join(destinationRoot, filepath.FromSlash(relative))
	data, err := readRegularFile(source, maxBundleFileBytes)
	if err != nil {
		return err
	}
	if err := ensureDirectory(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncTreeDirectories(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

type cacheInfo struct {
	Path   string
	SHA256 string
	Kind   string
}

func cacheBundle(bundle *validatedBundle, cacheDirectory string) (cacheInfo, bool, error) {
	if err := ensureDirectory(cacheDirectory, 0o700); err != nil {
		return cacheInfo{}, false, err
	}
	destination := filepath.Join(cacheDirectory, bundle.Identity+".tar.gz")
	if _, err := os.Lstat(destination); err == nil {
		digest, _, digestErr := digestFile(destination, maxBundleArchive)
		if digestErr != nil {
			return cacheInfo{}, false, digestErr
		}
		cached, openErr := openBundle(BundleInput{Archive: destination, SHA256: digest}, bundle.Manifest.Architecture)
		if openErr != nil {
			return cacheInfo{}, false, fmt.Errorf("cached bundle is invalid: %w", openErr)
		}
		defer cached.Close()
		if cached.Identity != bundle.Identity {
			return cacheInfo{}, false, fmt.Errorf("cached bundle identity mismatch")
		}
		if bundle.CacheKind == "verified-archive" && digest != bundle.ArchiveSHA256 {
			cached.Close()
			if err := removeAndSync(destination); err != nil {
				return cacheInfo{}, false, err
			}
			if err := atomicCopyStreaming(bundle.Archive, destination, 0o600, maxBundleArchive, bundle.ArchiveSHA256); err != nil {
				return cacheInfo{}, false, err
			}
			return cacheInfo{Path: destination, SHA256: bundle.ArchiveSHA256, Kind: "verified-archive"}, true, nil
		}
		kind := "root-snapshot"
		if bundle.CacheKind == "verified-archive" && digest == bundle.ArchiveSHA256 {
			kind = "verified-archive"
		}
		return cacheInfo{Path: destination, SHA256: digest, Kind: kind}, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return cacheInfo{}, false, err
	}
	if bundle.Archive != "" {
		if err := atomicCopyStreaming(bundle.Archive, destination, 0o600, maxBundleArchive, bundle.ArchiveSHA256); err != nil {
			return cacheInfo{}, false, err
		}
	} else if err := writeBundleCacheArchive(bundle, destination); err != nil {
		return cacheInfo{}, false, err
	}
	digest, _, err := digestFile(destination, maxBundleArchive)
	if err != nil {
		return cacheInfo{}, false, err
	}
	kind := bundle.CacheKind
	if kind == "" {
		kind = "root-snapshot"
	}
	return cacheInfo{Path: destination, SHA256: digest, Kind: kind}, true, nil
}

func writeBundleCacheArchive(bundle *validatedBundle, destination string) error {
	if err := ensureDirectory(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".bundle-cache-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	zipper := gzip.NewWriter(temporary)
	archive := tar.NewWriter(zipper)
	epoch := time.Unix(bundle.Manifest.SourceDateEpoch, 0).UTC()
	if err := archive.WriteHeader(&tar.Header{Name: bundleTopDirectory + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: epoch}); err != nil {
		return err
	}
	files := append([]manifestFile(nil), bundle.Manifest.Files...)
	files = append(files, manifestFile{Path: "release-manifest.json", Mode: "0644", Size: int64(len(bundle.ManifestRaw)), SHA256: bundle.Identity})
	for _, metadata := range files {
		data, err := readRegularFile(filepath.Join(bundle.Root, filepath.FromSlash(metadata.Path)), maxBundleFileBytes)
		if err != nil {
			return err
		}
		if int64(len(data)) != metadata.Size || digestBytes(data) != metadata.SHA256 {
			return fmt.Errorf("bundle source file %q changed after validation", metadata.Path)
		}
		mode, _ := strconv.ParseInt(metadata.Mode, 8, 64)
		header := &tar.Header{Name: path.Join(bundleTopDirectory, metadata.Path), Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: epoch}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if _, err := archive.Write(data); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := zipper.Close(); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return syncDirectory(filepath.Dir(destination))
}

func manifestSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
