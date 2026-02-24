package fzf

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	// MinFZFMajor is the minimum required fzf major version.
	MinFZFMajor = 0
	// MinFZFMinor is the minimum required fzf minor version.
	MinFZFMinor = 40
	// MinFZFPatch is the minimum required fzf patch version.
	MinFZFPatch = 0
)

// versionRegexp matches a semver-like X.Y.Z pattern.
var versionRegexp = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseFZFVersion parses the output of `fzf --version` and extracts
// the major, minor, and patch version numbers. It handles formats like
// "0.35.1 (brew)", "0.44.0 (e5765b3)", and "0.40.0".
func ParseFZFVersion(versionOutput string) (major, minor, patch int, err error) {
	matches := versionRegexp.FindStringSubmatch(strings.TrimSpace(versionOutput))
	if matches == nil {
		return 0, 0, 0, fmt.Errorf("could not parse fzf version from %q", versionOutput)
	}

	major, err = strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err = strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %w", err)
	}
	patch, err = strconv.Atoi(matches[3])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version: %w", err)
	}

	return major, minor, patch, nil
}

// isVersionSufficient returns true if the given version meets the
// minimum required fzf version defined by the MinFZF* constants.
func isVersionSufficient(major, minor, patch int) bool {
	if major != MinFZFMajor {
		return major > MinFZFMajor
	}
	if minor != MinFZFMinor {
		return minor > MinFZFMinor
	}
	return patch >= MinFZFPatch
}

// CheckFZFVersion runs `fzf --version`, parses the output, and returns
// an error if fzf is missing or the installed version is too old.
func CheckFZFVersion() error {
	out, err := exec.Command("fzf", "--version").Output()
	if err != nil {
		return fmt.Errorf("fzf is not installed. Please install it: brew install fzf")
	}

	major, minor, patch, err := ParseFZFVersion(string(out))
	if err != nil {
		return fmt.Errorf("fzf is installed but version could not be determined: %v", err)
	}

	if !isVersionSufficient(major, minor, patch) {
		return fmt.Errorf(
			"fzf version %d.%d.%d is installed, but version %d.%d.%d or later is required. Please upgrade: brew upgrade fzf",
			major, minor, patch, MinFZFMajor, MinFZFMinor, MinFZFPatch,
		)
	}

	return nil
}
