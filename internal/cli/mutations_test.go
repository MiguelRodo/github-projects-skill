package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

func cliProjectItemQueryArgs() []string {
	return []string{
		"api", "graphql",
		"-f", "query=" + githubcli.ProjectItemQuery("issues"),
		"-f", "owner=octo-org",
		"-f", "repo=example",
		"-F", "number=55",
	}
}

func cliProjectItemQueryJSON(itemID string) string {
	return fmt.Sprintf(`{"data":{"repository":{"target":{"url":"https://github.com/octo-org/example/issues/55","projectItems":{"nodes":[{"id":%q,"isArchived":false,"project":{"id":"PVT_12","number":12,"title":"Example planning","owner":{"login":"octo-org"}},"fieldValues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`, itemID)
}

func cliMissingProjectItemQueryJSON() string {
	return `{"data":{"repository":{"target":{"url":"https://github.com/octo-org/example/issues/55","projectItems":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`
}

func exactTitleScanArgs(repo, title string) []string {
	encodedTitle, err := json.Marshal(strings.TrimSpace(title))
	if err != nil {
		panic(err)
	}
	filter := fmt.Sprintf(
		`.[] | select((.pull_request == null) and (.title == %s)) | {number, title, state, url: .html_url}`,
		encodedTitle,
	)
	return []string{
		"api", "--paginate",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2026-03-10",
		fmt.Sprintf("repos/%s/issues?state=all&per_page=100", repo),
		"--jq", filter,
	}
}

func TestIssueCreatePlanDefault(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   exactTitleScanArgs("octo-org/example", "Sample Plan Issue"),
			output: ``,
		},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"issue", "create", "--root", fixture(t, "single"), "--title", "Sample Plan Issue", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "create_issue"`) ||
		!strings.Contains(stdout.String(), `"apply": false`) ||
		!strings.Contains(stdout.String(), `"title": "Sample Plan Issue"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[1/2] Inspecting") || !strings.Contains(stderr.String(), "[2/2] Planning issue creation") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestIssueCreateAllowDuplicateSkipsRepositoryScan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t}
	exitCode := Run(
		context.Background(),
		[]string{"issue", "create", "--root", fixture(t, "single"), "--title", "Intentional duplicate", "--allow-duplicate", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if fake.index != 0 {
		t.Fatalf("GitHub calls = %d, want 0 when duplicate inspection is bypassed", fake.index)
	}
	if strings.Contains(stdout.String(), `"exactTitleMatches"`) || !strings.Contains(stderr.String(), "Skipping duplicate inspection") {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
}

func TestIssueCreateApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   exactTitleScanArgs("octo-org/example", "Created Issue"),
			output: ``,
		},
		{
			args:   []string{"issue", "create", "--repo", "octo-org/example", "--title", "Created Issue", "--body", "Body text", "--label", "bug"},
			output: "https://github.com/octo-org/example/issues/55\n",
		},
		{
			args:   []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: `{"number":55,"title":"Created Issue","body":"Body text","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","labels":[{"name":"bug"}]}`,
		},
	}}

	exitCode := Run(
		context.Background(),
		[]string{"issue", "create", "--root", fixture(t, "single"), "--title", "Created Issue", "--body", "Body text", "--label", "bug", "--apply", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) ||
		!strings.Contains(stdout.String(), `"number": 55`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[2/3] Creating issue") ||
		!strings.Contains(stderr.String(), "[3/3] Verified created issue") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestIssueEditPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: `{"number":55,"title":"Original Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`,
		},
	}}

	exitCode := Run(
		context.Background(),
		[]string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "New Title", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "edit_issue"`) ||
		!strings.Contains(stdout.String(), `"apply": false`) ||
		!strings.Contains(stdout.String(), `"New Title"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestIssueEditApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: `{"number":55,"title":"Original Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`,
		},
		{
			args:   []string{"issue", "edit", "55", "--repo", "octo-org/example", "--title", "New Title"},
			output: "https://github.com/octo-org/example/issues/55\n",
		},
		{
			args:   []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: `{"number":55,"title":"New Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`,
		},
	}}

	exitCode := Run(
		context.Background(),
		[]string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "New Title", "--apply", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) ||
		!strings.Contains(stdout.String(), `"New Title"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestProjectItemAddPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   cliProjectItemQueryArgs(),
			output: cliMissingProjectItemQueryJSON(),
		},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"project", "item-add", "--root", fixture(t, "single"), "--issue", "55", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "project_item_add"`) ||
		!strings.Contains(stdout.String(), `"apply": false`) ||
		!strings.Contains(stdout.String(), "issues/55") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"contractPath"`) || strings.Contains(stdout.String(), `"fieldLocations"`) {
		t.Fatalf("plan includes unnecessary contract detail: %s", stdout.String())
	}
}

func TestProjectItemAddApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args:   cliProjectItemQueryArgs(),
			output: cliMissingProjectItemQueryJSON(),
		},
		{
			args:   []string{"project", "item-add", "12", "--owner", "octo-org", "--url", "https://github.com/octo-org/example/issues/55", "--format", "json"},
			output: `{"id": "PVTI_ITEM_55"}`,
		},
		{
			args:   cliProjectItemQueryArgs(),
			output: cliProjectItemQueryJSON("PVTI_ITEM_55"),
		},
	}}

	exitCode := Run(
		context.Background(),
		[]string{"project", "item-add", "--root", fixture(t, "single"), "--issue", "55", "--apply", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) ||
		!strings.Contains(stdout.String(), "PVTI_ITEM_55") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestProjectItemEditPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{
		{
			args: []string{
				"api", "--paginate", "--slurp",
				"-H", "Accept: application/vnd.github+json",
				"-H", "X-GitHub-Api-Version: 2026-03-10",
				"orgs/octo-org/issue-fields?per_page=100",
			},
			output: `[[{"id":1,"name":"Priority","data_type":"single_select","options":[{"id":2,"name":"High"}]}]]`,
		},
		{args: cliProjectItemQueryArgs(), output: cliProjectItemQueryJSON("PVTI_ITEM_55")},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"project", "item-edit", "--root", fixture(t, "single"), "--issue", "55", "--priority", "P1", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "project_item_edit"`) ||
		!strings.Contains(stdout.String(), `"apply": false`) ||
		!strings.Contains(stdout.String(), `"priority": "High"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"contractPath"`) || strings.Contains(stdout.String(), `"fieldLocations"`) {
		t.Fatalf("plan includes unnecessary contract detail: %s", stdout.String())
	}
}

func TestIssueCreateRejectsExactTitleDuplicate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{{
		args:   exactTitleScanArgs("octo-org/example", "Existing"),
		output: "{\"number\":55,\"title\":\"Existing\",\"state\":\"open\",\"url\":\"https://github.com/octo-org/example/issues/55\"}\n",
	}}}
	exitCode := Run(context.Background(), []string{
		"issue", "create", "--root", fixture(t, "single"), "--title", "Existing", "--apply",
	}, &stdout, &stderr, fake)
	if exitCode != 1 || !strings.Contains(stderr.String(), "exact-title issue already exists") {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestMutationCommandsRejectRepositoryDisagreementBeforeGitHub(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"issue", "edit", "--root", fixture(t, "single"), "--repo", "other/repo", "--issue", "55", "--title", "Title",
	}, &stdout, &stderr, &runner{t: t})
	if exitCode != 2 || !strings.Contains(stderr.String(), "disagrees with contract repository") {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestProjectMutationTargetsAreMutuallyExclusive(t *testing.T) {
	for _, command := range []string{"item-add", "item-edit"} {
		var stdout, stderr bytes.Buffer
		args := []string{"project", command, "--root", fixture(t, "single"), "--issue", "55", "--url", "https://github.com/octo-org/example/issues/55"}
		if command == "item-edit" {
			args = append(args, "--status", "Todo")
		}
		exitCode := Run(context.Background(), args, &stdout, &stderr, &runner{t: t})
		if exitCode != 2 || !strings.Contains(stderr.String(), "mutually exclusive") {
			t.Fatalf("%s exit code = %d, stderr = %s", command, exitCode, stderr.String())
		}
	}
}

func TestDispatcherIssueCreatePlanIncludesRoutingLabel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: []response{{
		args:   exactTitleScanArgs("octo-user/issues", "Routed"),
		output: ``,
	}}}
	exitCode := Run(context.Background(), []string{
		"issue", "create", "--root", fixture(t, "dispatcher"), "--project-key", "alpha", "--title", "Routed", "--json",
	}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"project:alpha"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
