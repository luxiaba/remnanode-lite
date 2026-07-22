package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildBundleIsDeterministicAndVerifiable(t *testing.T) {
	fixture := newBuildFixture(t)
	first := filepath.Join(t.TempDir(), "first.tar.gz")
	second := filepath.Join(t.TempDir(), "second.tar.gz")
	options := fixture.options
	options.outputPath = first
	if err := buildBundle(context.Background(), options); err != nil {
		t.Fatalf("first build: %v", err)
	}
	options.outputPath = second
	if err := buildBundle(context.Background(), options); err != nil {
		t.Fatalf("second build: %v", err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstBytes, secondBytes) {
		t.Fatalf("identical inputs produced different archives: %s != %s", digestBytes(firstBytes), digestBytes(secondBytes))
	}

	if err := verifyBundle(fixture.verify(first)); err != nil {
		t.Fatalf("verify generated bundle: %v", err)
	}
	entries, err := readBundleArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"remnanode-lite/install.sh",
		"remnanode-lite/bin/remnanode-lite",
		"remnanode-lite/bin/rnlctl",
		"remnanode-lite/lib/rw-core",
		"remnanode-lite/share/xray/geoip.dat",
		"remnanode-lite/share/xray/geosite.dat",
		"remnanode-lite/share/asn/asn-prefixes.bin",
		"remnanode-lite/SBOM.spdx.json",
		"remnanode-lite/release-manifest.json",
	} {
		if _, exists := entries[required]; !exists {
			t.Errorf("generated bundle is missing %s", required)
		}
	}
}

func TestVerifyBundleRejectsTamperedPayload(t *testing.T) {
	fixture := newBuildFixture(t)
	original := filepath.Join(t.TempDir(), "original.tar.gz")
	options := fixture.options
	options.outputPath = original
	if err := buildBundle(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	entries, err := readBundleArchive(original)
	if err != nil {
		t.Fatal(err)
	}
	var files []bundleFile
	for name, entry := range entries {
		if entry.isDir {
			continue
		}
		relative := strings.TrimPrefix(name, bundleName+"/")
		data := append([]byte(nil), entry.data...)
		if relative == "bin/remnanode-lite" {
			data[0] ^= 0xff
		}
		files = append(files, bundleFile{Path: relative, Mode: entry.mode, Data: data})
	}
	sortBundleFiles(files)
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	if err := writeDeterministicArchive(tampered, fixture.options.sourceDateEpoch, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyBundle(fixture.verify(tampered)); err == nil || !strings.Contains(err.Error(), "does not match manifest metadata") {
		t.Fatalf("verify error = %v, want manifest digest failure", err)
	}
}

func TestVerifyBundleBindsManifestPayloadToRuntimeLock(t *testing.T) {
	fixture := newBuildFixture(t)
	original := filepath.Join(t.TempDir(), "original.tar.gz")
	options := fixture.options
	options.outputPath = original
	if err := buildBundle(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	entries, err := readBundleArchive(original)
	if err != nil {
		t.Fatal(err)
	}
	manifestEntry := entries[bundleName+"/release-manifest.json"]
	manifest, err := decodeReleaseManifest(manifestEntry.data)
	if err != nil {
		t.Fatal(err)
	}
	originalSPDXInputs, err := sbomInputFiles(manifest, entries)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := collectGoModules(originalSPDXInputs)
	if err != nil {
		t.Fatal(err)
	}

	corePath := bundleName + "/lib/rw-core"
	core := entries[corePath]
	core.data = append([]byte(nil), core.data...)
	core.data[0] ^= 0xff
	entries[corePath] = core
	for index := range manifest.Files {
		if manifest.Files[index].Path == "lib/rw-core" {
			manifest.Files[index].SHA256 = digestBytes(core.data)
		}
	}

	spdxInputs, err := sbomInputFiles(manifest, entries)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := loadRuntimeLock(fixture.options.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	spdxJSON, err := marshalDeterministicJSON(buildSPDX(fixture.options, lock, spdxInputs, modules))
	if err != nil {
		t.Fatal(err)
	}
	sbomPath := bundleName + "/SBOM.spdx.json"
	sbom := entries[sbomPath]
	sbom.data = spdxJSON
	sbom.size = int64(len(spdxJSON))
	entries[sbomPath] = sbom
	for index := range manifest.Files {
		if manifest.Files[index].Path == "SBOM.spdx.json" {
			manifest.Files[index].SHA256 = digestBytes(spdxJSON)
			manifest.Files[index].Size = int64(len(spdxJSON))
		}
	}
	manifestJSON, err := marshalDeterministicJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestEntry.data = manifestJSON
	manifestEntry.size = int64(len(manifestJSON))
	entries[bundleName+"/release-manifest.json"] = manifestEntry

	var files []bundleFile
	for name, entry := range entries {
		if entry.isDir {
			continue
		}
		files = append(files, bundleFile{
			Path: strings.TrimPrefix(name, bundleName+"/"), Mode: entry.mode, Data: entry.data,
		})
	}
	sortBundleFiles(files)
	tampered := filepath.Join(t.TempDir(), "self-consistent-tampered.tar.gz")
	if err := writeDeterministicArchive(tampered, fixture.options.sourceDateEpoch, files); err != nil {
		t.Fatal(err)
	}
	if err := verifyBundle(fixture.verify(tampered)); err == nil || !strings.Contains(err.Error(), "does not match the runtime asset lock") {
		t.Fatalf("verify error = %v, want runtime lock binding failure", err)
	}
}

func TestReadBundleArchiveRejectsPathTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "malicious.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zipper := gzip.NewWriter(file)
	archive := tar.NewWriter(zipper)
	header := &tar.Header{
		Name: "remnanode-lite/../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg,
		Uid: 0, Gid: 0, Uname: "root", Gname: "root", ModTime: time.Unix(1, 0),
	}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundleArchive(archivePath); err == nil || !strings.Contains(err.Error(), "unsafe bundle archive entry") {
		t.Fatalf("read error = %v, want unsafe path rejection", err)
	}
}

func TestCollectSupportFilesRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite.service"), []byte("service"), 0o644)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite-hardening.conf"), []byte("hardening"), 0o644)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite.openrc"), []byte("openrc"), 0o755)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "node.env.example"), []byte("env"), 0o644)
	target := filepath.Join(t.TempDir(), "outside")
	writeFixtureFile(t, target, []byte("outside"), 0o644)
	if err := os.Symlink(target, filepath.Join(directory, "deploy", "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := collectSupportFiles(directory); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("collect error = %v, want symlink rejection", err)
	}
}

func TestCollectSupportFilesRejectsUncontractedFile(t *testing.T) {
	directory := t.TempDir()
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite.service"), []byte("service"), 0o644)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite-hardening.conf"), []byte("hardening"), 0o644)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "remnanode-lite.openrc"), []byte("openrc"), 0o755)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "node.env.example"), []byte("env"), 0o644)
	writeFixtureFile(t, filepath.Join(directory, "deploy", "unexpected"), []byte("unexpected"), 0o644)
	if _, err := collectSupportFiles(directory); err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("collect error = %v, want exact support contract rejection", err)
	}
}

func TestSPDXIncludesGoModulePackagesAndStaticLinks(t *testing.T) {
	options := buildOptions{
		version: "1.2.3", architecture: "amd64", sourceRevision: strings.Repeat("a", 40), sourceDateEpoch: 1_700_000_000,
	}
	files := []bundleFile{
		{Path: "bin/remnanode-lite", Data: []byte("node"), License: "AGPL-3.0-only"},
		{Path: "bin/rnlctl", Data: []byte("ctl"), License: "AGPL-3.0-only"},
	}
	modules := []goModule{{
		Path: "example.com/module", Version: "v1.2.3", Sum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		UsedBy: []string{"bin/remnanode-lite"},
	}}
	document := buildSPDX(options, runtimeLock{
		Xray:    xrayLock{Version: "v1.0.0", SourceURL: "https://example.invalid/xray"},
		GeoIP:   runtimeDataLock{Version: "202401010000", Artifact: artifactLock{URL: "https://example.invalid/geoip"}, License: "NOASSERTION"},
		GeoSite: runtimeDataLock{Version: "202401010000", Artifact: artifactLock{URL: "https://example.invalid/geosite"}, License: "GPL-3.0-only"},
		ASN:     asnLock{Commit: strings.Repeat("b", 40), Source: artifactLock{URL: "https://example.invalid/asn"}, Output: outputLock{License: "CC0-1.0"}},
	}, files, modules)
	moduleID := goModuleSPDXID(modules[0])
	foundPackage := false
	for _, pkg := range document.Packages {
		if pkg.SPDXID == moduleID {
			foundPackage = true
			if pkg.FilesAnalyzed || pkg.PackageVerificationCode != nil || len(pkg.ExternalRefs) != 1 {
				t.Fatalf("Go module package has invalid SPDX metadata: %+v", pkg)
			}
		}
	}
	if !foundPackage {
		t.Fatal("SPDX document is missing the Go module package")
	}
	foundLink := false
	for _, relationship := range document.Relationships {
		if relationship.RelationshipType == "STATIC_LINK" && relationship.RelatedSPDXElement == moduleID {
			foundLink = true
		}
	}
	if !foundLink {
		t.Fatal("SPDX document is missing the Go module static-link relationship")
	}
}

type buildFixture struct {
	options buildOptions
}

func newBuildFixture(t *testing.T) buildFixture {
	t.Helper()
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}

	binaries := fixtureGoBinaries(t)
	core := binaries["rw-core-amd64"]
	armCore := binaries["rw-core-arm64"]
	geoIP := []byte("fixture-geoip")
	geoSite := []byte("fixture-geosite")
	xrayArchivePath := filepath.Join(root, "xray.zip")
	writeFixtureZip(t, xrayArchivePath, map[string][]byte{
		"xray": core, "geoip.dat": geoIP, "geosite.dat": geoSite,
		"LICENSE": []byte("upstream license"), "README.md": []byte("upstream readme"),
	})
	xrayArchive, err := os.ReadFile(xrayArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	armXrayArchivePath := filepath.Join(root, "xray-arm64.zip")
	writeFixtureZip(t, armXrayArchivePath, map[string][]byte{
		"xray": armCore, "LICENSE": []byte("upstream license"), "README.md": []byte("upstream readme"),
	})
	armXrayArchive, err := os.ReadFile(armXrayArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	xrayArtifact := fixtureArtifact("https://github.com/XTLS/Xray-core/releases/download/v1.2.3/Xray-linux-64.zip", xrayArchive)
	armXrayArtifact := fixtureArtifact("https://github.com/XTLS/Xray-core/releases/download/v1.2.3/Xray-linux-arm64-v8a.zip", armXrayArchive)

	asnSource := []byte("fixture-asn-source")
	asnDatabase := []byte("fixture-asn-database")
	licenses := map[string][]byte{
		"MPL-2.0":      []byte("fixture MPL text"),
		"GPL-3.0-only": []byte("fixture GPL text"),
		"CC-BY-SA-4.0": []byte("fixture CC BY-SA text"),
		"CC0-1.0":      []byte("fixture CC0 text"),
	}
	lock := runtimeLock{
		SchemaVersion: 2,
		Xray: xrayLock{
			Version: "v1.2.3", Commit: strings.Repeat("a", 40),
			SourceURL: "https://github.com/XTLS/Xray-core/tree/" + strings.Repeat("a", 40),
			Architectures: xrayArchitectures{
				AMD64: fixtureXrayArchitecture(xrayArtifact, core, geoIP, geoSite),
				ARM64: fixtureXrayArchitecture(armXrayArtifact, armCore, geoIP, geoSite),
			},
		},
		GeoIP:   fixtureRuntimeData(strings.Repeat("d", 40), "202401010000", "Loyalsoldier/geoip", "geoip.dat", geoIP, "NOASSERTION", "fixture mixed-license rationale"),
		GeoSite: fixtureRuntimeData(strings.Repeat("e", 40), "202401020000", "Loyalsoldier/v2ray-rules-dat", "geosite.dat", geoSite, "GPL-3.0-only", ""),
		ASN: asnLock{
			Commit: strings.Repeat("b", 40),
			Source: fixtureArtifact("https://github.com/ipverse/as-ip-blocks/archive/"+strings.Repeat("b", 40)+".tar.gz", asnSource),
			Output: outputLock{SHA256: digestBytes(asnDatabase), Size: int64(len(asnDatabase)), License: "CC0-1.0"},
		},
		Licenses: licenseLock{
			MPL2:   fixtureArtifact(spdxLicenseURL("MPL-2.0"), licenses["MPL-2.0"]),
			GPL3:   fixtureArtifact(spdxLicenseURL("GPL-3.0-only"), licenses["GPL-3.0-only"]),
			CCBYSA: fixtureArtifact(spdxLicenseURL("CC-BY-SA-4.0"), licenses["CC-BY-SA-4.0"]),
			CC0:    fixtureArtifact(spdxLicenseURL("CC0-1.0"), licenses["CC0-1.0"]),
		},
	}
	lockJSON, err := marshalDeterministicJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "runtime-assets.lock.json")
	writeFixtureFile(t, lockPath, lockJSON, 0o644)

	cacheFixtureArtifact(t, cache, xrayArchive)
	cacheFixtureArtifact(t, cache, armXrayArchive)
	cacheFixtureArtifact(t, cache, geoIP)
	cacheFixtureArtifact(t, cache, geoSite)
	cacheFixtureArtifact(t, cache, asnSource)
	for _, contents := range licenses {
		cacheFixtureArtifact(t, cache, contents)
	}

	writeFixtureFile(t, filepath.Join(root, "LICENSE"), []byte("project license"), 0o644)
	writeFixtureFile(t, filepath.Join(root, "release", "bundle", "THIRD_PARTY_NOTICES.md"), fixtureNotices(lock), 0o644)
	writeFixtureFile(t, filepath.Join(root, "release", "bundle", "SOURCE-OFFER.md"), fixtureSourceOffer(lock), 0o644)
	node := filepath.Join(root, "remnanode-lite")
	rnlctl := filepath.Join(root, "rnlctl")
	installer := filepath.Join(root, "install.sh")
	asnBuilder := filepath.Join(root, "asn-builder")
	writeFixtureFile(t, node, binaries["remnanode-lite-amd64"], 0o755)
	writeFixtureFile(t, rnlctl, binaries["rnlctl-amd64"], 0o755)
	writeFixtureFile(t, installer, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	builderScript := "#!/bin/sh\nout=''\nwhile [ \"$#\" -gt 0 ]; do\n  case \"$1\" in\n    -out) out=$2; shift 2 ;;\n    *) shift ;;\n  esac\ndone\nprintf 'fixture-asn-database' > \"$out\"\n"
	writeFixtureFile(t, asnBuilder, []byte(builderScript), 0o755)

	support := filepath.Join(root, "support")
	writeFixtureFile(t, filepath.Join(support, "deploy", "remnanode-lite.service"), []byte("service"), 0o644)
	writeFixtureFile(t, filepath.Join(support, "deploy", "remnanode-lite-hardening.conf"), []byte("hardening"), 0o644)
	writeFixtureFile(t, filepath.Join(support, "deploy", "remnanode-lite.openrc"), []byte("openrc"), 0o755)
	writeFixtureFile(t, filepath.Join(support, "deploy", "node.env.example"), []byte("NODE_PORT=38329\n"), 0o644)

	return buildFixture{options: buildOptions{
		lockPath: lockPath, architecture: "amd64", version: "1.2.3-rnl.1",
		contractVersion: "1.2.3", sourceRevision: strings.Repeat("c", 40),
		sourceDateEpoch: 1_700_000_000, projectRoot: root,
		nodePath: node, rnlctlPath: rnlctl, asnBuilderPath: asnBuilder,
		installerPath: installer, supportDirectory: support,
		cacheDirectory: cache, offline: true,
	}}
}

func fixtureRuntimeData(commit, version, repository, assetName string, data []byte, license, rationale string) runtimeDataLock {
	return runtimeDataLock{
		Version: version, Commit: commit,
		SourceURL:      "https://github.com/" + repository + "/tree/" + commit,
		SourceArtifact: fixtureArtifact("https://github.com/"+repository+"/archive/"+commit+".tar.gz", []byte("source-"+assetName)),
		Artifact:       fixtureArtifact("https://github.com/"+repository+"/releases/download/"+version+"/"+assetName, data),
		License:        license, LicenseRationale: rationale,
	}
}

func fixtureNotices(lock runtimeLock) []byte {
	lines := []string{
		"# Third-party notices",
		"`lib/rw-core` `" + lock.Xray.Version + "` `" + lock.Xray.Commit + "` " + lock.Xray.SourceURL,
		"`share/xray/geoip.dat` `" + lock.GeoIP.Version + "` `" + lock.GeoIP.Commit + "` " + lock.GeoIP.SourceURL + " " + lock.GeoIP.SourceArtifact.URL + " " + lock.GeoIP.Artifact.URL,
		lock.GeoIP.LicenseRationale,
		"`share/xray/geosite.dat` `" + lock.GeoSite.Version + "` `" + lock.GeoSite.Commit + "` " + lock.GeoSite.SourceURL + " " + lock.GeoSite.SourceArtifact.URL + " " + lock.GeoSite.Artifact.URL,
		"`share/asn/asn-prefixes.bin` `" + lock.ASN.Commit + "` https://github.com/ipverse/as-ip-blocks/tree/" + lock.ASN.Commit,
	}
	lines = append(lines, lockedProvenanceRows(lock)...)
	return []byte(strings.Join(lines, "\n"))
}

func fixtureSourceOffer(lock runtimeLock) []byte {
	return []byte(strings.Join([]string{
		lock.Xray.SourceURL,
		lock.GeoIP.SourceURL, lock.GeoIP.SourceArtifact.URL, lock.GeoIP.SourceArtifact.SHA256,
		lock.GeoSite.SourceURL, lock.GeoSite.SourceArtifact.URL, lock.GeoSite.SourceArtifact.SHA256,
		"https://github.com/ipverse/as-ip-blocks/tree/" + lock.ASN.Commit,
		lock.ASN.Source.URL, lock.ASN.Source.SHA256,
	}, "\n"))
}

var (
	fixtureBinariesOnce sync.Once
	fixtureBinariesData map[string][]byte
	fixtureBinariesErr  error
)

func fixtureGoBinaries(t *testing.T) map[string][]byte {
	t.Helper()
	fixtureBinariesOnce.Do(func() {
		root, err := os.MkdirTemp("", "release-tool-go-binaries-*")
		if err != nil {
			fixtureBinariesErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/luxiaba/remnanode-lite\n\ngo 1.22\n"), 0o644); err != nil {
			fixtureBinariesErr = err
			return
		}
		for _, command := range []string{"remnanode-lite", "rnlctl"} {
			directory := filepath.Join(root, "cmd", command)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				fixtureBinariesErr = err
				return
			}
			if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
				fixtureBinariesErr = err
				return
			}
		}
		xrayRoot := filepath.Join(root, "xray-core")
		if err := os.MkdirAll(filepath.Join(xrayRoot, "main"), 0o755); err != nil {
			fixtureBinariesErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(xrayRoot, "go.mod"), []byte("module github.com/xtls/xray-core\n\ngo 1.22\n"), 0o644); err != nil {
			fixtureBinariesErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(xrayRoot, "main", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
			fixtureBinariesErr = err
			return
		}
		fixtureBinariesData = make(map[string][]byte)
		for _, architecture := range []string{"amd64", "arm64"} {
			for _, commandName := range []string{"remnanode-lite", "rnlctl"} {
				outputPath := filepath.Join(root, commandName+"-"+architecture)
				command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", outputPath, "./cmd/"+commandName)
				command.Dir = root
				command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+architecture)
				if output, err := command.CombinedOutput(); err != nil {
					fixtureBinariesErr = fmt.Errorf("build fixture %s/%s: %w: %s", commandName, architecture, err, output)
					return
				}
				data, err := os.ReadFile(outputPath)
				if err != nil {
					fixtureBinariesErr = err
					return
				}
				fixtureBinariesData[commandName+"-"+architecture] = data
			}
			outputPath := filepath.Join(root, "rw-core-"+architecture)
			command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", outputPath, "./main")
			command.Dir = xrayRoot
			command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+architecture)
			if output, err := command.CombinedOutput(); err != nil {
				fixtureBinariesErr = fmt.Errorf("build fixture rw-core/%s: %w: %s", architecture, err, output)
				return
			}
			data, err := os.ReadFile(outputPath)
			if err != nil {
				fixtureBinariesErr = err
				return
			}
			fixtureBinariesData["rw-core-"+architecture] = data
		}
	})
	if fixtureBinariesErr != nil {
		t.Fatal(fixtureBinariesErr)
	}
	return fixtureBinariesData
}

func (fixture buildFixture) verify(archivePath string) verifyOptions {
	return verifyOptions{
		lockPath: fixture.options.lockPath, archivePath: archivePath,
		architecture: fixture.options.architecture, version: fixture.options.version,
		contractVersion: fixture.options.contractVersion, sourceRevision: fixture.options.sourceRevision,
	}
}

func fixtureArtifact(url string, contents []byte) artifactLock {
	return artifactLock{URL: url, SHA256: digestBytes(contents), Size: int64(len(contents))}
}

func spdxLicenseURL(identifier string) string {
	return "https://raw.githubusercontent.com/spdx/license-list-data/v3.27.0/text/" + identifier + ".txt"
}

func cacheFixtureArtifact(t *testing.T, cache string, contents []byte) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(cache, digestBytes(contents)), contents, 0o644)
}

func writeFixtureFile(t *testing.T, outputPath string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, contents, mode); err != nil {
		t.Fatal(err)
	}
}
