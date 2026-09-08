package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRoot(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "skills", "github-projects", "tests", "fixtures", name)
}

func TestDispatcherRejectsCredentialLikeContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projectsDir := filepath.Join(root, ".projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	document := `# Dispatcher

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | octo-org/issues |
| Privacy | private repository |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |

GH_TOKEN=secret-value-that-must-not-be-accepted
`
	if err := os.WriteFile(filepath.Join(projectsDir, "project.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("error = %v, want credential rejection", err)
	}
}

func TestLoadMatchesShellFixtureValidity(t *testing.T) {
	t.Parallel()
	valid := []string{"single", "single-user", "dispatcher", "empty-dispatcher"}
	for _, name := range valid {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(fixtureRoot(t, name)); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}

	invalid := []string{
		"invalid",
		"invalid-colour",
		"invalid-dispatcher",
		"invalid-pending",
		"invalid-prose",
		"invalid-writeup",
	}
	for _, name := range invalid {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(fixtureRoot(t, name)); err == nil {
				t.Fatal("Load() error = nil, want invalid contract error")
			}
		})
	}
}

func TestResolveDispatcher(t *testing.T) {
	t.Parallel()
	configuration, err := Load(fixtureRoot(t, "dispatcher"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		selector Selector
		wantKey  string
	}{
		{name: "key", selector: Selector{Key: "alpha"}, wantKey: "alpha"},
		{name: "label", selector: Selector{RoutingLabel: "project:beta"}, wantKey: "beta"},
		{name: "number", selector: Selector{Number: 4}, wantKey: "alpha"},
		{
			name: "agreeing identifiers",
			selector: Selector{
				Key:          "beta",
				RoutingLabel: "project:beta",
				Number:       5,
			},
			wantKey: "beta",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			project, err := configuration.Resolve(test.selector)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if project.Key != test.wantKey {
				t.Fatalf("Resolve().Key = %q, want %q", project.Key, test.wantKey)
			}
		})
	}
}

func TestResolveDispatcherRejectsMissingOrDisagreeingSelectors(t *testing.T) {
	t.Parallel()
	configuration, err := Load(fixtureRoot(t, "dispatcher"))
	if err != nil {
		t.Fatal(err)
	}

	for _, selector := range []Selector{
		{},
		{Key: "alpha", Number: 5},
		{Key: "missing"},
	} {
		if _, err := configuration.Resolve(selector); err == nil {
			t.Fatalf("Resolve(%+v) error = nil", selector)
		}
	}
}

func TestSingleSelectorMustAgree(t *testing.T) {
	t.Parallel()
	configuration, err := Load(fixtureRoot(t, "single"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuration.Resolve(Selector{Number: 12}); err != nil {
		t.Fatalf("matching number error = %v", err)
	}
	if _, err := configuration.Resolve(Selector{Number: 99}); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("mismatching number error = %v", err)
	}
}

func TestProjectFieldLocationsAndHelpers(t *testing.T) {
	t.Parallel()
	configuration, err := Load(fixtureRoot(t, "single"))
	if err != nil {
		t.Fatal(err)
	}
	p := configuration.Project
	if p == nil {
		t.Fatal("Project is nil")
	}
	if loc, ok := p.FieldLocations["Priority"]; !ok || loc.Location != "organization issue field" || loc.Field != "Priority" {
		t.Fatalf("Priority field location = %+v", loc)
	}

	// Priority resolution
	if val, err := p.ResolvePriority("P0"); err != nil || val != "Urgent" {
		t.Fatalf("ResolvePriority(P0) = %q, err = %v", val, err)
	}
	if _, err := p.ResolvePriority("P99"); err == nil {
		t.Fatal("ResolvePriority(P99) error = nil")
	}

	// Class validation when empty allows anything
	if val, err := p.ValidateClass("task"); err != nil || val != "task" {
		t.Fatalf("ValidateClass(task) = %q, err = %v", val, err)
	}

	// Class validation with declared values
	p.ClassValues = []string{"Task", "Bug"}
	if val, err := p.ValidateClass("task"); err != nil || val != "Task" {
		t.Fatalf("ValidateClass(task) = %q, err = %v", val, err)
	}
	if _, err := p.ValidateClass("NonExistentClass"); err == nil {
		t.Fatal("ValidateClass(NonExistentClass) error = nil")
	}

	// Status mapping
	if val, err := p.ResolveStatus("Todo"); err != nil || val != "Todo" {
		t.Fatalf("ResolveStatus(Todo) = %q, err = %v, want Todo", val, err)
	}
	p.StatusValues = map[string]string{"Queued": "Backlog"}
	if val, err := p.ResolveStatus("backlog"); err != nil || val != "Backlog" {
		t.Fatalf("ResolveStatus(backlog) = %q, err = %v, want Backlog", val, err)
	}
	if _, err := p.ResolveStatus("Done"); err == nil {
		t.Fatal("ResolveStatus(Done) error = nil")
	}
}
