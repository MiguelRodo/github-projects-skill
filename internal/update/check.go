package update

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

const repository = "MiguelRodo/github-projects-skill"

// releaseHost pins the release lookup to github.com so a GH_HOST or enterprise
// default cannot turn a published release into a false "not found".
const releaseHost = "github.com"

// checkTimeout bounds the read-only release lookup.
const checkTimeout = 15 * time.Second

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
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	output, err := runner.Run(ctx, "api", "--hostname", releaseHost, "repos/"+repository+"/releases/latest", "--jq", ".tag_name")
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return Result{
				Installed:          normalise(installed),
				Development:        isDevelopment(normalise(installed)),
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
	if isDevelopment(result.Installed) {
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

// isDevelopment reports builds that cannot be compared with a release: the
// unstamped default, and Go pseudo-versions or locally modified builds such as
// v0.0.0-20260102030405-abcdef123456 or v1.2.3+dirty.
func isDevelopment(installed string) bool {
	return installed == "dev" || installed == "unknown" || installed == "" ||
		strings.ContainsAny(installed, "-+")
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
