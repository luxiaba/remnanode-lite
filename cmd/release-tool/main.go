// Command release-tool validates pinned runtime assets and builds or verifies
// deterministic, self-contained Native Linux release bundles.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const usageText = `Usage: release-tool <command> [options]

Commands:
  metadata  Classify the source version and emit release workflow outputs
  validate  Strictly validate the runtime asset lock
  materialize  Materialize locked runtime assets for one architecture
  build     Build and verify one deterministic Native Linux bundle
  verify    Verify bundle structure, manifest, payload digests, and SBOM
  assemble  Assemble the complete GitHub Release asset set
  verify-package  Verify a complete GitHub Release asset set
  verify-index  Verify the accepted multi-architecture OCI image index
  verify-release  Compare a GitHub Release API snapshot with local assets

Run "release-tool <command> --help" for command-specific options.
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "release-tool: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usageText)
		return fmt.Errorf("a command is required")
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, usageText)
		return nil
	case "metadata":
		return runMetadata(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "materialize":
		return runMaterialize(ctx, args[1:], stdout, stderr)
	case "build":
		return runBuild(ctx, args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "assemble":
		return runAssemble(args[1:], stdout, stderr)
	case "verify-package":
		return runVerifyPackage(args[1:], stdout, stderr)
	case "verify-index":
		return runVerifyIndex(args[1:], stdout, stderr)
	case "verify-release":
		return runVerifyRelease(args[1:], stdout, stderr)
	default:
		_, _ = io.WriteString(stderr, usageText)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runMaterialize(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("materialize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options materializeOptions
	flags.StringVar(&options.lockPath, "lock", "release/runtime-assets.lock.json", "runtime asset lock path")
	flags.StringVar(&options.architecture, "arch", "", "target architecture: amd64 or arm64")
	flags.StringVar(&options.asnBuilderPath, "asn-builder", "", "host asn-builder executable")
	flags.StringVar(&options.cacheDirectory, "cache-dir", "", "content-addressed runtime asset cache")
	flags.StringVar(&options.outputDirectory, "out-dir", "", "new output directory for the materialized runtime tree")
	flags.BoolVar(&options.offline, "offline", false, "forbid downloads and require every asset in cache")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("materialize does not accept positional arguments")
	}
	if err := materializeRuntimeAssets(ctx, options); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "materialized locked linux/%s runtime assets: %s\n", options.architecture, options.outputDirectory)
	return nil
}

func runValidate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockPath := flags.String("lock", "release/runtime-assets.lock.json", "runtime asset lock path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("validate does not accept positional arguments")
	}
	if _, err := loadRuntimeLock(*lockPath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "validated runtime asset lock: %s\n", *lockPath)
	return nil
}

func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options buildOptions
	flags.StringVar(&options.lockPath, "lock", "release/runtime-assets.lock.json", "runtime asset lock path")
	flags.StringVar(&options.architecture, "arch", "", "target architecture: amd64 or arm64")
	flags.StringVar(&options.version, "version", "", "project version")
	flags.StringVar(&options.contractVersion, "contract-version", "", "Panel contract version")
	flags.StringVar(&options.sourceRevision, "source-revision", "", "40-character source Git commit")
	flags.Int64Var(&options.sourceDateEpoch, "source-date-epoch", 0, "canonical Unix timestamp for archive entries")
	flags.StringVar(&options.projectRoot, "project-root", ".", "repository root containing LICENSE and release/bundle")
	flags.StringVar(&options.nodePath, "node", "", "target remnanode-lite binary")
	flags.StringVar(&options.rnlctlPath, "rnlctl", "", "target rnlctl binary")
	flags.StringVar(&options.asnBuilderPath, "asn-builder", "", "host asn-builder executable")
	flags.StringVar(&options.installerPath, "installer", "", "self-contained Native install.sh")
	flags.StringVar(&options.supportDirectory, "support-dir", "", "support tree copied beneath support/")
	flags.StringVar(&options.cacheDirectory, "cache-dir", "", "content-addressed runtime asset cache")
	flags.StringVar(&options.outputPath, "out", "", "output .tar.gz path")
	flags.BoolVar(&options.offline, "offline", false, "forbid downloads and require every asset in cache")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("build does not accept positional arguments")
	}
	if err := buildBundle(ctx, options); err != nil {
		return err
	}
	digest, size, err := fileDigestAndSize(options.outputPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "built %s (%d bytes, sha256:%s)\n", options.outputPath, size, digest)
	return nil
}

func runVerify(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options verifyOptions
	flags.StringVar(&options.lockPath, "lock", "release/runtime-assets.lock.json", "runtime asset lock path")
	flags.StringVar(&options.archivePath, "archive", "", "Native bundle .tar.gz path")
	flags.StringVar(&options.architecture, "arch", "", "expected architecture: amd64 or arm64")
	flags.StringVar(&options.version, "version", "", "optional expected project version")
	flags.StringVar(&options.contractVersion, "contract-version", "", "optional expected Panel contract version")
	flags.StringVar(&options.sourceRevision, "source-revision", "", "optional expected source Git commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("verify does not accept positional arguments")
	}
	if options.archivePath == "" || options.architecture == "" {
		return fmt.Errorf("verify requires --archive and --arch")
	}
	if err := verifyBundle(options); err != nil {
		return err
	}
	digest, size, err := fileDigestAndSize(options.archivePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "verified %s (%d bytes, sha256:%s)\n", options.archivePath, size, digest)
	return nil
}

func fileDigestAndSize(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
