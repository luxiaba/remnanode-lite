package main

import (
	"fmt"
	"strings"
)

func validateThirdPartyNotices(lock runtimeLock, notices []byte) error {
	text := string(notices)
	required := []struct {
		name  string
		value string
	}{
		{name: "Xray version", value: "`" + lock.Xray.Version + "`"},
		{name: "Xray commit", value: "`" + lock.Xray.Commit + "`"},
		{name: "Xray source URL", value: lock.Xray.SourceURL},
		{name: "GeoIP version", value: "`" + lock.GeoIP.Version + "`"},
		{name: "GeoIP commit", value: "`" + lock.GeoIP.Commit + "`"},
		{name: "GeoIP source URL", value: lock.GeoIP.SourceURL},
		{name: "GeoIP source artifact URL", value: lock.GeoIP.SourceArtifact.URL},
		{name: "GeoIP artifact URL", value: lock.GeoIP.Artifact.URL},
		{name: "GeoSite version", value: "`" + lock.GeoSite.Version + "`"},
		{name: "GeoSite commit", value: "`" + lock.GeoSite.Commit + "`"},
		{name: "GeoSite source URL", value: lock.GeoSite.SourceURL},
		{name: "GeoSite source artifact URL", value: lock.GeoSite.SourceArtifact.URL},
		{name: "GeoSite artifact URL", value: lock.GeoSite.Artifact.URL},
		{name: "ASN commit", value: "`" + lock.ASN.Commit + "`"},
		{name: "ASN source URL", value: "https://github.com/ipverse/as-ip-blocks/tree/" + lock.ASN.Commit},
	}
	if lock.GeoIP.License == "NOASSERTION" && !strings.Contains(text, lock.GeoIP.LicenseRationale) {
		return fmt.Errorf("third-party notices do not include the locked GeoIP license rationale")
	}
	for _, row := range lockedProvenanceRows(lock) {
		if !strings.Contains(text, row) {
			return fmt.Errorf("third-party notices are missing locked provenance row %q", row)
		}
	}
	for _, field := range required {
		if !strings.Contains(text, field.value) {
			return fmt.Errorf("third-party notices do not identify the locked %s", field.name)
		}
	}
	for _, path := range []string{
		"lib/rw-core", "share/xray/geoip.dat", "share/xray/geosite.dat", "share/asn/asn-prefixes.bin",
	} {
		if !strings.Contains(text, "`"+path+"`") {
			return fmt.Errorf("third-party notices do not describe %s", path)
		}
	}
	return nil
}

func validateSourceOffer(lock runtimeLock, sourceOffer []byte) error {
	text := string(sourceOffer)
	required := []string{
		lock.Xray.SourceURL,
		lock.GeoIP.SourceURL,
		lock.GeoIP.SourceArtifact.URL,
		lock.GeoIP.SourceArtifact.SHA256,
		lock.GeoSite.SourceURL,
		lock.GeoSite.SourceArtifact.URL,
		lock.GeoSite.SourceArtifact.SHA256,
		"https://github.com/ipverse/as-ip-blocks/tree/" + lock.ASN.Commit,
		lock.ASN.Source.URL,
		lock.ASN.Source.SHA256,
	}
	for _, value := range required {
		if !strings.Contains(text, value) {
			return fmt.Errorf("source offer does not identify locked source value %q", value)
		}
	}
	return nil
}

func lockedProvenanceRows(lock runtimeLock) []string {
	rows := []string{
		provenanceRow("Xray core (amd64)", lock.Xray.Version+" @ "+lock.Xray.Commit,
			lock.Xray.Architectures.AMD64.Archive, artifactLock{SHA256: lock.Xray.Architectures.AMD64.Core.SHA256, Size: lock.Xray.Architectures.AMD64.Core.Size}, lock.Xray.Architectures.AMD64.Core.License),
		provenanceRow("Xray core (arm64)", lock.Xray.Version+" @ "+lock.Xray.Commit,
			lock.Xray.Architectures.ARM64.Archive, artifactLock{SHA256: lock.Xray.Architectures.ARM64.Core.SHA256, Size: lock.Xray.Architectures.ARM64.Core.Size}, lock.Xray.Architectures.ARM64.Core.License),
		provenanceRow("GeoIP", lock.GeoIP.Version+" @ "+lock.GeoIP.Commit,
			lock.GeoIP.SourceArtifact, lock.GeoIP.Artifact, lock.GeoIP.License),
		provenanceRow("GeoSite", lock.GeoSite.Version+" @ "+lock.GeoSite.Commit,
			lock.GeoSite.SourceArtifact, lock.GeoSite.Artifact, lock.GeoSite.License),
		provenanceRow("ASN database", lock.ASN.Commit, lock.ASN.Source,
			artifactLock{SHA256: lock.ASN.Output.SHA256, Size: lock.ASN.Output.Size}, lock.ASN.Output.License),
	}
	licenseIDs := sortedLicenseIDs(lock)
	for _, identifier := range licenseIDs {
		artifact := lock.licenseArtifacts()[identifier]
		rows = append(rows, fmt.Sprintf("| `%s` | %s | `sha256:%s` | %d |",
			identifier, artifact.URL, artifact.SHA256, artifact.Size))
	}
	return rows
}

func provenanceRow(component, revision string, source, payload artifactLock, license string) string {
	return fmt.Sprintf("| %s | `%s` | `sha256:%s` (%d bytes) | `sha256:%s` (%d bytes) | `%s` |",
		component, revision, source.SHA256, source.Size, payload.SHA256, payload.Size, license)
}
