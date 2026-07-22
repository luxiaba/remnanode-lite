package rnlctl

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateNativeTemporaryDirectoryUsesSafeTMPDIR(t *testing.T) {
	root := filepath.Join(t.TempDir(), "native-work")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", root)

	directory, err := createNativeTemporaryDirectory("fixture-*")
	if err != nil {
		t.Fatalf("createNativeTemporaryDirectory() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pathWithin(resolvedRoot, directory) {
		t.Fatalf("temporary directory %q is not below TMPDIR %q", directory, resolvedRoot)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("temporary directory mode = %04o, want 0700", got)
	}
}

func TestValidateNativeTemporaryRootRejectsReplaceableAncestor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ownership-chain enforcement is Linux-specific")
	}
	root := t.TempDir()
	unsafeParent := filepath.Join(root, "shared")
	if err := os.Mkdir(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(unsafeParent, "private")
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := validateNativeTemporaryRoot(candidate)
	if err == nil || !strings.Contains(err.Error(), "writable by another user") {
		t.Fatalf("validateNativeTemporaryRoot() error = %v, want unsafe ancestor rejection", err)
	}
}

func TestValidateNativeTemporaryRootRejectsCustomSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := validateNativeTemporaryRoot(link)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("validateNativeTemporaryRoot() error = %v, want symlink rejection", err)
	}
}
