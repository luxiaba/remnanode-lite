package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractXrayAssetsRejectsMaliciousPath(t *testing.T) {
	core := []byte("core")
	geoIP := []byte("geoip")
	geoSite := []byte("geosite")
	architecture := fixtureXrayArchitecture(artifactLock{}, core, geoIP, geoSite)
	archivePath := filepath.Join(t.TempDir(), "xray.zip")
	writeFixtureZip(t, archivePath, map[string][]byte{
		"xray": core, "geoip.dat": geoIP, "geosite.dat": geoSite,
		"../escape": []byte("malicious"),
	})
	if _, err := extractXrayAssets(archivePath, architecture); err == nil || !strings.Contains(err.Error(), "unsafe Xray archive entry") {
		t.Fatalf("extractXrayAssets error = %v, want unsafe path rejection", err)
	}
}

func TestExtractXrayAssetsRejectsLockedDigestMismatch(t *testing.T) {
	core := []byte("core")
	geoIP := []byte("geoip")
	geoSite := []byte("geosite")
	architecture := fixtureXrayArchitecture(artifactLock{}, core, geoIP, geoSite)
	architecture.Core.SHA256 = strings.Repeat("a", 64)
	archivePath := filepath.Join(t.TempDir(), "xray.zip")
	writeFixtureZip(t, archivePath, map[string][]byte{
		"xray": core, "geoip.dat": geoIP, "geosite.dat": geoSite,
	})
	if _, err := extractXrayAssets(archivePath, architecture); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("extractXrayAssets error = %v, want digest mismatch", err)
	}
}

func TestExtractGeoCheckBinaryRejectsMaliciousPath(t *testing.T) {
	binary := []byte("geocheck")
	architecture := geoCheckArchitecture{Binary: archiveEntry{
		ArchivePath: "geocheck", SHA256: digestBytes(binary), Size: int64(len(binary)), License: "MIT",
	}}
	archivePath := filepath.Join(t.TempDir(), "geocheck.tar.gz")
	writeFixtureTarGzip(t, archivePath, map[string][]byte{"geocheck": binary, "../escape": []byte("bad")})
	if _, err := extractGeoCheckBinary(archivePath, architecture); err == nil || !strings.Contains(err.Error(), "unsafe geocheck archive entry") {
		t.Fatalf("extract error = %v, want unsafe path rejection", err)
	}
}

func TestExtractGeoCheckBinaryVerifiesPayload(t *testing.T) {
	binary := []byte("geocheck")
	architecture := geoCheckArchitecture{Binary: archiveEntry{
		ArchivePath: "geocheck", SHA256: digestBytes(binary), Size: int64(len(binary)), License: "MIT",
	}}
	archivePath := filepath.Join(t.TempDir(), "geocheck.tar.gz")
	writeFixtureTarGzip(t, archivePath, map[string][]byte{"LICENSE": []byte("MIT"), "geocheck": binary})
	got, err := extractGeoCheckBinary(archivePath, architecture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
	architecture.Binary.SHA256 = strings.Repeat("a", 64)
	if _, err := extractGeoCheckBinary(archivePath, architecture); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("extract error = %v, want digest rejection", err)
	}
}

func writeFixtureZip(t *testing.T, outputPath string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, contents := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureTarGzip(t *testing.T, outputPath string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	zipper := gzip.NewWriter(file)
	archive := tar.NewWriter(zipper)
	for name, contents := range entries {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(contents); err != nil {
			t.Fatal(err)
		}
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
}

func fixtureXrayArchitecture(archive artifactLock, core, _, _ []byte) xrayArchitecture {
	return xrayArchitecture{
		Archive: archive,
		Core:    archiveEntry{ArchivePath: "xray", SHA256: digestBytes(core), Size: int64(len(core)), License: "MPL-2.0"},
	}
}
