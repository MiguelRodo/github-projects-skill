package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), args, &stdout, &stderr, &runner{t: t})
	return exitCode, stdout.String(), stderr.String()
}

func TestVersionFlagsMatchVersionCommand(t *testing.T) {
	_, want, _ := run(t, "version")
	for _, flag := range []string{"--version", "-v"} {
		exitCode, stdout, stderr := run(t, flag)
		if exitCode != 0 || stdout != want || !strings.HasPrefix(stdout, "projects ") {
			t.Fatalf("%s: exit %d, stdout %q (want %q), stderr %q", flag, exitCode, stdout, want, stderr)
		}
	}
}

func TestGroupHelpListsSubcommands(t *testing.T) {
	wanted := map[string][]string{
		"issue":    {"issue create", "issue edit"},
		"project":  {"project item-list (items)", "project item-add", "project item-edit", "project setup-fields", "project setup-backlog-view"},
		"contract": {"contract validate"},
		"update":   {"update check"},
	}
	for group, commands := range wanted {
		for _, args := range [][]string{{group, "--help"}, {group, "-h"}, {"help", group}} {
			exitCode, stdout, stderr := run(t, args...)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("%v: exit %d, stderr %q", args, exitCode, stderr)
			}
			for _, command := range commands {
				if !strings.Contains(stdout, command) {
					t.Fatalf("%v: stdout %q missing %q", args, stdout, command)
				}
			}
		}
	}
}

func TestHelpTopicForSubcommandAndAlias(t *testing.T) {
	exitCode, stdout, _ := run(t, "help", "project", "items")
	if exitCode != 0 || !strings.Contains(stdout, "Usage: projects project item-list") {
		t.Fatalf("exit %d, stdout %q", exitCode, stdout)
	}
	exitCode, stdout, _ = run(t, "issue", "create", "--help")
	if exitCode != 0 || !strings.Contains(stdout, "-title") {
		t.Fatalf("exit %d, stdout %q", exitCode, stdout)
	}
}

func TestGlobalUsageListsVersionAndRootDiscovery(t *testing.T) {
	exitCode, stdout, _ := run(t, "--help")
	for _, wanted := range []string{"projects project setup-fields", "item-list (items)", "--version", ".projects/project.md"} {
		if exitCode != 0 || !strings.Contains(stdout, wanted) {
			t.Fatalf("exit %d, stdout missing %q: %s", exitCode, wanted, stdout)
		}
	}
}

func TestMissingOrUnknownGroupSubcommandShowsGroupUsage(t *testing.T) {
	for _, args := range [][]string{{"issue"}, {"issue", "delete"}} {
		exitCode, stdout, stderr := run(t, args...)
		if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "Usage: projects issue <subcommand>") ||
			strings.Contains(stderr, "update check") {
			t.Fatalf("%v: exit %d, stderr %q", args, exitCode, stderr)
		}
	}
}

func TestSubcommandUsageErrorShowsThatSubcommandUsage(t *testing.T) {
	exitCode, _, stderr := run(t, "issue", "create", "--root", fixture(t, "single"))
	if exitCode != 2 || !strings.Contains(stderr, "--title is required") ||
		!strings.Contains(stderr, "-title") || strings.Contains(stderr, "Commands:") {
		t.Fatalf("exit %d, stderr %q", exitCode, stderr)
	}
}

func TestSelectorMistakesAreUsageErrorsWithRoutes(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		message string
		routes  bool
	}{
		{"unknown key", []string{"project", "item-add", "--root", fixture(t, "dispatcher"), "--project-key", "missing", "--issue", "1"}, "does not match", true},
		{"missing selector", []string{"project", "item-edit", "--root", fixture(t, "dispatcher"), "--issue", "1", "--priority", "P1"}, "dispatcher resolution requires", true},
		{"selector on single", []string{"project", "setup-fields", "--root", fixture(t, "single"), "--project-key", "alpha"}, "only valid for a dispatcher", false},
		{"item-list unknown label", []string{"project", "items", "--root", fixture(t, "dispatcher"), "--routing-label", "project:nope"}, "does not match", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exitCode, _, stderr := run(t, append(test.args, "--quiet")...)
			if exitCode != 2 || !strings.Contains(stderr, test.message) {
				t.Fatalf("exit %d, stderr %q", exitCode, stderr)
			}
			if got := strings.Count(stderr, "configured routes:"); test.routes && got != 1 {
				t.Fatalf("configured routes listed %d times: %q", got, stderr)
			}
		})
	}
}

func copyFixture(t *testing.T, name, destination string) {
	t.Helper()
	source := fixture(t, name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(source, path)
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestContractRootIsDiscoveredFromSubdirectory(t *testing.T) {
	repository := t.TempDir()
	copyFixture(t, "single", repository)
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repository, "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	exitCode, stdout, stderr := run(t, "contract", "validate")
	if exitCode != 0 || !strings.Contains(stdout, "Valid single Project contract") ||
		!strings.Contains(stderr, repository) {
		t.Fatalf("exit %d, stdout %q, stderr %q", exitCode, stdout, stderr)
	}
}

func TestContractRootDiscoveryStopsAtRepositoryBoundary(t *testing.T) {
	workspace := t.TempDir()
	copyFixture(t, "single", workspace)
	child := filepath.Join(workspace, "child")
	if err := os.MkdirAll(filepath.Join(child, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	exitCode, _, stderr := run(t, "contract", "validate")
	if exitCode != 1 || !strings.Contains(stderr, "missing Project contract") ||
		!strings.Contains(stderr, "pass --root") {
		t.Fatalf("exit %d, stderr %q", exitCode, stderr)
	}
}

func TestExplicitRootIsNotDiscovered(t *testing.T) {
	repository := t.TempDir()
	copyFixture(t, "single", repository)
	nested := filepath.Join(repository, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	exitCode, _, stderr := run(t, "contract", "validate", "--root", nested)
	if exitCode != 1 || !strings.Contains(stderr, "missing Project contract") {
		t.Fatalf("exit %d, stderr %q", exitCode, stderr)
	}
}
