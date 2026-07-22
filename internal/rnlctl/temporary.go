package rnlctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const managedTemporaryRoot = "/var/lib/remnanode-lite-installer/tmp"

// createNativeTemporaryDirectory keeps release archives and extracted bundles
// off a potentially memory-backed /tmp by default. A caller-supplied TMPDIR is
// used only when its directory chain cannot be replaced by another local user.
func createNativeTemporaryDirectory(pattern string) (string, error) {
	root, err := selectNativeTemporaryRoot()
	if err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp(root, pattern)
	if err != nil {
		return "", fmt.Errorf("create Native temporary directory below %s: %w", root, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("restrict Native temporary directory: %w", err)
	}
	return directory, nil
}

func selectNativeTemporaryRoot() (string, error) {
	var rejected []error
	if configured := os.Getenv("TMPDIR"); configured != "" {
		root, err := validateNativeTemporaryRoot(configured)
		if err == nil {
			return root, nil
		}
		rejected = append(rejected, fmt.Errorf("ignore unsafe TMPDIR %q: %w", configured, err))
	}

	if os.Geteuid() == 0 {
		if err := ensureDirectory(managedTemporaryRoot, 0o700); err == nil {
			root, validateErr := validateNativeTemporaryRoot(managedTemporaryRoot)
			if validateErr == nil {
				return root, nil
			}
			rejected = append(rejected, fmt.Errorf("validate managed temporary root: %w", validateErr))
		} else {
			rejected = append(rejected, fmt.Errorf("prepare managed temporary root: %w", err))
		}
	}

	// /var/tmp is conventionally backed by persistent storage. os.TempDir is a
	// last-resort compatibility fallback for non-root inspection and tests.
	for _, candidate := range []string{"/var/tmp", os.TempDir()} {
		if candidate == "" {
			continue
		}
		root, err := validateNativeTemporaryRoot(candidate)
		if err == nil {
			return root, nil
		}
		rejected = append(rejected, fmt.Errorf("temporary root %q is unusable: %w", candidate, err))
	}
	return "", fmt.Errorf("no safe Native temporary directory is available: %w", errors.Join(rejected...))
}

func validateNativeTemporaryRoot(candidate string) (string, error) {
	if !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("path must be absolute")
	}
	cleaned := filepath.Clean(candidate)
	if cleaned != "/tmp" && cleaned != "/var/tmp" {
		info, err := os.Lstat(cleaned)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("custom temporary root must not be a symlink")
		}
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}
	resolved = filepath.Clean(resolved)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}
	if err := validateNativeTemporaryChain(resolved); err != nil {
		return "", err
	}
	if err := unix.Access(resolved, unix.W_OK|unix.X_OK); err != nil {
		return "", fmt.Errorf("directory is not writable and searchable: %w", err)
	}
	return resolved, nil
}

func validateNativeTemporaryChain(directory string) error {
	effectiveUID := uint32(os.Geteuid())
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a real directory", current)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot inspect ownership of %s", current)
		}
		uid := uint32(stat.Uid)
		if uid != 0 && uid != effectiveUID {
			return fmt.Errorf("%s is owned by uid %d", current, uid)
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("%s is writable by another user without the sticky bit", current)
		}
		if current == string(filepath.Separator) {
			break
		}
	}
	return nil
}
