package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type materializeOptions struct {
	lockPath        string
	architecture    string
	asnBuilderPath  string
	cacheDirectory  string
	outputDirectory string
	offline         bool
}

type runtimePayloadSet struct {
	document     runtimeLockDocument
	architecture xrayArchitecture
	core         []byte
	geoIP        []byte
	geoSite      []byte
	asn          []byte
	licenses     map[string][]byte
}

func resolveRuntimePayloads(ctx context.Context, lockPath, architecture, asnBuilderPath, cacheDirectory string, offline bool) (runtimePayloadSet, error) {
	document, err := loadRuntimeLockDocument(lockPath)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	architectureLock, err := document.Lock.xrayForArchitecture(architecture)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	fetcher := newAssetFetcher(cacheDirectory, offline)
	xrayArchivePath, err := fetcher.fetch(ctx, "Xray "+architecture+" archive", architectureLock.Archive)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	xrayAssets, err := extractXrayAssets(xrayArchivePath, architectureLock)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	core := xrayAssets[architectureLock.Core.ArchivePath]
	if err := validateELFArchitecture("rw-core", core, architecture); err != nil {
		return runtimePayloadSet{}, err
	}
	geoIPPath, err := fetcher.fetch(ctx, "GeoIP data", document.Lock.GeoIP.Artifact)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	geoIP, err := readRegularInput(geoIPPath, 256<<20, false)
	if err != nil {
		return runtimePayloadSet{}, fmt.Errorf("read GeoIP data: %w", err)
	}
	geoSitePath, err := fetcher.fetch(ctx, "GeoSite data", document.Lock.GeoSite.Artifact)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	geoSite, err := readRegularInput(geoSitePath, 256<<20, false)
	if err != nil {
		return runtimePayloadSet{}, fmt.Errorf("read GeoSite data: %w", err)
	}
	asnSourcePath, err := fetcher.fetch(ctx, "ASN source archive", document.Lock.ASN.Source)
	if err != nil {
		return runtimePayloadSet{}, err
	}
	asnDatabase, err := buildASNDatabase(ctx, asnBuilderPath, asnSourcePath, document.Lock.ASN.Output)
	if err != nil {
		return runtimePayloadSet{}, err
	}

	licenses := make(map[string][]byte, len(document.Lock.licenseArtifacts()))
	licenseIDs := sortedLicenseIDs(document.Lock)
	for _, identifier := range licenseIDs {
		artifact := document.Lock.licenseArtifacts()[identifier]
		licensePath, fetchErr := fetcher.fetch(ctx, "license "+identifier, artifact)
		if fetchErr != nil {
			return runtimePayloadSet{}, fetchErr
		}
		licenseData, readErr := readRegularInput(licensePath, 1<<20, false)
		if readErr != nil {
			return runtimePayloadSet{}, fmt.Errorf("read license %s: %w", identifier, readErr)
		}
		licenses[identifier] = licenseData
	}
	return runtimePayloadSet{
		document: document, architecture: architectureLock,
		core:    core,
		geoIP:   geoIP,
		geoSite: geoSite,
		asn:     asnDatabase, licenses: licenses,
	}, nil
}

func sortedLicenseIDs(lock runtimeLock) []string {
	identifiers := make([]string, 0, len(lock.licenseArtifacts()))
	for identifier := range lock.licenseArtifacts() {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	return identifiers
}

func materializeRuntimeAssets(ctx context.Context, options materializeOptions) error {
	if err := options.validate(); err != nil {
		return err
	}
	payloads, err := resolveRuntimePayloads(ctx, options.lockPath, options.architecture,
		options.asnBuilderPath, options.cacheDirectory, options.offline)
	if err != nil {
		return err
	}
	files := []bundleFile{
		{Path: "lib/rw-core", Mode: 0o755, Data: payloads.core},
		{Path: "runtime-assets.lock.json", Mode: 0o644, Data: payloads.document.Data},
		{Path: "share/asn/asn-prefixes.bin", Mode: 0o644, Data: payloads.asn},
		{Path: "share/xray/geoip.dat", Mode: 0o644, Data: payloads.geoIP},
		{Path: "share/xray/geosite.dat", Mode: 0o644, Data: payloads.geoSite},
	}
	for _, identifier := range sortedLicenseIDs(payloads.document.Lock) {
		files = append(files, bundleFile{
			Path: "licenses/" + identifier + ".txt", Mode: 0o644,
			Data: payloads.licenses[identifier],
		})
	}
	sortBundleFiles(files)
	return writeMaterializedTree(options.outputDirectory, files)
}

func (options materializeOptions) validate() error {
	if options.lockPath == "" || options.asnBuilderPath == "" || options.cacheDirectory == "" || options.outputDirectory == "" {
		return fmt.Errorf("materialize requires lock, ASN builder, cache, and output directory paths")
	}
	if options.architecture != "amd64" && options.architecture != "arm64" {
		return fmt.Errorf("unsupported architecture %q", options.architecture)
	}
	if info, err := os.Lstat(options.outputDirectory); err == nil {
		return fmt.Errorf("output directory already exists: %s (%s)", options.outputDirectory, info.Mode())
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output directory: %w", err)
	}
	return nil
}

func writeMaterializedTree(outputDirectory string, files []bundleFile) error {
	parent := filepath.Dir(outputDirectory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create materialize output parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".runtime-assets-*")
	if err != nil {
		return fmt.Errorf("create materialize staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("set materialize staging permissions: %w", err)
	}
	for _, file := range files {
		if err := validateArchiveRelativePath(file.Path, 512); err != nil {
			return fmt.Errorf("invalid materialized path %q: %w", file.Path, err)
		}
		if file.Mode != 0o644 && file.Mode != 0o755 {
			return fmt.Errorf("invalid materialized mode for %q", file.Path)
		}
		target := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create materialized directory for %q: %w", file.Path, err)
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(file.Mode))
		if err != nil {
			return fmt.Errorf("create materialized file %q: %w", file.Path, err)
		}
		if _, err := output.Write(file.Data); err != nil {
			_ = output.Close()
			return fmt.Errorf("write materialized file %q: %w", file.Path, err)
		}
		if err := output.Sync(); err != nil {
			_ = output.Close()
			return fmt.Errorf("sync materialized file %q: %w", file.Path, err)
		}
		if err := output.Close(); err != nil {
			return fmt.Errorf("close materialized file %q: %w", file.Path, err)
		}
		if err := os.Chmod(target, os.FileMode(file.Mode)); err != nil {
			return fmt.Errorf("set materialized file %q permissions: %w", file.Path, err)
		}
		if err := verifyFileIdentity(target, digestBytes(file.Data), int64(len(file.Data))); err != nil {
			return fmt.Errorf("verify materialized file %q: %w", file.Path, err)
		}
	}
	if err := syncTreeDirectories(staging); err != nil {
		return err
	}
	if err := os.Rename(staging, outputDirectory); err != nil {
		return fmt.Errorf("publish materialized runtime tree: %w", err)
	}
	published = true
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync materialize output parent: %w", err)
	}
	return nil
}

func syncTreeDirectories(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, current)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk materialized directories: %w", err)
	}
	sort.Slice(directories, func(left, right int) bool { return len(directories[left]) > len(directories[right]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return fmt.Errorf("sync materialized directory: %w", err)
		}
	}
	return nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
