package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

func TestMembershipDispatcherIssueEditPlanApplyRetry(t *testing.T) {
	root := fixture(t, "membership-dispatcher")
	view := request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-user", "name": "issues", "number": 55}}
	issue := func(title, body string) string {
		return string(issueFixture([]byte(`{"number":55,"title":"` + title + `","body":"` + body + `","state":"OPEN","url":"https://github.com/octo-user/issues/issues/55","labels":[{"name":"project:beta"},{"name":"subproject:legacy"},{"name":"unrelated"}],"projectItems":[{"title":"Alpha","status":{"name":"Todo"}},{"title":"Beta","status":{"name":"In progress"}}]}`)))
	}
	before, after := issue("Before", "Old body"), issue("After", "New body")
	fake := &fakeClient{t: t, responses: []response{
		{request: view, output: before},
		{request: view, output: before},
		{request: request{method: "PATCH", path: "/repos/octo-user/issues/issues/55", body: map[string]any{"title": "After", "body": "New body"}}, output: "{}"},
		{request: view, output: after},
		{request: view, output: after},
	}}
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("New body"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []struct {
		name    string
		apply   bool
		changed string
		calls   int
	}{
		{"plan", false, `"applied": false`, 1},
		{"apply", true, `"changed": true`, 4},
		{"retry", true, `"changed": false`, 5},
	} {
		t.Run(phase.name, func(t *testing.T) {
			args := []string{"issue", "edit", "--root", root, "--issue", "55", "--title", "After", "--body-file", bodyFile, "--json"}
			if phase.apply {
				args = append(args, "--apply")
			}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr, fake); code != 0 {
				t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), phase.changed) || fake.usedCount() != phase.calls {
				t.Fatalf("stdout = %s, requests = %d", stdout.String(), fake.usedCount())
			}
		})
	}
}

func TestDispatcherValidationRejectsBrokenUnrelatedRoutesBeforeGitHub(t *testing.T) {
	for _, test := range []struct {
		name, file, before, after, want string
	}{
		{"truncated", "project.md", "| beta | - | 5 | .projects/projects/beta.md |", "| beta | - | 5 |", "incomplete route"},
		{"conflicting routing", "projects/beta.md", "| Routing | membership |", "| Routing | label:project:beta |", "route label disagrees"},
		{"invalid binding", "projects/beta.md", "| Mode | project |", "| Sub-project storage | field |\n| Sub-project field type | TEXT |\n| Mode | project |", "SINGLE_SELECT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			copyFixture(t, "membership-dispatcher", root)
			path := filepath.Join(root, ".projects", filepath.FromSlash(test.file))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(data), test.before, test.after, 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"contract", "validate"},
				{"issue", "edit", "--issue", "55", "--title", "After", "--apply"},
				{"project", "item-edit", "--project-key", "alpha", "--issue", "55", "--priority", "P1", "--apply"},
			} {
				var stdout, stderr bytes.Buffer
				fake := &fakeClient{t: t}
				code := Run(context.Background(), append(args, "--root", root), &stdout, &stderr, fake)
				if code != 1 || !strings.Contains(stderr.String(), test.want) || fake.usedCount() != 0 {
					t.Fatalf("%v: exit = %d, stderr = %s, requests = %d", args, code, stderr.String(), fake.usedCount())
				}
			}
			validator := filepath.Join("..", "..", "skills", "github-projects", "scripts", "validate-contract.sh")
			output, err := exec.Command("bash", validator, root).CombinedOutput()
			if err == nil || !strings.Contains(string(output), test.want) {
				t.Fatalf("shell validation: error = %v, output = %s", err, output)
			}
		})
	}
}
