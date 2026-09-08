package update

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

const repository = "MiguelRodo/github-projects-skill"

// Result describes the installed and latest published versions. Check never
// installs or upgrades anything.
type Result struct {
	Installed          string `json:"installed"`
	Latest             string `json:"latest,omitempty"`
	UpdateAvailable    bool   `json:"updateAvailable"`
	Development        bool   `json:"developmentBuild"`
	NoPublishedRelease bool   `json:"noPublishedRelease"`
}

// Check reads the latest GitHub Release through gh and compares strict release
// versions. Release automation in this repository emits X.Y.Z versions only.
func Check(ctx context.Context, runner githubcli.Runner, installed string) (Result, error) {
	output, err := runner.Run(ctx, "api", "repos/"+repository+"/releases/latest", "--jq", ".tag_name")
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return Result{
				Installed:          normalise(installed),
				Development:        normalise(installed) == "dev",
				NoPublishedRelease: true,
			}, nil
		}
		return Result{}, fmt.Errorf("read latest projects release: %w", err)
	}
	latest := strings.TrimSpace(string(output))
	if latest == "" {
		return Result{}, fmt.Errorf("read latest projects release: GitHub returned an empty tag")
	}
	result := Result{Installed: normalise(installed), Latest: normalise(latest)}
	if result.Installed == "dev" || result.Installed == "unknown" || result.Installed == "" {
		result.Development = true
		return result, nil
	}

	installedVersion, err := parseVersion(result.Installed)
	if err != nil {
		return Result{}, fmt.Errorf("compare installed version: %w", err)
	}
	latestVersion, err := parseVersion(result.Latest)
	if err != nil {
		return Result{}, fmt.Errorf("compare latest release: %w", err)
	}
	result.UpdateAvailable = compare(installedVersion, latestVersion) < 0
	return result, nil
}

func normalise(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

type version [3]int

func parseVersion(value string) (version, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("%q is not an X.Y.Z release version", value)
	}
	var parsed version
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return version{}, fmt.Errorf("%q is not an X.Y.Z release version", value)
		}
		parsed[index] = number
	}
	return parsed, nil
}

func compare(left, right version) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
