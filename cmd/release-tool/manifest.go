package main

import (
	"bytes"
	"crypto/sha1" // SPDX 2.3 mandates SHA-1 for PackageVerificationCode.
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	manifestSchemaVersion = 2
	bundleName            = "remnanode-lite"
	bundleOS              = "linux"
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

type manifestRuntimeAssets struct {
	Xray    manifestXray    `json:"xray"`
	GeoIP   runtimeDataLock `json:"geoIP"`
	GeoSite runtimeDataLock `json:"geoSite"`
	ASN     manifestASN     `json:"asn"`
}

type manifestXray struct {
	Version   string                 `json:"version"`
	Commit    string                 `json:"commit"`
	SourceURL string                 `json:"sourceURL"`
	Archive   artifactLock           `json:"archive"`
	Core      manifestRuntimePayload `json:"core"`
}

type manifestASN struct {
	Commit string       `json:"commit"`
	Source artifactLock `json:"source"`
	Output outputLock   `json:"output"`
}

type manifestRuntimePayload struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	License string `json:"license"`
}

type manifestFile struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Role    string `json:"role"`
	License string `json:"license"`
}

type bundleFile struct {
	Path    string
	Mode    int64
	Data    []byte
	Role    string
	License string
}

func buildManifest(options buildOptions, lock runtimeLock, lockSHA256 string, architecture xrayArchitecture, files []bundleFile) (releaseManifest, error) {
	manifestFiles := make([]manifestFile, 0, len(files))
	for _, file := range files {
		if err := validateBundleFile(file); err != nil {
			return releaseManifest{}, err
		}
		manifestFiles = append(manifestFiles, manifestFile{
			Path:    file.Path,
			Mode:    fmt.Sprintf("%04o", file.Mode),
			Size:    int64(len(file.Data)),
			SHA256:  digestBytes(file.Data),
			Role:    file.Role,
			License: file.License,
		})
	}
	sort.Slice(manifestFiles, func(left, right int) bool {
		return manifestFiles[left].Path < manifestFiles[right].Path
	})

	return releaseManifest{
		SchemaVersion:          manifestSchemaVersion,
		Name:                   bundleName,
		Version:                options.version,
		ContractVersion:        options.contractVersion,
		OS:                     bundleOS,
		Architecture:           options.architecture,
		SourceRevision:         options.sourceRevision,
		SourceDateEpoch:        options.sourceDateEpoch,
		RuntimeAssetLockSHA256: lockSHA256,
		RuntimeAssets: manifestRuntimeAssets{
			Xray: manifestXray{
				Version:   lock.Xray.Version,
				Commit:    lock.Xray.Commit,
				SourceURL: lock.Xray.SourceURL,
				Archive:   architecture.Archive,
				Core:      runtimePayload(architecture.Core),
			},
			GeoIP:   lock.GeoIP,
			GeoSite: lock.GeoSite,
			ASN: manifestASN{
				Commit: lock.ASN.Commit,
				Source: lock.ASN.Source,
				Output: lock.ASN.Output,
			},
		},
		Files: manifestFiles,
	}, nil
}

func runtimePayload(entry archiveEntry) manifestRuntimePayload {
	return manifestRuntimePayload{SHA256: entry.SHA256, Size: entry.Size, License: entry.License}
}

func validateBundleFile(file bundleFile) error {
	if err := validateArchiveRelativePath(file.Path, 512); err != nil {
		return fmt.Errorf("invalid bundle path %q: %w", file.Path, err)
	}
	if file.Mode != 0o644 && file.Mode != 0o755 {
		return fmt.Errorf("bundle file %q has unsupported mode %04o", file.Path, file.Mode)
	}
	if file.Role == "" || file.License == "" {
		return fmt.Errorf("bundle file %q requires role and license metadata", file.Path)
	}
	return nil
}

func marshalDeterministicJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Files             []spdxFile         `json:"files"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name                    string                       `json:"name"`
	SPDXID                  string                       `json:"SPDXID"`
	VersionInfo             string                       `json:"versionInfo"`
	DownloadLocation        string                       `json:"downloadLocation"`
	FilesAnalyzed           bool                         `json:"filesAnalyzed"`
	PackageVerificationCode *spdxPackageVerificationCode `json:"packageVerificationCode,omitempty"`
	LicenseConcluded        string                       `json:"licenseConcluded"`
	LicenseDeclared         string                       `json:"licenseDeclared"`
	CopyrightText           string                       `json:"copyrightText"`
	SourceInfo              string                       `json:"sourceInfo,omitempty"`
	ExternalRefs            []spdxExternalRef            `json:"externalRefs,omitempty"`
}

type spdxPackageVerificationCode struct {
	Value string `json:"packageVerificationCodeValue"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxFile struct {
	FileName           string         `json:"fileName"`
	SPDXID             string         `json:"SPDXID"`
	Checksums          []spdxChecksum `json:"checksums"`
	LicenseConcluded   string         `json:"licenseConcluded"`
	LicenseInfoInFiles []string       `json:"licenseInfoInFiles"`
	CopyrightText      string         `json:"copyrightText"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func buildSPDX(options buildOptions, lock runtimeLock, files []bundleFile, goModules []goModule) spdxDocument {
	const (
		bundlePackage  = "SPDXRef-Package-native-bundle"
		projectPackage = "SPDXRef-Package-remnanode-lite"
		xrayPackage    = "SPDXRef-Package-xray-core"
		geoIPPackage   = "SPDXRef-Package-geoip-data"
		geoSitePackage = "SPDXRef-Package-geosite-data"
		asnPackage     = "SPDXRef-Package-asn-database"
		asnSource      = "SPDXRef-Package-asn-source"
	)
	spdxFiles := make([]spdxFile, 0, len(files))
	packageSHA1 := make(map[string][]string)
	relationships := []spdxRelationship{
		{SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: bundlePackage},
		{SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: projectPackage},
		{SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: xrayPackage},
		{SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: geoIPPackage},
		{SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: geoSitePackage},
		{SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: asnPackage},
		{SPDXElementID: asnPackage, RelationshipType: "GENERATED_FROM", RelatedSPDXElement: asnSource},
	}
	fileIdentifiers := make(map[string]string, len(files))
	for index, file := range files {
		identifier := fmt.Sprintf("SPDXRef-File-%04d", index+1)
		fileIdentifiers[file.Path] = identifier
		sha1Digest := fmt.Sprintf("%x", sha1.Sum(file.Data))
		spdxFiles = append(spdxFiles, spdxFile{
			FileName: "./" + file.Path,
			SPDXID:   identifier,
			Checksums: []spdxChecksum{
				{Algorithm: "SHA1", ChecksumValue: sha1Digest},
				{Algorithm: "SHA256", ChecksumValue: digestBytes(file.Data)},
			},
			LicenseConcluded:   file.License,
			LicenseInfoInFiles: []string{"NOASSERTION"},
			CopyrightText:      "NOASSERTION",
		})
		owner := spdxOwnerForPath(file.Path)
		packageSHA1[bundlePackage] = append(packageSHA1[bundlePackage], sha1Digest)
		packageSHA1[owner] = append(packageSHA1[owner], sha1Digest)
		relationships = append(relationships, spdxRelationship{
			SPDXElementID: bundlePackage, RelationshipType: "CONTAINS", RelatedSPDXElement: identifier,
		})
		relationships = append(relationships, spdxRelationship{
			SPDXElementID: owner, RelationshipType: "CONTAINS", RelatedSPDXElement: identifier,
		})
	}

	packages := []spdxPackage{
		{
			Name: "remnanode-lite-native-bundle", SPDXID: bundlePackage, VersionInfo: options.version,
			DownloadLocation: "https://github.com/luxiaba/remnanode-lite/releases", FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[bundlePackage]),
			LicenseConcluded:        "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION",
		},
		{
			Name: "remnanode-lite", SPDXID: projectPackage, VersionInfo: options.version,
			DownloadLocation: "https://github.com/luxiaba/remnanode-lite/tree/" + options.sourceRevision, FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[projectPackage]),
			LicenseConcluded:        "AGPL-3.0-only", LicenseDeclared: "AGPL-3.0-only", CopyrightText: "NOASSERTION",
			ExternalRefs: []spdxExternalRef{{ReferenceCategory: "PACKAGE-MANAGER", ReferenceType: "purl", ReferenceLocator: "pkg:github/luxiaba/remnanode-lite@" + options.sourceRevision}},
		},
		{
			Name: "Xray-core", SPDXID: xrayPackage, VersionInfo: lock.Xray.Version,
			DownloadLocation: lock.Xray.SourceURL, FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[xrayPackage]),
			LicenseConcluded:        "MPL-2.0", LicenseDeclared: "MPL-2.0", CopyrightText: "NOASSERTION",
			ExternalRefs: []spdxExternalRef{{ReferenceCategory: "PACKAGE-MANAGER", ReferenceType: "purl", ReferenceLocator: "pkg:github/xtls/xray-core@" + lock.Xray.Version}},
		},
		{
			Name: "Loyalsoldier GeoIP data", SPDXID: geoIPPackage, VersionInfo: lock.GeoIP.Version,
			DownloadLocation: lock.GeoIP.Artifact.URL, FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[geoIPPackage]),
			LicenseConcluded:        lock.GeoIP.License, LicenseDeclared: lock.GeoIP.License, CopyrightText: "NOASSERTION",
			SourceInfo: lock.GeoIP.LicenseRationale,
		},
		{
			Name: "Loyalsoldier GeoSite data", SPDXID: geoSitePackage, VersionInfo: lock.GeoSite.Version,
			DownloadLocation: lock.GeoSite.Artifact.URL, FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[geoSitePackage]),
			LicenseConcluded:        lock.GeoSite.License, LicenseDeclared: lock.GeoSite.License, CopyrightText: "NOASSERTION",
		},
		{
			Name: "remnanode-lite ASN database", SPDXID: asnPackage, VersionInfo: lock.ASN.Commit,
			DownloadLocation: "NOASSERTION", FilesAnalyzed: true,
			PackageVerificationCode: packageVerificationCode(packageSHA1[asnPackage]),
			LicenseConcluded:        lock.ASN.Output.License, LicenseDeclared: lock.ASN.Output.License, CopyrightText: "NOASSERTION",
		},
		{
			Name: "ipverse-as-ip-blocks", SPDXID: asnSource, VersionInfo: lock.ASN.Commit,
			DownloadLocation: lock.ASN.Source.URL, FilesAnalyzed: false,
			LicenseConcluded: "CC0-1.0", LicenseDeclared: "CC0-1.0", CopyrightText: "NOASSERTION",
			ExternalRefs: []spdxExternalRef{{ReferenceCategory: "PACKAGE-MANAGER", ReferenceType: "purl", ReferenceLocator: "pkg:github/ipverse/as-ip-blocks@" + lock.ASN.Commit}},
		},
	}
	for _, module := range goModules {
		identifier := goModuleSPDXID(module)
		packages = append(packages, spdxPackage{
			Name: module.Path, SPDXID: identifier, VersionInfo: module.Version,
			DownloadLocation: "NOASSERTION", FilesAnalyzed: false,
			LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION",
			SourceInfo:   "Go module checksum: " + module.Sum,
			ExternalRefs: []spdxExternalRef{{ReferenceCategory: "PACKAGE-MANAGER", ReferenceType: "purl", ReferenceLocator: goModulePURL(module)}},
		})
		for _, binaryPath := range module.UsedBy {
			relationships = append(relationships, spdxRelationship{
				SPDXElementID: fileIdentifiers[binaryPath], RelationshipType: "STATIC_LINK", RelatedSPDXElement: identifier,
			})
		}
	}
	sort.Slice(relationships, func(left, right int) bool {
		if relationships[left].SPDXElementID != relationships[right].SPDXElementID {
			return relationships[left].SPDXElementID < relationships[right].SPDXElementID
		}
		if relationships[left].RelationshipType != relationships[right].RelationshipType {
			return relationships[left].RelationshipType < relationships[right].RelationshipType
		}
		return relationships[left].RelatedSPDXElement < relationships[right].RelatedSPDXElement
	})

	return spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              fmt.Sprintf("remnanode-lite-%s-linux-%s", options.version, options.architecture),
		DocumentNamespace: fmt.Sprintf("https://github.com/luxiaba/remnanode-lite/sbom/%s/%s/%s", options.version, options.architecture, options.sourceRevision),
		CreationInfo: spdxCreationInfo{
			Created:  time.Unix(options.sourceDateEpoch, 0).UTC().Format("2006-01-02T15:04:05Z"),
			Creators: []string{"Tool: remnanode-lite-release-tool"},
		},
		Packages:      packages,
		Files:         spdxFiles,
		Relationships: relationships,
	}
}

func spdxOwnerForPath(filePath string) string {
	switch filePath {
	case "lib/rw-core":
		return "SPDXRef-Package-xray-core"
	case "share/xray/geoip.dat":
		return "SPDXRef-Package-geoip-data"
	case "share/xray/geosite.dat":
		return "SPDXRef-Package-geosite-data"
	case "share/asn/asn-prefixes.bin":
		return "SPDXRef-Package-asn-database"
	default:
		return "SPDXRef-Package-remnanode-lite"
	}
}

func goModuleSPDXID(module goModule) string {
	return "SPDXRef-GoModule-" + digestBytes([]byte(module.Path + "\x00" + module.Version))[:20]
}

func goModulePURL(module goModule) string {
	escapePath := func(value string) string {
		return strings.ReplaceAll(url.PathEscape(value), "%2F", "/")
	}
	return "pkg:golang/" + escapePath(module.Path) + "@" + url.PathEscape(module.Version)
}

func packageVerificationCode(fileSHA1 []string) *spdxPackageVerificationCode {
	values := append([]string(nil), fileSHA1...)
	sort.Strings(values)
	hasher := sha1.New() // SPDX 2.3 section 7.9 specifies this exact construction.
	for _, value := range values {
		_, _ = hasher.Write([]byte(value))
	}
	return &spdxPackageVerificationCode{Value: fmt.Sprintf("%x", hasher.Sum(nil))}
}

func validateSPDX(document spdxDocument, files map[string]manifestFile, entries map[string]archivedEntry, goModules []goModule) error {
	if document.SPDXVersion != "SPDX-2.3" || document.DataLicense != "CC0-1.0" || document.SPDXID != "SPDXRef-DOCUMENT" {
		return fmt.Errorf("SBOM does not identify a valid SPDX 2.3 document")
	}
	if document.Name == "" || document.DocumentNamespace == "" || len(document.Packages) < 7 {
		return fmt.Errorf("SBOM is missing document or package identity")
	}
	seen := make(map[string]struct{}, len(document.Files))
	packageSHA1 := make(map[string][]string)
	allSHA1 := make([]string, 0, len(document.Files))
	elementIDs := map[string]struct{}{"SPDXRef-DOCUMENT": {}}
	for _, file := range document.Files {
		pathName := strings.TrimPrefix(file.FileName, "./")
		manifestEntry, exists := files[pathName]
		if !exists || pathName == "SBOM.spdx.json" {
			return fmt.Errorf("SBOM references unknown file %q", file.FileName)
		}
		if _, duplicate := seen[pathName]; duplicate {
			return fmt.Errorf("SBOM contains duplicate file %q", file.FileName)
		}
		seen[pathName] = struct{}{}
		if _, duplicate := elementIDs[file.SPDXID]; duplicate || file.SPDXID == "" {
			return fmt.Errorf("SBOM contains duplicate or empty file SPDX identifier %q", file.SPDXID)
		}
		elementIDs[file.SPDXID] = struct{}{}
		archiveEntry, exists := entries[path.Join(bundleName, pathName)]
		if !exists || archiveEntry.isDir {
			return fmt.Errorf("SBOM references missing archive file %q", file.FileName)
		}
		checksums := make(map[string]string, len(file.Checksums))
		for _, checksum := range file.Checksums {
			if _, duplicate := checksums[checksum.Algorithm]; duplicate {
				return fmt.Errorf("SBOM contains duplicate %s checksum for %q", checksum.Algorithm, file.FileName)
			}
			checksums[checksum.Algorithm] = checksum.ChecksumValue
		}
		sha1Digest := fmt.Sprintf("%x", sha1.Sum(archiveEntry.data))
		if len(checksums) != 2 || checksums["SHA256"] != manifestEntry.SHA256 || checksums["SHA1"] != sha1Digest {
			return fmt.Errorf("SBOM checksum for %q does not match manifest", file.FileName)
		}
		owner := spdxOwnerForPath(pathName)
		packageSHA1[owner] = append(packageSHA1[owner], sha1Digest)
		allSHA1 = append(allSHA1, sha1Digest)
	}
	if len(seen) != len(files)-1 {
		return fmt.Errorf("SBOM describes %d files, want %d non-SBOM payload files", len(seen), len(files)-1)
	}
	wantAnalyzed := map[string]*spdxPackageVerificationCode{
		"SPDXRef-Package-native-bundle":  packageVerificationCode(allSHA1),
		"SPDXRef-Package-remnanode-lite": packageVerificationCode(packageSHA1["SPDXRef-Package-remnanode-lite"]),
		"SPDXRef-Package-xray-core":      packageVerificationCode(packageSHA1["SPDXRef-Package-xray-core"]),
		"SPDXRef-Package-geoip-data":     packageVerificationCode(packageSHA1["SPDXRef-Package-geoip-data"]),
		"SPDXRef-Package-geosite-data":   packageVerificationCode(packageSHA1["SPDXRef-Package-geosite-data"]),
		"SPDXRef-Package-asn-database":   packageVerificationCode(packageSHA1["SPDXRef-Package-asn-database"]),
	}
	wantUnanalyzed := map[string]struct{}{"SPDXRef-Package-asn-source": {}}
	for _, module := range goModules {
		wantUnanalyzed[goModuleSPDXID(module)] = struct{}{}
	}
	wantPackageCount := len(wantAnalyzed) + len(wantUnanalyzed)
	if len(document.Packages) != wantPackageCount {
		return fmt.Errorf("SBOM package count is %d, want %d", len(document.Packages), wantPackageCount)
	}
	seenPackages := make(map[string]struct{}, len(document.Packages))
	for _, pkg := range document.Packages {
		if _, duplicate := seenPackages[pkg.SPDXID]; duplicate {
			return fmt.Errorf("SBOM contains duplicate package %q", pkg.SPDXID)
		}
		seenPackages[pkg.SPDXID] = struct{}{}
		if _, duplicate := elementIDs[pkg.SPDXID]; duplicate || pkg.SPDXID == "" {
			return fmt.Errorf("SBOM contains duplicate or empty package SPDX identifier %q", pkg.SPDXID)
		}
		elementIDs[pkg.SPDXID] = struct{}{}
		if wantCode, exists := wantAnalyzed[pkg.SPDXID]; exists {
			if !pkg.FilesAnalyzed || !reflect.DeepEqual(pkg.PackageVerificationCode, wantCode) {
				return fmt.Errorf("SBOM package verification code for %q is invalid", pkg.SPDXID)
			}
			continue
		}
		if _, exists := wantUnanalyzed[pkg.SPDXID]; !exists {
			return fmt.Errorf("SBOM contains unexpected package %q", pkg.SPDXID)
		}
		if pkg.FilesAnalyzed || pkg.PackageVerificationCode != nil {
			return fmt.Errorf("SBOM unanalyzed package %q must omit packageVerificationCode", pkg.SPDXID)
		}
	}
	seenRelationships := make(map[spdxRelationship]struct{}, len(document.Relationships))
	for _, relationship := range document.Relationships {
		if _, exists := elementIDs[relationship.SPDXElementID]; !exists {
			return fmt.Errorf("SBOM relationship references unknown element %q", relationship.SPDXElementID)
		}
		if _, exists := elementIDs[relationship.RelatedSPDXElement]; !exists {
			return fmt.Errorf("SBOM relationship references unknown related element %q", relationship.RelatedSPDXElement)
		}
		if relationship.RelationshipType == "" {
			return fmt.Errorf("SBOM relationship has an empty type")
		}
		if _, duplicate := seenRelationships[relationship]; duplicate {
			return fmt.Errorf("SBOM contains a duplicate relationship")
		}
		seenRelationships[relationship] = struct{}{}
	}
	return nil
}
