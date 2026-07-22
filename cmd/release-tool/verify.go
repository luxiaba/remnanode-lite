package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxBundleCompressedBytes   = 512 << 20
	maxBundleUncompressedBytes = 512 << 20
	maxBundleEntries           = 512
	maxManifestBytes           = 4 << 20
)

var manifestModePattern = regexp.MustCompile(`^0[67][0-7]{2}$`)

type verifyOptions struct {
	lockPath        string
	archivePath     string
	architecture    string
	version         string
	contractVersion string
	sourceRevision  string
}

type archivedEntry struct {
	name    string
	mode    int64
	size    int64
	modTime time.Time
	isDir   bool
	data    []byte
}

func verifyBundle(options verifyOptions) error {
	lockDocument, err := loadRuntimeLockDocument(options.lockPath)
	if err != nil {
		return err
	}
	lock := lockDocument.Lock
	architecture, err := lock.xrayForArchitecture(options.architecture)
	if err != nil {
		return err
	}
	if options.version != "" {
		if err := validateProjectVersion(options.version); err != nil {
			return fmt.Errorf("invalid expected project version: %w", err)
		}
	}
	if options.contractVersion != "" {
		if err := validateContractVersion(options.contractVersion); err != nil {
			return fmt.Errorf("invalid expected contract version: %w", err)
		}
	}
	if options.sourceRevision != "" && !gitCommitPattern.MatchString(options.sourceRevision) {
		return fmt.Errorf("invalid expected source revision")
	}

	entries, err := readBundleArchive(options.archivePath)
	if err != nil {
		return err
	}
	manifestEntry, exists := entries[bundleName+"/release-manifest.json"]
	if !exists || manifestEntry.isDir {
		return fmt.Errorf("bundle is missing release-manifest.json")
	}
	if manifestEntry.mode != 0o644 {
		return fmt.Errorf("release-manifest.json mode is %04o, want 0644", manifestEntry.mode)
	}
	manifest, err := decodeReleaseManifest(manifestEntry.data)
	if err != nil {
		return err
	}
	if err := validateReleaseManifest(manifest, options, lock, lockDocument.SHA256, architecture); err != nil {
		return err
	}
	if err := validateManifestFiles(manifest, entries, lock, architecture); err != nil {
		return err
	}
	if err := validateLockedPayloadFiles(manifest, lockDocument, architecture); err != nil {
		return err
	}
	if err := validateArchiveDirectories(manifest, entries); err != nil {
		return err
	}
	epoch := time.Unix(manifest.SourceDateEpoch, 0).UTC()
	for _, entry := range entries {
		if !entry.modTime.Equal(epoch) {
			return fmt.Errorf("archive entry %q timestamp %s does not match sourceDateEpoch", entry.name, entry.modTime.UTC())
		}
	}

	manifestFiles := make(map[string]manifestFile, len(manifest.Files))
	for _, file := range manifest.Files {
		manifestFiles[file.Path] = file
	}
	sbomEntry, exists := entries[bundleName+"/SBOM.spdx.json"]
	if !exists || sbomEntry.isDir {
		return fmt.Errorf("bundle is missing SBOM.spdx.json")
	}
	spdx, err := decodeSPDX(sbomEntry.data)
	if err != nil {
		return err
	}
	spdxInputs, err := sbomInputFiles(manifest, entries)
	if err != nil {
		return err
	}
	if err := validateBundleArchitectures(entries, manifest.Architecture); err != nil {
		return err
	}
	goModules, err := collectGoModules(spdxInputs)
	if err != nil {
		return fmt.Errorf("collect Go module dependencies from bundle: %w", err)
	}
	wantSPDX := buildSPDX(buildOptions{
		version: manifest.Version, contractVersion: manifest.ContractVersion,
		architecture: manifest.Architecture, sourceRevision: manifest.SourceRevision,
		sourceDateEpoch: manifest.SourceDateEpoch,
	}, lock, spdxInputs, goModules)
	if !reflect.DeepEqual(spdx, wantSPDX) {
		return fmt.Errorf("SPDX SBOM identity, packages, relationships, or file metadata do not match the bundle")
	}
	if err := validateSPDX(spdx, manifestFiles, entries, goModules); err != nil {
		return err
	}
	return nil
}

func validateLockedPayloadFiles(manifest releaseManifest, document runtimeLockDocument, architecture xrayArchitecture) error {
	lock := document.Lock
	files := make(map[string]manifestFile, len(manifest.Files))
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	type lockedPayload struct {
		path    string
		sha256  string
		size    int64
		mode    string
		role    string
		license string
	}
	wanted := []lockedPayload{
		{path: "lib/rw-core", sha256: architecture.Core.SHA256, size: architecture.Core.Size, mode: "0755", role: "runtime-core", license: architecture.Core.License},
		{path: "runtime-assets.lock.json", sha256: document.SHA256, size: int64(len(document.Data)), mode: "0644", role: "runtime-lock", license: "AGPL-3.0-only"},
		{path: "share/xray/geoip.dat", sha256: lock.GeoIP.Artifact.SHA256, size: lock.GeoIP.Artifact.Size, mode: "0644", role: "runtime-data", license: lock.GeoIP.License},
		{path: "share/xray/geosite.dat", sha256: lock.GeoSite.Artifact.SHA256, size: lock.GeoSite.Artifact.Size, mode: "0644", role: "runtime-data", license: lock.GeoSite.License},
		{path: "share/asn/asn-prefixes.bin", sha256: lock.ASN.Output.SHA256, size: lock.ASN.Output.Size, mode: "0644", role: "runtime-data", license: lock.ASN.Output.License},
	}
	licenseIDs := make([]string, 0, len(lock.licenseArtifacts()))
	for identifier := range lock.licenseArtifacts() {
		licenseIDs = append(licenseIDs, identifier)
	}
	sort.Strings(licenseIDs)
	for _, identifier := range licenseIDs {
		artifact := lock.licenseArtifacts()[identifier]
		wanted = append(wanted, lockedPayload{
			path: "licenses/" + identifier + ".txt", sha256: artifact.SHA256,
			size: artifact.Size, mode: "0644", role: "third-party-license", license: identifier,
		})
	}
	for _, expected := range wanted {
		file, exists := files[expected.path]
		if !exists {
			return fmt.Errorf("manifest is missing locked payload %q", expected.path)
		}
		if file.SHA256 != expected.sha256 || file.Size != expected.size || file.Mode != expected.mode ||
			file.Role != expected.role || file.License != expected.license {
			return fmt.Errorf("manifest payload %q does not match the runtime asset lock", expected.path)
		}
	}
	return nil
}

func validateBundleArchitectures(entries map[string]archivedEntry, architecture string) error {
	for _, binary := range []struct {
		path string
		name string
	}{
		{path: "bin/remnanode-lite", name: "remnanode-lite"},
		{path: "bin/rnlctl", name: "rnlctl"},
		{path: "lib/rw-core", name: "rw-core"},
	} {
		entry, exists := entries[path.Join(bundleName, binary.path)]
		if !exists || entry.isDir {
			return fmt.Errorf("bundle is missing %s", binary.path)
		}
		if err := validateELFArchitecture(binary.name, entry.data, architecture); err != nil {
			return err
		}
	}
	return nil
}

func sbomInputFiles(manifest releaseManifest, entries map[string]archivedEntry) ([]bundleFile, error) {
	files := make([]bundleFile, 0, len(manifest.Files)-1)
	for _, manifestEntry := range manifest.Files {
		if manifestEntry.Path == "SBOM.spdx.json" {
			continue
		}
		entry, exists := entries[path.Join(bundleName, manifestEntry.Path)]
		if !exists || entry.isDir {
			return nil, fmt.Errorf("cannot reconstruct SBOM input %q", manifestEntry.Path)
		}
		mode, err := strconv.ParseInt(manifestEntry.Mode, 8, 64)
		if err != nil {
			return nil, fmt.Errorf("parse SBOM input mode for %q: %w", manifestEntry.Path, err)
		}
		files = append(files, bundleFile{
			Path: manifestEntry.Path, Mode: mode, Data: entry.data,
			Role: manifestEntry.Role, License: manifestEntry.License,
		})
	}
	sortBundleFiles(files)
	return files, nil
}

func readBundleArchive(archivePath string) (map[string]archivedEntry, error) {
	info, err := os.Lstat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("inspect bundle archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bundle archive is not a regular non-symlink file")
	}
	if info.Size() <= 0 || info.Size() > maxBundleCompressedBytes {
		return nil, fmt.Errorf("bundle archive compressed size %d is outside 1..%d", info.Size(), maxBundleCompressedBytes)
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open bundle archive: %w", err)
	}
	defer file.Close()
	compressed := io.LimitReader(file, maxBundleCompressedBytes+1)
	zipper, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("open bundle gzip stream: %w", err)
	}
	defer zipper.Close()
	archive := tar.NewReader(zipper)
	entries := make(map[string]archivedEntry)
	var totalBytes int64
	for count := 0; ; count++ {
		if count >= maxBundleEntries {
			return nil, fmt.Errorf("bundle archive exceeds %d entries", maxBundleEntries)
		}
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read bundle archive: %w", err)
		}
		if err := validateArchiveRelativePath(header.Name, 512); err != nil {
			return nil, fmt.Errorf("unsafe bundle archive entry %q: %w", header.Name, err)
		}
		cleanName := strings.TrimSuffix(header.Name, "/")
		if cleanName != bundleName && !strings.HasPrefix(cleanName, bundleName+"/") {
			return nil, fmt.Errorf("bundle entry %q is outside the %s top-level directory", header.Name, bundleName)
		}
		if _, duplicate := entries[cleanName]; duplicate {
			return nil, fmt.Errorf("bundle archive contains duplicate entry %q", cleanName)
		}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
			return nil, fmt.Errorf("bundle entry %q has non-canonical ownership", header.Name)
		}

		entry := archivedEntry{
			name: cleanName, mode: header.Mode, size: header.Size,
			modTime: header.ModTime.UTC(),
		}
		switch header.Typeflag {
		case tar.TypeDir:
			entry.isDir = true
			if header.Mode != 0o755 || header.Size != 0 || !strings.HasSuffix(header.Name, "/") {
				return nil, fmt.Errorf("bundle directory %q has non-canonical metadata", header.Name)
			}
		case tar.TypeReg, tar.TypeRegA:
			if strings.HasSuffix(header.Name, "/") || header.Size <= 0 || header.Size > maxBundleUncompressedBytes {
				return nil, fmt.Errorf("bundle file %q has invalid size or path", header.Name)
			}
			if totalBytes > maxBundleUncompressedBytes-header.Size {
				return nil, fmt.Errorf("bundle uncompressed payload exceeds %d bytes", maxBundleUncompressedBytes)
			}
			totalBytes += header.Size
			data, err := io.ReadAll(io.LimitReader(archive, header.Size+1))
			if err != nil {
				return nil, fmt.Errorf("read bundle file %q: %w", header.Name, err)
			}
			if int64(len(data)) != header.Size {
				return nil, fmt.Errorf("bundle file %q is truncated", header.Name)
			}
			entry.data = data
		default:
			return nil, fmt.Errorf("bundle entry %q uses forbidden tar type %d", header.Name, header.Typeflag)
		}
		entries[cleanName] = entry
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("bundle archive is empty")
	}
	return entries, nil
}

func decodeReleaseManifest(data []byte) (releaseManifest, error) {
	if len(data) == 0 || len(data) > maxManifestBytes {
		return releaseManifest{}, fmt.Errorf("release manifest size is invalid")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func decodeSPDX(data []byte) (spdxDocument, error) {
	if len(data) == 0 || len(data) > maxManifestBytes {
		return spdxDocument{}, fmt.Errorf("SPDX SBOM size is invalid")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return spdxDocument{}, fmt.Errorf("decode SPDX SBOM: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document spdxDocument
	if err := decoder.Decode(&document); err != nil {
		return spdxDocument{}, fmt.Errorf("decode SPDX SBOM: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return spdxDocument{}, fmt.Errorf("decode SPDX SBOM: %w", err)
	}
	return document, nil
}

func validateReleaseManifest(manifest releaseManifest, options verifyOptions, lock runtimeLock, lockSHA256 string, architecture xrayArchitecture) error {
	if manifest.SchemaVersion != manifestSchemaVersion || manifest.Name != bundleName || manifest.OS != bundleOS {
		return fmt.Errorf("release manifest has unsupported identity or schema")
	}
	if manifest.Architecture != options.architecture {
		return fmt.Errorf("release manifest architecture %q does not match %q", manifest.Architecture, options.architecture)
	}
	if err := validateVersionPair(manifest.Version, manifest.ContractVersion); err != nil {
		return fmt.Errorf("release manifest version metadata: %w", err)
	}
	if !gitCommitPattern.MatchString(manifest.SourceRevision) || manifest.SourceDateEpoch <= 0 {
		return fmt.Errorf("release manifest contains invalid revision or epoch metadata")
	}
	if options.version != "" && manifest.Version != options.version {
		return fmt.Errorf("release manifest version %q does not match %q", manifest.Version, options.version)
	}
	if options.contractVersion != "" && manifest.ContractVersion != options.contractVersion {
		return fmt.Errorf("release manifest contract version %q does not match %q", manifest.ContractVersion, options.contractVersion)
	}
	if options.sourceRevision != "" && manifest.SourceRevision != options.sourceRevision {
		return fmt.Errorf("release manifest source revision does not match expected revision")
	}
	if manifest.RuntimeAssetLockSHA256 != lockSHA256 {
		return fmt.Errorf("release manifest runtime asset lock digest does not match the supplied lock")
	}
	wantRuntime := manifestRuntimeAssets{
		Xray: manifestXray{
			Version: lock.Xray.Version, Commit: lock.Xray.Commit, SourceURL: lock.Xray.SourceURL,
			Archive: architecture.Archive, Core: runtimePayload(architecture.Core),
		},
		GeoIP: lock.GeoIP, GeoSite: lock.GeoSite,
		ASN: manifestASN{Commit: lock.ASN.Commit, Source: lock.ASN.Source, Output: lock.ASN.Output},
	}
	if !reflect.DeepEqual(manifest.RuntimeAssets, wantRuntime) {
		return fmt.Errorf("release manifest runtime asset provenance does not match the lock")
	}
	return nil
}

func validateManifestFiles(manifest releaseManifest, entries map[string]archivedEntry, lock runtimeLock, architecture xrayArchitecture) error {
	type requiredMetadata struct {
		mode    string
		role    string
		license string
	}
	required := map[string]requiredMetadata{
		"LICENSE":                                      {mode: "0644", role: "project-license", license: "AGPL-3.0-only"},
		"SOURCE-OFFER.md":                              {mode: "0644", role: "source-offer", license: "AGPL-3.0-only"},
		"THIRD_PARTY_NOTICES.md":                       {mode: "0644", role: "third-party-notices", license: "NOASSERTION"},
		"SBOM.spdx.json":                               {mode: "0644", role: "sbom", license: "CC0-1.0"},
		"runtime-assets.lock.json":                     {mode: "0644", role: "runtime-lock", license: "AGPL-3.0-only"},
		"bin/remnanode-lite":                           {mode: "0755", role: "node-binary", license: "AGPL-3.0-only"},
		"bin/rnlctl":                                   {mode: "0755", role: "administration-cli", license: "AGPL-3.0-only"},
		"install.sh":                                   {mode: "0755", role: "installer", license: "AGPL-3.0-only"},
		"lib/rw-core":                                  {mode: "0755", role: "runtime-core", license: architecture.Core.License},
		"share/asn/asn-prefixes.bin":                   {mode: "0644", role: "runtime-data", license: lock.ASN.Output.License},
		"share/xray/geoip.dat":                         {mode: "0644", role: "runtime-data", license: lock.GeoIP.License},
		"share/xray/geosite.dat":                       {mode: "0644", role: "runtime-data", license: lock.GeoSite.License},
		"support/deploy/node.env.example":              {mode: "0644", role: "configuration-template", license: "AGPL-3.0-only"},
		"support/deploy/remnanode-lite.service":        {mode: "0644", role: "systemd-service-template", license: "AGPL-3.0-only"},
		"support/deploy/remnanode-lite-hardening.conf": {mode: "0644", role: "service-hardening-template", license: "AGPL-3.0-only"},
		"support/deploy/remnanode-lite.openrc":         {mode: "0755", role: "openrc-service-template", license: "AGPL-3.0-only"},
	}
	for _, identifier := range sortedLicenseIDs(lock) {
		required["licenses/"+identifier+".txt"] = requiredMetadata{
			mode: "0644", role: "third-party-license", license: identifier,
		}
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	lastPath := ""
	for _, file := range manifest.Files {
		if err := validateArchiveRelativePath(file.Path, 512); err != nil {
			return fmt.Errorf("manifest file path %q is invalid: %w", file.Path, err)
		}
		if file.Path <= lastPath {
			return fmt.Errorf("manifest file entries are not in strict path order at %q", file.Path)
		}
		lastPath = file.Path
		if file.Path == "release-manifest.json" {
			return fmt.Errorf("release manifest must not contain a self-referential checksum")
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return fmt.Errorf("manifest contains duplicate file %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		expected, allowed := required[file.Path]
		if !allowed {
			return fmt.Errorf("manifest contains unsupported bundle file %q", file.Path)
		}
		if !manifestModePattern.MatchString(file.Mode) || file.Size <= 0 || !hexDigestPattern.MatchString(file.SHA256) || file.Role == "" || file.License == "" {
			return fmt.Errorf("manifest file %q has invalid metadata", file.Path)
		}
		mode, err := strconv.ParseInt(file.Mode, 8, 64)
		if err != nil {
			return fmt.Errorf("manifest file %q has invalid mode: %w", file.Path, err)
		}
		entry, exists := entries[path.Join(bundleName, file.Path)]
		if !exists || entry.isDir {
			return fmt.Errorf("manifest file %q is missing from archive", file.Path)
		}
		if entry.mode != mode || entry.size != file.Size || digestBytes(entry.data) != file.SHA256 {
			return fmt.Errorf("archive file %q does not match manifest metadata", file.Path)
		}
		if file.Mode != expected.mode || file.Role != expected.role || file.License != expected.license {
			return fmt.Errorf("manifest file %q does not match the required mode, role, or license", file.Path)
		}
	}
	for requiredPath := range required {
		if _, exists := seen[requiredPath]; !exists {
			return fmt.Errorf("manifest is missing required file %q", requiredPath)
		}
	}
	if len(seen) != len(required) {
		return fmt.Errorf("manifest file count does not match the bundle contract")
	}
	regularArchiveFiles := 0
	for name, entry := range entries {
		if entry.isDir {
			continue
		}
		regularArchiveFiles++
		if name == bundleName+"/release-manifest.json" {
			continue
		}
		relative := strings.TrimPrefix(name, bundleName+"/")
		if _, exists := seen[relative]; !exists {
			return fmt.Errorf("archive contains unmanifested file %q", relative)
		}
	}
	if regularArchiveFiles != len(manifest.Files)+1 {
		return fmt.Errorf("archive file count does not match manifest")
	}
	return nil
}

func validateArchiveDirectories(manifest releaseManifest, entries map[string]archivedEntry) error {
	files := make([]bundleFile, 0, len(manifest.Files)+1)
	for _, file := range manifest.Files {
		files = append(files, bundleFile{Path: file.Path})
	}
	files = append(files, bundleFile{Path: "release-manifest.json"})
	wantDirectories := bundleDirectories(files)
	sort.Strings(wantDirectories)
	actualDirectories := make([]string, 0, len(wantDirectories))
	for _, entry := range entries {
		if entry.isDir {
			actualDirectories = append(actualDirectories, entry.name+"/")
		}
	}
	sort.Strings(actualDirectories)
	if !reflect.DeepEqual(actualDirectories, wantDirectories) {
		return fmt.Errorf("archive directory structure does not match manifested files")
	}
	return nil
}
