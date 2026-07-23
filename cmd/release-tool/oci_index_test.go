package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifyOCIIndex(t *testing.T) {
	amd64Digest := "sha256:" + strings.Repeat("a", 64)
	arm64Digest := "sha256:" + strings.Repeat("b", 64)
	index := ociIndex{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.index.v1+json",
		Manifests: []ociDescriptor{
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: amd64Digest, Size: 100,
				Platform: ociPlatform{OS: "linux", Architecture: "amd64"},
			},
			{
				MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: arm64Digest, Size: 101,
				Platform: ociPlatform{OS: "linux", Architecture: "arm64"},
			},
			attestationDescriptor("c", amd64Digest),
			attestationDescriptor("d", arm64Digest),
		},
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if err := verifyOCIIndex(data, "sha256:"+hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("verifyOCIIndex(): %v", err)
	}

	index.Manifests = append(index.Manifests, attestationDescriptor("e", amd64Digest))
	data, _ = json.Marshal(index)
	digest = sha256.Sum256(data)
	if err := verifyOCIIndex(data, "sha256:"+hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("verifyOCIIndex() rejected an additional attestation: %v", err)
	}

	index.Manifests[3].Annotations["vnd.docker.reference.digest"] = amd64Digest
	data, _ = json.Marshal(index)
	digest = sha256.Sum256(data)
	if err := verifyOCIIndex(data, "sha256:"+hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("verifyOCIIndex() accepted duplicate attestation coverage")
	}
}

func TestValidSHA256Digest(t *testing.T) {
	if !validSHA256Digest("sha256:" + strings.Repeat("0", 64)) {
		t.Fatal("validSHA256Digest() rejected a valid digest")
	}
	for _, invalid := range []string{
		"sha256:" + strings.Repeat("0", 63),
		"sha256:" + strings.Repeat("G", 64),
		"sha512:" + strings.Repeat("0", 64),
	} {
		if validSHA256Digest(invalid) {
			t.Fatalf("validSHA256Digest() accepted %q", invalid)
		}
	}
}

func attestationDescriptor(seed, subject string) ociDescriptor {
	return ociDescriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    "sha256:" + strings.Repeat(seed, 64),
		Size:      50,
		Platform:  ociPlatform{OS: "unknown", Architecture: "unknown"},
		Annotations: map[string]string{
			"vnd.docker.reference.type":   "attestation-manifest",
			"vnd.docker.reference.digest": subject,
		},
	}
}
