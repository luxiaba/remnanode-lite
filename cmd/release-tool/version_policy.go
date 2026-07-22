package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Release versions intentionally use a strict numeric form. In particular,
// accepting a leading zero would create a second spelling for the same
// semantic version and would disagree with the Git tag and installer rules.
var (
	projectVersionPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rnl\.[1-9][0-9]*)?$`)
	contractVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

func validateProjectVersion(version string) error {
	if !projectVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid project version %q", version)
	}
	return nil
}

func validateContractVersion(version string) error {
	if !contractVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid contract version %q", version)
	}
	return nil
}

// validateVersionPair enforces the release policy shared by Native bundles
// and their verifier. An rnl.N build may continue to carry an older contract;
// a plain X.Y.Z build is an official-alignment release and must match it.
func validateVersionPair(projectVersion, contractVersion string) error {
	if err := validateProjectVersion(projectVersion); err != nil {
		return err
	}
	if err := validateContractVersion(contractVersion); err != nil {
		return err
	}
	if !strings.Contains(projectVersion, "-rnl.") && projectVersion != contractVersion {
		return fmt.Errorf("stable project version %q must equal contract version %q", projectVersion, contractVersion)
	}
	return nil
}
