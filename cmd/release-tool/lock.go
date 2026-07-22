package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

const maxLockBytes = 1 << 20

var (
	hexDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	gitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	xrayTagPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	dataTagPattern   = regexp.MustCompile(`^[0-9]{12}$`)
)

type runtimeLock struct {
	SchemaVersion int             `json:"schemaVersion"`
	Xray          xrayLock        `json:"xray"`
	GeoIP         runtimeDataLock `json:"geoIP"`
	GeoSite       runtimeDataLock `json:"geoSite"`
	ASN           asnLock         `json:"asn"`
	Licenses      licenseLock     `json:"licenses"`
}

type xrayLock struct {
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	SourceURL     string            `json:"sourceURL"`
	Architectures xrayArchitectures `json:"architectures"`
}

type xrayArchitectures struct {
	AMD64 xrayArchitecture `json:"amd64"`
	ARM64 xrayArchitecture `json:"arm64"`
}

type xrayArchitecture struct {
	Archive artifactLock `json:"archive"`
	Core    archiveEntry `json:"core"`
}

type runtimeDataLock struct {
	Version          string       `json:"version"`
	Commit           string       `json:"commit"`
	SourceURL        string       `json:"sourceURL"`
	SourceArtifact   artifactLock `json:"sourceArtifact"`
	Artifact         artifactLock `json:"artifact"`
	License          string       `json:"license"`
	LicenseRationale string       `json:"licenseRationale,omitempty"`
}

type artifactLock struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type archiveEntry struct {
	ArchivePath string `json:"archivePath"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	License     string `json:"license"`
}

type asnLock struct {
	Commit string       `json:"commit"`
	Source artifactLock `json:"source"`
	Output outputLock   `json:"output"`
}

type outputLock struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	License string `json:"license"`
}

type licenseLock struct {
	MPL2   artifactLock `json:"MPL-2.0"`
	GPL3   artifactLock `json:"GPL-3.0-only"`
	CCBYSA artifactLock `json:"CC-BY-SA-4.0"`
	CC0    artifactLock `json:"CC0-1.0"`
}

type runtimeLockDocument struct {
	Lock   runtimeLock
	Data   []byte
	SHA256 string
}

func loadRuntimeLock(path string) (runtimeLock, error) {
	document, err := loadRuntimeLockDocument(path)
	if err != nil {
		return runtimeLock{}, err
	}
	return document.Lock, nil
}

func loadRuntimeLockDocument(path string) (runtimeLockDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return runtimeLockDocument{}, fmt.Errorf("open runtime asset lock: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxLockBytes+1))
	if err != nil {
		return runtimeLockDocument{}, fmt.Errorf("read runtime asset lock: %w", err)
	}
	if len(data) > maxLockBytes {
		return runtimeLockDocument{}, fmt.Errorf("runtime asset lock exceeds %d bytes", maxLockBytes)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return runtimeLockDocument{}, fmt.Errorf("decode runtime asset lock: %w", err)
	}

	var lock runtimeLock
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lock); err != nil {
		return runtimeLockDocument{}, fmt.Errorf("decode runtime asset lock: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return runtimeLockDocument{}, fmt.Errorf("decode runtime asset lock: %w", err)
	}
	if err := lock.validate(); err != nil {
		return runtimeLockDocument{}, fmt.Errorf("validate runtime asset lock: %w", err)
	}
	return runtimeLockDocument{Lock: lock, Data: data, SHA256: digestBytes(data)}, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func inspectJSONValue(decoder *json.Decoder) error {
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
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object has invalid closing delimiter")
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array has invalid closing delimiter")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func (lock runtimeLock) validate() error {
	if lock.SchemaVersion != 2 {
		return fmt.Errorf("schemaVersion must be 2")
	}
	if !xrayTagPattern.MatchString(lock.Xray.Version) {
		return fmt.Errorf("invalid Xray version %q", lock.Xray.Version)
	}
	if !gitCommitPattern.MatchString(lock.Xray.Commit) {
		return fmt.Errorf("invalid Xray commit %q", lock.Xray.Commit)
	}
	wantSource := "https://github.com/XTLS/Xray-core/tree/" + lock.Xray.Commit
	if lock.Xray.SourceURL != wantSource {
		return fmt.Errorf("Xray sourceURL must be %q", wantSource)
	}

	architectures := []struct {
		name string
		lock xrayArchitecture
		file string
	}{
		{name: "amd64", lock: lock.Xray.Architectures.AMD64, file: "Xray-linux-64.zip"},
		{name: "arm64", lock: lock.Xray.Architectures.ARM64, file: "Xray-linux-arm64-v8a.zip"},
	}
	for _, architecture := range architectures {
		prefix := "xray.architectures." + architecture.name
		wantURL := "https://github.com/XTLS/Xray-core/releases/download/" +
			lock.Xray.Version + "/" + architecture.file
		if architecture.lock.Archive.URL != wantURL {
			return fmt.Errorf("%s.archive.url must be %q", prefix, wantURL)
		}
		if err := validateArtifact(prefix+".archive", architecture.lock.Archive, 1<<30); err != nil {
			return err
		}
		if architecture.lock.Core.ArchivePath != "xray" {
			return fmt.Errorf("%s.core.archivePath must be %q", prefix, "xray")
		}
		if architecture.lock.Core.License != "MPL-2.0" {
			return fmt.Errorf("%s.core.license must be MPL-2.0", prefix)
		}
		if err := validateDigestAndSize(prefix+".core", architecture.lock.Core.SHA256, architecture.lock.Core.Size, 1<<30); err != nil {
			return err
		}
	}
	if err := validateRuntimeData("geoIP", lock.GeoIP, "Loyalsoldier/geoip", "geoip.dat", "NOASSERTION", true); err != nil {
		return err
	}
	if err := validateRuntimeData("geoSite", lock.GeoSite, "Loyalsoldier/v2ray-rules-dat", "geosite.dat", "GPL-3.0-only", false); err != nil {
		return err
	}

	if !gitCommitPattern.MatchString(lock.ASN.Commit) {
		return fmt.Errorf("invalid ASN commit %q", lock.ASN.Commit)
	}
	wantASNURL := "https://github.com/ipverse/as-ip-blocks/archive/" + lock.ASN.Commit + ".tar.gz"
	if lock.ASN.Source.URL != wantASNURL {
		return fmt.Errorf("asn.source.url must be %q", wantASNURL)
	}
	if err := validateArtifact("asn.source", lock.ASN.Source, 1<<30); err != nil {
		return err
	}
	if lock.ASN.Output.License != "CC0-1.0" {
		return fmt.Errorf("asn.output.license must be CC0-1.0")
	}
	if err := validateDigestAndSize("asn.output", lock.ASN.Output.SHA256, lock.ASN.Output.Size, 1<<30); err != nil {
		return err
	}

	licenses := []struct {
		id       string
		artifact artifactLock
	}{
		{id: "MPL-2.0", artifact: lock.Licenses.MPL2},
		{id: "GPL-3.0-only", artifact: lock.Licenses.GPL3},
		{id: "CC-BY-SA-4.0", artifact: lock.Licenses.CCBYSA},
		{id: "CC0-1.0", artifact: lock.Licenses.CC0},
	}
	for _, license := range licenses {
		wantURL := "https://raw.githubusercontent.com/spdx/license-list-data/v3.27.0/text/" + license.id + ".txt"
		if license.artifact.URL != wantURL {
			return fmt.Errorf("licenses.%s.url must be %q", license.id, wantURL)
		}
		if err := validateArtifact("licenses."+license.id, license.artifact, 1<<20); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeData(name string, data runtimeDataLock, repository, assetName, license string, requireRationale bool) error {
	if !dataTagPattern.MatchString(data.Version) {
		return fmt.Errorf("%s.version must be a 12-digit upstream release tag", name)
	}
	if !gitCommitPattern.MatchString(data.Commit) {
		return fmt.Errorf("%s.commit must be a 40-character lowercase Git commit", name)
	}
	wantSourceURL := "https://github.com/" + repository + "/tree/" + data.Commit
	if data.SourceURL != wantSourceURL {
		return fmt.Errorf("%s.sourceURL must be %q", name, wantSourceURL)
	}
	wantSourceArtifactURL := "https://github.com/" + repository + "/archive/" + data.Commit + ".tar.gz"
	if data.SourceArtifact.URL != wantSourceArtifactURL {
		return fmt.Errorf("%s.sourceArtifact.url must be %q", name, wantSourceArtifactURL)
	}
	if err := validateArtifact(name+".sourceArtifact", data.SourceArtifact, 1<<30); err != nil {
		return err
	}
	wantArtifactURL := "https://github.com/" + repository + "/releases/download/" + data.Version + "/" + assetName
	if data.Artifact.URL != wantArtifactURL {
		return fmt.Errorf("%s.artifact.url must be %q", name, wantArtifactURL)
	}
	if err := validateArtifact(name+".artifact", data.Artifact, 1<<30); err != nil {
		return err
	}
	if data.License != license {
		return fmt.Errorf("%s.license must be %s", name, license)
	}
	if requireRationale && strings.TrimSpace(data.LicenseRationale) == "" {
		return fmt.Errorf("%s.licenseRationale is required for NOASSERTION", name)
	}
	if !requireRationale && data.LicenseRationale != "" {
		return fmt.Errorf("%s.licenseRationale must be omitted when the license is asserted", name)
	}
	return nil
}

func validateArtifact(name string, artifact artifactLock, maxSize int64) error {
	parsed, err := url.Parse(artifact.URL)
	if err != nil {
		return fmt.Errorf("%s.url: %w", name, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s.url must be an absolute HTTPS URL without credentials, query, or fragment", name)
	}
	if parsed.Hostname() != "github.com" && parsed.Hostname() != "raw.githubusercontent.com" {
		return fmt.Errorf("%s.url uses unapproved host %q", name, parsed.Hostname())
	}
	if strings.Contains(parsed.EscapedPath(), "%2f") || strings.Contains(parsed.EscapedPath(), "%2F") {
		return fmt.Errorf("%s.url contains an escaped path separator", name)
	}
	return validateDigestAndSize(name, artifact.SHA256, artifact.Size, maxSize)
}

func validateDigestAndSize(name, digest string, size, maxSize int64) error {
	if !hexDigestPattern.MatchString(digest) {
		return fmt.Errorf("%s.sha256 must be 64 lowercase hexadecimal characters", name)
	}
	if size <= 0 || size > maxSize {
		return fmt.Errorf("%s.size must be between 1 and %d", name, maxSize)
	}
	return nil
}

func (lock runtimeLock) xrayForArchitecture(architecture string) (xrayArchitecture, error) {
	switch architecture {
	case "amd64":
		return lock.Xray.Architectures.AMD64, nil
	case "arm64":
		return lock.Xray.Architectures.ARM64, nil
	default:
		return xrayArchitecture{}, fmt.Errorf("unsupported architecture %q", architecture)
	}
}

func (lock runtimeLock) licenseArtifacts() map[string]artifactLock {
	return map[string]artifactLock{
		"MPL-2.0":      lock.Licenses.MPL2,
		"GPL-3.0-only": lock.Licenses.GPL3,
		"CC-BY-SA-4.0": lock.Licenses.CCBYSA,
		"CC0-1.0":      lock.Licenses.CC0,
	}
}
