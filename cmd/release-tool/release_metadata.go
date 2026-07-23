package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	projectversion "github.com/luxiaba/remnanode-lite/internal/version"
)

type releaseMetadata struct {
	Version    string
	Tag        string
	Channel    string
	Prerelease bool
	MakeLatest bool
}

func classifyRelease(version, contractVersion string) (releaseMetadata, error) {
	if err := validateVersionPair(version, contractVersion); err != nil {
		return releaseMetadata{}, err
	}
	return classifyReleaseVersion(version)
}

func classifyReleaseVersion(version string) (releaseMetadata, error) {
	if err := validateProjectVersion(version); err != nil {
		return releaseMetadata{}, err
	}
	metadata := releaseMetadata{
		Version: version,
		Tag:     version,
	}
	if strings.Contains(version, "-rnl.") {
		metadata.Channel = "preview"
		metadata.Prerelease = true
		return metadata, nil
	}
	metadata.Channel = "latest"
	metadata.MakeLatest = true
	return metadata, nil
}

func runMetadata(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("metadata", flag.ContinueOnError)
	flags.SetOutput(stderr)
	requestedVersion := flags.String("version", "", "expected source version; defaults to the embedded project version")
	requestedTag := flags.String("tag", "", "classify an existing release tag independently of the source version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("metadata does not accept positional arguments")
	}
	if *requestedVersion != "" && *requestedTag != "" {
		return fmt.Errorf("metadata accepts either --version or --tag, not both")
	}
	var metadata releaseMetadata
	var err error
	if *requestedTag != "" {
		metadata, err = classifyReleaseVersion(*requestedTag)
	} else {
		version := projectversion.Version
		if *requestedVersion != "" && *requestedVersion != version {
			return fmt.Errorf("requested version %q does not match source version %q", *requestedVersion, version)
		}
		metadata, err = classifyRelease(version, projectversion.ContractVersion)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "version=%s\n", metadata.Version)
	fmt.Fprintf(stdout, "tag=%s\n", metadata.Tag)
	fmt.Fprintf(stdout, "channel=%s\n", metadata.Channel)
	fmt.Fprintf(stdout, "prerelease=%t\n", metadata.Prerelease)
	fmt.Fprintf(stdout, "make_latest=%t\n", metadata.MakeLatest)
	return nil
}
