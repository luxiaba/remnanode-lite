package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxProjectBinaryBytes = 128 << 20
	maxSupportBytes       = 16 << 20
	maxTextInputBytes     = 2 << 20
	maxBuilderOutputBytes = 1 << 20
)

type buildOptions struct {
	lockPath         string
	architecture     string
	version          string
	contractVersion  string
	sourceRevision   string
	sourceDateEpoch  int64
	projectRoot      string
	nodePath         string
	rnlctlPath       string
	asnBuilderPath   string
	installerPath    string
	supportDirectory string
	cacheDirectory   string
	outputPath       string
	offline          bool
}

func buildBundle(ctx context.Context, options buildOptions) error {
	if err := options.validate(); err != nil {
		return err
	}
	payloads, err := resolveRuntimePayloads(ctx, options.lockPath, options.architecture,
		options.asnBuilderPath, options.cacheDirectory, options.offline)
	if err != nil {
		return err
	}
	lock := payloads.document.Lock
	architecture := payloads.architecture

	node, err := readRegularInput(options.nodePath, maxProjectBinaryBytes, true)
	if err != nil {
		return fmt.Errorf("read remnanode-lite binary: %w", err)
	}
	rnlctl, err := readRegularInput(options.rnlctlPath, maxProjectBinaryBytes, true)
	if err != nil {
		return fmt.Errorf("read rnlctl binary: %w", err)
	}
	if err := validateELFArchitecture("remnanode-lite", node, options.architecture); err != nil {
		return err
	}
	if err := validateELFArchitecture("rnlctl", rnlctl, options.architecture); err != nil {
		return err
	}
	installer, err := readRegularInput(options.installerPath, maxTextInputBytes, true)
	if err != nil {
		return fmt.Errorf("read Native installer: %w", err)
	}
	support, err := collectSupportFiles(options.supportDirectory)
	if err != nil {
		return err
	}

	projectLicense, err := readRegularInput(filepath.Join(options.projectRoot, "LICENSE"), maxTextInputBytes, false)
	if err != nil {
		return fmt.Errorf("read project LICENSE: %w", err)
	}
	thirdPartyNotices, err := readRegularInput(filepath.Join(options.projectRoot, "release", "bundle", "THIRD_PARTY_NOTICES.md"), maxTextInputBytes, false)
	if err != nil {
		return fmt.Errorf("read third-party notices: %w", err)
	}
	if err := validateThirdPartyNotices(lock, thirdPartyNotices); err != nil {
		return err
	}
	sourceOffer, err := readRegularInput(filepath.Join(options.projectRoot, "release", "bundle", "SOURCE-OFFER.md"), maxTextInputBytes, false)
	if err != nil {
		return fmt.Errorf("read source offer: %w", err)
	}
	if err := validateSourceOffer(lock, sourceOffer); err != nil {
		return err
	}

	files := []bundleFile{
		{Path: "LICENSE", Mode: 0o644, Data: projectLicense, Role: "project-license", License: "AGPL-3.0-only"},
		{Path: "SOURCE-OFFER.md", Mode: 0o644, Data: sourceOffer, Role: "source-offer", License: "AGPL-3.0-only"},
		{Path: "THIRD_PARTY_NOTICES.md", Mode: 0o644, Data: thirdPartyNotices, Role: "third-party-notices", License: "NOASSERTION"},
		{Path: "bin/remnanode-lite", Mode: 0o755, Data: node, Role: "node-binary", License: "AGPL-3.0-only"},
		{Path: "bin/rnlctl", Mode: 0o755, Data: rnlctl, Role: "administration-cli", License: "AGPL-3.0-only"},
		{Path: "install.sh", Mode: 0o755, Data: installer, Role: "installer", License: "AGPL-3.0-only"},
		{Path: "lib/rw-core", Mode: 0o755, Data: payloads.core, Role: "runtime-core", License: architecture.Core.License},
		{Path: "runtime-assets.lock.json", Mode: 0o644, Data: payloads.document.Data, Role: "runtime-lock", License: "AGPL-3.0-only"},
		{Path: "share/asn/asn-prefixes.bin", Mode: 0o644, Data: payloads.asn, Role: "runtime-data", License: lock.ASN.Output.License},
		{Path: "share/xray/geoip.dat", Mode: 0o644, Data: payloads.geoIP, Role: "runtime-data", License: lock.GeoIP.License},
		{Path: "share/xray/geosite.dat", Mode: 0o644, Data: payloads.geoSite, Role: "runtime-data", License: lock.GeoSite.License},
	}
	files = append(files, support...)

	for _, identifier := range sortedLicenseIDs(lock) {
		files = append(files, bundleFile{
			Path: "licenses/" + identifier + ".txt", Mode: 0o644, Data: payloads.licenses[identifier],
			Role: "third-party-license", License: identifier,
		})
	}

	sortBundleFiles(files)
	goModules, err := collectGoModules(files)
	if err != nil {
		return fmt.Errorf("collect Go module dependencies: %w", err)
	}
	spdx, err := marshalDeterministicJSON(buildSPDX(options, lock, files, goModules))
	if err != nil {
		return fmt.Errorf("generate SPDX SBOM: %w", err)
	}
	files = append(files, bundleFile{
		Path: "SBOM.spdx.json", Mode: 0o644, Data: spdx,
		Role: "sbom", License: "CC0-1.0",
	})
	sortBundleFiles(files)

	manifest, err := buildManifest(options, lock, payloads.document.SHA256, architecture, files)
	if err != nil {
		return fmt.Errorf("generate release manifest: %w", err)
	}
	manifestJSON, err := marshalDeterministicJSON(manifest)
	if err != nil {
		return fmt.Errorf("encode release manifest: %w", err)
	}
	files = append(files, bundleFile{
		Path: "release-manifest.json", Mode: 0o644, Data: manifestJSON,
		Role: "manifest", License: "CC0-1.0",
	})
	sortBundleFiles(files)

	if err := writeDeterministicArchive(options.outputPath, options.sourceDateEpoch, files); err != nil {
		return err
	}
	verify := verifyOptions{
		lockPath: options.lockPath, archivePath: options.outputPath,
		architecture: options.architecture, version: options.version,
		contractVersion: options.contractVersion, sourceRevision: options.sourceRevision,
	}
	if err := verifyBundle(verify); err != nil {
		_ = os.Remove(options.outputPath)
		return fmt.Errorf("verify generated bundle: %w", err)
	}
	return nil
}

func (options buildOptions) validate() error {
	if options.lockPath == "" || options.projectRoot == "" || options.nodePath == "" ||
		options.rnlctlPath == "" || options.asnBuilderPath == "" || options.installerPath == "" ||
		options.supportDirectory == "" || options.cacheDirectory == "" || options.outputPath == "" {
		return fmt.Errorf("build requires lock, project root, binaries, installer, support directory, cache, and output paths")
	}
	if options.architecture != "amd64" && options.architecture != "arm64" {
		return fmt.Errorf("unsupported architecture %q", options.architecture)
	}
	if err := validateVersionPair(options.version, options.contractVersion); err != nil {
		return err
	}
	if !gitCommitPattern.MatchString(options.sourceRevision) {
		return fmt.Errorf("source revision must be a 40-character lowercase Git commit")
	}
	if options.sourceDateEpoch <= 0 || options.sourceDateEpoch > time.Now().Add(24*time.Hour).Unix() {
		return fmt.Errorf("source date epoch is outside the supported range")
	}
	if info, err := os.Lstat(options.outputPath); err == nil {
		return fmt.Errorf("output path already exists: %s (%s)", options.outputPath, info.Mode())
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output path: %w", err)
	}
	return nil
}

func readRegularInput(filePath string, maxBytes int64, requireExecutable bool) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular non-symlink file", filePath)
	}
	if requireExecutable && info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not executable", filePath)
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("%s size %d is outside 1..%d bytes", filePath, info.Size(), maxBytes)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != info.Size() {
		return nil, fmt.Errorf("%s changed while being read", filePath)
	}
	return data, nil
}

type supportFileSpec struct {
	path string
	mode int64
	role string
}

var nativeSupportContract = []supportFileSpec{
	{path: "deploy/node.env.example", mode: 0o644, role: "configuration-template"},
	{path: "deploy/remnanode-lite-hardening.conf", mode: 0o644, role: "service-hardening-template"},
	{path: "deploy/remnanode-lite.openrc", mode: 0o755, role: "openrc-service-template"},
	{path: "deploy/remnanode-lite.service", mode: 0o644, role: "systemd-service-template"},
}

func collectSupportFiles(directory string) ([]bundleFile, error) {
	rootInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect support directory: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("support directory must be a real directory")
	}

	contract := make(map[string]supportFileSpec, len(nativeSupportContract))
	for _, spec := range nativeSupportContract {
		contract[spec.path] = spec
	}
	files := make([]bundleFile, 0, len(nativeSupportContract))
	var totalBytes int64
	seen := make(map[string]struct{}, len(nativeSupportContract))
	err = filepath.WalkDir(directory, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == directory {
			return nil
		}
		relative, err := filepath.Rel(directory, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if err := validateArchiveRelativePath(relative, 384); err != nil {
			return fmt.Errorf("invalid support path %q: %w", relative, err)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("support path %q is a symlink", relative)
		}
		if info.IsDir() {
			if relative != "deploy" {
				return fmt.Errorf("support directory contains unexpected directory %q", relative)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("support path %q is not a regular file", relative)
		}
		spec, allowed := contract[relative]
		if !allowed {
			return fmt.Errorf("support directory contains unexpected file %q", relative)
		}
		if info.Size() <= 0 || info.Size() > maxTextInputBytes || totalBytes > maxSupportBytes-info.Size() {
			return fmt.Errorf("support path %q exceeds the support size budget", relative)
		}
		data, err := readRegularInput(current, maxTextInputBytes, false)
		if err != nil {
			return err
		}
		totalBytes += int64(len(data))
		if int64(info.Mode().Perm()) != spec.mode {
			return fmt.Errorf("support path %q mode is %04o, want %04o", relative, info.Mode().Perm(), spec.mode)
		}
		files = append(files, bundleFile{
			Path: "support/" + relative, Mode: spec.mode, Data: data,
			Role: spec.role, License: "AGPL-3.0-only",
		})
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect support directory: %w", err)
	}
	for _, required := range nativeSupportContract {
		if _, present := seen[required.path]; !present {
			return nil, fmt.Errorf("support directory is missing %s", required.path)
		}
	}
	sortBundleFiles(files)
	return files, nil
}

func buildASNDatabase(ctx context.Context, builderPath, sourcePath string, expected outputLock) ([]byte, error) {
	if _, err := readRegularInput(builderPath, maxProjectBinaryBytes, true); err != nil {
		return nil, fmt.Errorf("inspect ASN builder: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp("", "remnanode-asn-build-*")
	if err != nil {
		return nil, fmt.Errorf("create ASN build directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	outputPath := filepath.Join(temporaryDirectory, "asn-prefixes.bin")

	commandContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandContext, builderPath,
		"-format", "ipverse-tar-gz", "-in", sourcePath, "-out", outputPath)
	var output boundedBuffer
	output.limit = maxBuilderOutputBytes
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("run ASN builder: %w: %s", err, strings.TrimSpace(output.String()))
	}
	if commandContext.Err() != nil {
		return nil, fmt.Errorf("run ASN builder: %w", commandContext.Err())
	}
	data, err := readRegularInput(outputPath, 64<<20, false)
	if err != nil {
		return nil, fmt.Errorf("read generated ASN database: %w", err)
	}
	if int64(len(data)) != expected.Size || digestBytes(data) != expected.SHA256 {
		return nil, fmt.Errorf("generated ASN database does not match locked size and SHA-256")
	}
	return data, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = buffer.Buffer.Write(data[:remaining])
		return len(data), nil
	}
	return buffer.Buffer.Write(data)
}

func sortBundleFiles(files []bundleFile) {
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
}

func writeDeterministicArchive(outputPath string, epoch int64, files []bundleFile) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".native-bundle-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create output archive: %w", err)
	}
	temporaryPath := temporary.Name()
	published := false
	defer func() {
		_ = temporary.Close()
		if !published {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("set output archive permissions: %w", err)
	}

	zipper, err := gzip.NewWriterLevel(temporary, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip stream: %w", err)
	}
	modificationTime := time.Unix(epoch, 0).UTC()
	zipper.Header.ModTime = modificationTime
	zipper.Header.OS = 255
	archive := tar.NewWriter(zipper)

	directories := bundleDirectories(files)
	for _, directory := range directories {
		header := &tar.Header{
			Name: directory, Mode: 0o755, Typeflag: tar.TypeDir,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			ModTime: modificationTime, Format: tar.FormatUSTAR,
		}
		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("write archive directory %s: %w", directory, err)
		}
	}
	for _, file := range files {
		header := &tar.Header{
			Name: path.Join(bundleName, file.Path), Mode: file.Mode, Size: int64(len(file.Data)),
			Typeflag: tar.TypeReg, Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			ModTime: modificationTime, Format: tar.FormatUSTAR,
		}
		if err := archive.WriteHeader(header); err != nil {
			return fmt.Errorf("write archive file %s: %w", file.Path, err)
		}
		if _, err := archive.Write(file.Data); err != nil {
			return fmt.Errorf("write archive file %s content: %w", file.Path, err)
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish tar archive: %w", err)
	}
	if err := zipper.Close(); err != nil {
		return fmt.Errorf("finish gzip archive: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync output archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close output archive: %w", err)
	}
	if err := os.Link(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish output archive: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("remove temporary output archive: %w", err)
	}
	published = true
	return nil
}

func bundleDirectories(files []bundleFile) []string {
	directories := map[string]struct{}{bundleName + "/": {}}
	for _, file := range files {
		current := path.Dir(path.Join(bundleName, file.Path))
		for current != "." && current != "/" {
			directories[current+"/"] = struct{}{}
			if current == bundleName {
				break
			}
			current = path.Dir(current)
		}
	}
	result := make([]string, 0, len(directories))
	for directory := range directories {
		result = append(result, directory)
	}
	sort.Strings(result)
	return result
}
