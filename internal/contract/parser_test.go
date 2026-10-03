package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const syntheticHeader = `# Synthetic Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | octo-org/example |
| Project owner | octo-org |
| Project number | 12 |
| Project title | Example planning |
| Routing | linked repository |
| Privacy | repository |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Priority | project field | Priority |
| Status | project field | Status |
`

func loadSynthetic(t *testing.T, document string) (*Configuration, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".projects", "project.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(root)
}

func mustLoadSynthetic(t *testing.T, document string) Project {
	t.Helper()
	configuration, err := loadSynthetic(t, document)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return *configuration.Project
}

func TestPendingPriorityWithoutTableIsHonoured(t *testing.T) {
	t.Parallel()
	p := mustLoadSynthetic(t, syntheticHeader+`
## Priority mapping

Priority mapping status: pending
`)
	if !p.Pending {
		t.Fatal("Pending = false, want true for a pending section without a table")
	}
	if _, err := p.ResolvePriority("P1"); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("ResolvePriority(P1) error = %v, want pending refusal", err)
	}
}

func TestEmptyPriorityMappingSectionIsRejected(t *testing.T) {
	t.Parallel()
	_, err := loadSynthetic(t, syntheticHeader+"\n## Priority mapping\n\nTo be decided.\n")
	if err == nil || !strings.Contains(err.Error(), "must map P0") {
		t.Fatalf("error = %v, want incomplete mapping error", err)
	}
}

func TestPendingMarkerOutsidePrioritySectionIsRejected(t *testing.T) {
	t.Parallel()
	_, err := loadSynthetic(t, syntheticHeader+"\n## Governance\n\nPriority mapping status: pending\n")
	if err == nil || !strings.Contains(err.Error(), "outside the Priority mapping section") {
		t.Fatalf("error = %v, want misplaced marker error", err)
	}
}

func TestFencedContentIsNotParsed(t *testing.T) {
	t.Parallel()
	for _, fence := range []string{"```", "~~~", "````"} {
		fence := fence
		t.Run(fence, func(t *testing.T) {
			t.Parallel()
			p := mustLoadSynthetic(t, syntheticHeader+`
## Governance

Example of an override (not live configuration):

`+fence+`markdown
## Priority mapping

Priority mapping status: pending

| Common value | Provider value |
| --- | --- |
| P0 | Urgent |
| P1 | High |
| P2 | Medium |
| P3 | Low |

## Status mapping

| Common value | Provider value |
| --- | --- |
| Queued | Backlog |
`+fence+`

- Collaboration mode: solo administration in a private repository.
`)
			if p.Pending || len(p.Priority) != 0 {
				t.Fatalf("Pending = %v, Priority = %v; fenced example leaked", p.Pending, p.Priority)
			}
			if got, err := p.ResolvePriority("P0"); err != nil || got != "P0" {
				t.Fatalf("ResolvePriority(P0) = %q, %v; want shared default", got, err)
			}
			if len(p.StatusValues) != 0 {
				t.Fatalf("StatusValues = %v; fenced example leaked", p.StatusValues)
			}
		})
	}
}

func TestFencedHeadingDoesNotChangeSection(t *testing.T) {
	t.Parallel()
	p := mustLoadSynthetic(t, syntheticHeader+`
## Status mapping

~~~text
## Notes
~~~

| Common value | Provider value |
| --- | --- |
| Queued | Backlog |
`)
	if p.StatusValues["Queued"] != "Backlog" {
		t.Fatalf("StatusValues = %v, want table recorded under Status mapping", p.StatusValues)
	}
}

func TestTableRowsWithoutOuterPipesFailExplicitly(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing trailing pipe": "# C\n\n| Key | Value |\n| --- | --- |\n| Contract version | 1 |\n| Mode | single\n",
		"missing both pipes":    "# C\n\n| Key | Value |\n| --- | --- |\n| Contract version | 1 |\nMode | single\n",
		"pipeless table":        "# C\n\nKey | Value\n--- | ---\nMode | single\n",
		"orphan row":            "# C\n\n| Mode | single |\n",
	}
	wantLine := map[string]string{
		"missing trailing pipe": "line 6",
		"missing both pipes":    "line 6",
		"pipeless table":        "line 3",
		"orphan row":            "line 3",
	}
	for name, document := range tests {
		name, document := name, document
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := loadSynthetic(t, document)
			if err == nil || !strings.Contains(err.Error(), "malformed table row at "+wantLine[name]) {
				t.Fatalf("error = %v, want malformed table row at %s", err, wantLine[name])
			}
		})
	}
}

func TestProseWithPipeIsNotATable(t *testing.T) {
	t.Parallel()
	mustLoadSynthetic(t, syntheticHeader+"\n## Governance\n\nUse `a | b` in prose.\n\n---\n")
}

func TestMultipleTablesInSectionAreParsedIndependently(t *testing.T) {
	t.Parallel()
	p := mustLoadSynthetic(t, syntheticHeader+`
## Class values

| Option | Colour |
| --- | --- |
| Task | YELLOW |
| Bug | RED |

| Option | Description |
| --- | --- |
| Task | Ordinary work |
| Bug | Defect |

## Status mapping

| Common value | Provider value |
| --- | --- |
| Queued | Backlog |
| Active | Doing |

| Option | Colour |
| --- | --- |
| Backlog | GRAY |
| Doing | BLUE |
`)
	if strings.Join(p.ClassValues, ",") != "Task,Bug" {
		t.Fatalf("ClassValues = %v, want [Task Bug]", p.ClassValues)
	}
	if len(p.StatusValues) != 2 || p.StatusValues["Queued"] != "Backlog" || p.StatusValues["Active"] != "Doing" {
		t.Fatalf("StatusValues = %v, want only the mapping table", p.StatusValues)
	}
}

func TestColourTableStillValidatedAfterAnotherTable(t *testing.T) {
	t.Parallel()
	_, err := loadSynthetic(t, syntheticHeader+`
## Class values

| Option | Description |
| --- | --- |
| Task | Ordinary work |

| Option | Colour |
| --- | --- |
| Task | TEAL |
`)
	if err == nil || !strings.Contains(err.Error(), "unsupported colour") {
		t.Fatalf("error = %v, want unsupported colour", err)
	}
}

func TestIdentityStatusMappingKeepsNormalisation(t *testing.T) {
	t.Parallel()
	p := mustLoadSynthetic(t, syntheticHeader+`
## Status mapping

| Common value | Provider value |
| --- | --- |
| Todo | Todo |
| In progress | In progress |
| Done | Done |
`)
	for input, want := range map[string]string{
		"to do":       "Todo",
		"in-progress": "In progress",
		"IN_PROGRESS": "In progress",
		"completed":   "Done",
		"Done":        "Done",
	} {
		if got, err := p.ResolveStatus(input); err != nil || got != want {
			t.Fatalf("ResolveStatus(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := p.ResolveStatus("Blocked"); err == nil {
		t.Fatal("ResolveStatus(Blocked) error = nil with a declared mapping")
	}
}

func TestStatusMappingMatchesCommonBeforeProvider(t *testing.T) {
	t.Parallel()
	p := Project{ContractPath: "synthetic", StatusValues: map[string]string{"Ready": "Todo", "Queued": "Ready-ish"}}
	for i := 0; i < 50; i++ {
		if got, err := p.ResolveStatus("ready"); err != nil || got != "Todo" {
			t.Fatalf("ResolveStatus(ready) = %q, %v; want common match Todo", got, err)
		}
		if got, err := p.ResolveStatus("ready ish"); err != nil || got != "Ready-ish" {
			t.Fatalf("ResolveStatus(ready ish) = %q, %v; want provider match", got, err)
		}
	}
}

func TestAmbiguousStatusMappingIsRejectedAtLoad(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"common equals another provider": "| Ready | Todo |\n| Todo | Doing |\n",
		"duplicate common":               "| In progress | Doing |\n| in-progress | Active |\n",
		"empty provider":                 "| Ready |  |\n",
	}
	for name, rows := range tests {
		name, rows := name, rows
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := loadSynthetic(t, syntheticHeader+"\n## Status mapping\n\n| Common value | Provider value |\n| --- | --- |\n"+rows)
			if err == nil || !strings.Contains(err.Error(), "Status mapping") {
				t.Fatalf("error = %v, want Status mapping rejection", err)
			}
		})
	}
}
