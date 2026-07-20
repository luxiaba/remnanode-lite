package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/contract"
)

const checkTimeout = 45 * time.Second

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	flags := flag.NewFlagSet("contract-source-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var source, manifestPath string
	var write bool
	flags.StringVar(&source, "source", getenv("REMNANODE_OFFICIAL_SOURCE"), "official Git repository containing the pinned commit")
	flags.StringVar(&manifestPath, "manifest", contract.OfficialSourceManifestPath, "checked-in source evidence manifest")
	flags.BoolVar(&write, "write", false, "replace the manifest with evidence extracted from the pinned Git commit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: contract-source-check -source PATH [-manifest PATH] [-write]")
		fmt.Fprintln(stderr, "The source worktree and index are ignored; evidence is read from the pinned Git commit object.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || source == "" || manifestPath == "" {
		flags.Usage()
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if write {
		raw, err := contract.GenerateOfficialSourceManifest(ctx, source)
		if err != nil {
			fmt.Fprintf(stderr, "generate official source manifest: %v\n", err)
			return 1
		}
		if err := contract.ValidateOfficialSourceManifest(raw); err != nil {
			fmt.Fprintf(stderr, "refuse to write a manifest that is not aligned with the local contract: %v\n", err)
			return 1
		}
		if err := writeAtomic(manifestPath, raw); err != nil {
			fmt.Fprintf(stderr, "write official source manifest: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote official source manifest from %s\n", contract.OfficialNodeCommit)
		return 0
	}

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "read official source manifest: %v\n", err)
		return 1
	}
	if err := contract.VerifyOfficialSourceManifest(ctx, source, raw); err != nil {
		fmt.Fprintf(stderr, "verify official source manifest: %v\n", err)
		return 1
	}
	fmt.Fprintf(
		stdout,
		"official source oracle passed: %s@%s (%s)\n",
		contract.OfficialNodeVersion,
		contract.OfficialNodeCommit,
		contract.OfficialNodeRepository,
	)
	return 0
}

func writeAtomic(path string, raw []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".official-source-manifest-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
