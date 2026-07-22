package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

const maxOCIIndexBytes = 4 << 20

type ociIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []ociDescriptor `json:"manifests"`
}

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    ociPlatform       `json:"platform"`
	Annotations map[string]string `json:"annotations"`
}

type ociPlatform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

func verifyOCIIndex(data []byte, expectedDigest string) error {
	if len(data) == 0 || len(data) > maxOCIIndexBytes {
		return fmt.Errorf("OCI index size is outside 1..%d bytes", maxOCIIndexBytes)
	}
	digest := sha256.Sum256(data)
	actualDigest := "sha256:" + hex.EncodeToString(digest[:])
	if actualDigest != expectedDigest {
		return fmt.Errorf("OCI index digest is %s, want %s", actualDigest, expectedDigest)
	}
	var index ociIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("decode OCI index: %w", err)
	}
	if index.SchemaVersion != 2 {
		return fmt.Errorf("OCI index schemaVersion is %d, want 2", index.SchemaVersion)
	}
	if index.MediaType != "application/vnd.oci.image.index.v1+json" &&
		index.MediaType != "application/vnd.docker.distribution.manifest.list.v2+json" {
		return fmt.Errorf("unsupported OCI index media type %q", index.MediaType)
	}
	runnable := make(map[string]string, 2)
	attested := make(map[string]struct{}, 2)
	seenDescriptorDigests := make(map[string]struct{}, len(index.Manifests))
	for _, descriptor := range index.Manifests {
		if !validSHA256Digest(descriptor.Digest) || descriptor.Size <= 0 {
			return fmt.Errorf("OCI descriptor has invalid digest or size")
		}
		if _, duplicate := seenDescriptorDigests[descriptor.Digest]; duplicate {
			return fmt.Errorf("OCI index repeats descriptor digest %s", descriptor.Digest)
		}
		seenDescriptorDigests[descriptor.Digest] = struct{}{}
		referenceType := descriptor.Annotations["vnd.docker.reference.type"]
		switch referenceType {
		case "":
			if descriptor.MediaType != "application/vnd.oci.image.manifest.v1+json" &&
				descriptor.MediaType != "application/vnd.docker.distribution.manifest.v2+json" {
				return fmt.Errorf("runnable descriptor has unsupported media type %q", descriptor.MediaType)
			}
			if descriptor.Platform.OS != "linux" ||
				(descriptor.Platform.Architecture != "amd64" && descriptor.Platform.Architecture != "arm64") {
				return fmt.Errorf("unexpected runnable platform %s/%s", descriptor.Platform.OS, descriptor.Platform.Architecture)
			}
			platform := descriptor.Platform.OS + "/" + descriptor.Platform.Architecture
			if _, duplicate := runnable[platform]; duplicate {
				return fmt.Errorf("OCI index repeats runnable platform %s", platform)
			}
			runnable[platform] = descriptor.Digest
		case "attestation-manifest":
			if descriptor.MediaType != "application/vnd.oci.image.manifest.v1+json" ||
				descriptor.Platform.OS != "unknown" || descriptor.Platform.Architecture != "unknown" {
				return fmt.Errorf("invalid attestation descriptor")
			}
			referenced := descriptor.Annotations["vnd.docker.reference.digest"]
			if !validSHA256Digest(referenced) {
				return fmt.Errorf("attestation descriptor has invalid subject digest")
			}
			attested[referenced] = struct{}{}
		default:
			return fmt.Errorf("unsupported OCI reference type %q", referenceType)
		}
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		digest, exists := runnable[platform]
		if !exists {
			return fmt.Errorf("OCI index is missing runnable platform %s", platform)
		}
		if _, covered := attested[digest]; !covered {
			return fmt.Errorf("OCI index has no attestation for runnable platform %s", platform)
		}
	}
	if len(runnable) != 2 {
		return fmt.Errorf("OCI index contains unsupported runnable platforms")
	}
	return nil
}

func validSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+64 || value[:len("sha256:")] != "sha256:" {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if character < '0' || (character > '9' && character < 'a') || character > 'f' {
			return false
		}
	}
	return true
}

func runVerifyIndex(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify-index", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "raw OCI index JSON")
	digest := flags.String("digest", "", "expected sha256 index digest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("verify-index does not accept positional arguments")
	}
	if *manifestPath == "" || !validSHA256Digest(*digest) {
		return fmt.Errorf("verify-index requires --manifest and a valid --digest")
	}
	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read OCI index: %w", err)
	}
	if err := verifyOCIIndex(data, *digest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "verified two-platform OCI index %s\n", *digest)
	return nil
}
