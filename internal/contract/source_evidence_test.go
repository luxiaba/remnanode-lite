package contract

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOfficialSourceManifestMatchesContract(t *testing.T) {
	raw, err := os.ReadFile("official-source-manifest.json")
	if err != nil {
		t.Fatalf("read official source manifest: %v", err)
	}
	if err := ValidateOfficialSourceManifest(raw); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedOfficialSourceEvidence(t *testing.T) {
	repository := os.Getenv("REMNANODE_OFFICIAL_SOURCE")
	if repository == "" {
		t.Skip("REMNANODE_OFFICIAL_SOURCE is not set")
	}

	raw, err := os.ReadFile("official-source-manifest.json")
	if err != nil {
		t.Fatalf("read official source manifest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := VerifyOfficialSourceManifest(ctx, repository, raw); err != nil {
		t.Fatal(err)
	}
}
