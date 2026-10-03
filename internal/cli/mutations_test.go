package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

func cliProjectItemRequest() request {
	return request{query: githubcli.ProjectItemQuery("issues"), variables: map[string]any{"owner": "octo-org", "repo": "example", "number": 55, "projectOwner": "octo-org", "projectNumber": 12}}
}

const cliProjectOwnerJSON = `"projectOwner":{"__typename":"Organization","login":"octo-org","projectV2":{"id":"PVT_12","number":12,"title":"Example planning"}}`

func cliProjectItemQueryJSON(itemID string) string {
	return fmt.Sprintf(`{"data":{`+cliProjectOwnerJSON+`,"repository":{"target":{"id":"I_55","url":"https://github.com/octo-org/example/issues/55","state":"OPEN","projectItems":{"nodes":[{"id":%q,"isArchived":false,"project":{"id":"PVT_12","number":12,"title":"Example planning","owner":{"login":"octo-org"}},"fieldValues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`, itemID)
}

func cliMissingProjectItemQueryJSON() string {
	return `{"data":{` + cliProjectOwnerJSON + `,"repository":{"target":{"id":"I_55","url":"https://github.com/octo-org/example/issues/55","state":"OPEN","projectItems":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`
}

// exactTitleRecentQuery mirrors the titles-only GraphQL query used by the
// duplicate check.
const exactTitleRecentQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, after: $cursor, orderBy: {field: CREATED_AT, direction: DESC}, states: [OPEN]) {
      nodes { number title state url createdAt }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

func exactTitleSearchRequest(repo, title string) request {
	return request{method: "GET", path: "/" + "search/issues" + "?" + url.Values{"q": {fmt.Sprintf("repo:%s is:issue in:title \"%s\"", repo, strings.TrimSpace(title))}, "per_page": {"100"}, "page": {"1"}}.Encode()}
}

// exactTitleTitlesPage returns the fake response for one titles-only page of
// the newest open issues in repo, requested after cursor ("" for the first page).
func exactTitleTitlesPage(repo, cursor string, hasNext bool, endCursor, nodes string) response {
	owner, name, _ := strings.Cut(repo, "/")
	args := request{query: exactTitleRecentQuery, variables: map[string]any{"owner": owner, "name": name}}
	if cursor != "" {
		args.variables["cursor"] = cursor
	}
	return response{
		request: args,
		output:  fmt.Sprintf(`{"data":{"repository":{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}`, nodes, hasNext, endCursor),
		group:   1,
	}
}

// exactTitleCheck returns the fake responses for the search-based duplicate
// check: one Search API page and one titles-only page of the newest issues.
func exactTitleCheck(repo, title, searchItems, titleNodes string) []response {
	count := strings.Count(searchItems, `"number"`)
	return []response{
		{
			request: exactTitleSearchRequest(repo, title),
			output:  fmt.Sprintf(`{"total_count":%d,"incomplete_results":false,"items":[%s]}`, count, searchItems),
			group:   1,
		},
		exactTitleTitlesPage(repo, "", false, "", titleNodes),
	}
}

// exactTitleCappedFallback returns the fake responses for a failed search
// followed by the recent-open scan stopping at its 500-issue cap while still
// inside the window (the issues are dated far in the future).
func exactTitleCappedFallback(repo, title string) []response {
	responses := []response{{request: exactTitleSearchRequest(repo, title), err: errors.New("gh: API rate limit exceeded (HTTP 403)"), group: 1}}
	cursor := ""
	for page := 1; page <= 5; page++ {
		next := fmt.Sprintf("c%d", page)
		nodes := make([]string, 0, 100)
		for i := 0; i < 100; i++ {
			number := 1000 - (page-1)*100 - i
			nodes = append(nodes, fmt.Sprintf(`{"number":%d,"title":"Other","state":"OPEN","url":"https://github.com/%s/issues/%d","createdAt":"2999-01-01T00:00:00Z"}`, number, repo, number))
		}
		responses = append(responses, exactTitleTitlesPage(repo, cursor, true, next, strings.Join(nodes, ",")))
		cursor = next
	}
	return responses
}

func TestIssueCreatePlanDefault(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: exactTitleCheck("octo-org/example", "Sample Plan Issue", "", "")}
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
		!strings.Contains(stdout.String(), `"applied": false`) ||
		!strings.Contains(stdout.String(), `"title": "Sample Plan Issue"`) ||
		!strings.Contains(stdout.String(), `"method": "search+recent"`) ||
		!strings.Contains(stdout.String(), `"complete": true`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[1/2] Inspecting") || !strings.Contains(stderr.String(), "[2/2] Planning issue creation") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestIssueCreateReportsCappedDuplicateCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: exactTitleCappedFallback("octo-org/example", "Capped")}
	exitCode := Run(context.Background(), []string{
		"issue", "create", "--root", fixture(t, "single"), "--title", "Capped", "--json",
	}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var plan struct {
		DuplicateCheck struct {
			Method       string `json:"method"`
			RecentWindow string `json:"recentWindow"`
			Complete     *bool  `json:"complete"`
			Unchecked    string `json:"unchecked"`
		} `json:"duplicateCheck"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, stdout.String())
	}
	if got := plan.DuplicateCheck; got.Method != "recent-open" || got.RecentWindow != "7d" || got.Complete == nil || *got.Complete || !strings.Contains(got.Unchecked, "closed issues") {
		t.Fatalf("duplicateCheck = %+v, stdout = %s", got, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	fake = &fakeClient{t: t, responses: exactTitleCappedFallback("octo-org/example", "Capped")}
	exitCode = Run(context.Background(), []string{
		"issue", "create", "--root", fixture(t, "single"), "--title", "Capped",
	}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "covered only recent open issues; closed issues and open issues beyond the newest 500 created in the last 7d were not checked") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestIssueCreateAllowDuplicateSkipsRepositoryScan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t}
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
	if strings.Contains(stdout.String(), `"exactTitleMatches"`) || strings.Contains(stdout.String(), `"duplicateCheck"`) || !strings.Contains(stderr.String(), "Skipping duplicate inspection") {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
}

func TestIssueCreateApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: append(exactTitleCheck("octo-org/example", "Created Issue", "", ""), []response{
		{request: request{method: "GET", path: "/repos/octo-org/example/labels/" + url.PathEscape("bug")}, output: "{}"},
		{request: request{method: "POST", path: "/repos/octo-org/example/issues", body: map[string]any{"title": "Created Issue", "body": "Body text", "labels": []string{"bug"}}}, output: string(objectFixture("html_url", []byte("https://github.com/octo-org/example/issues/55\n")))},
		{
			request: request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}},
			output:  string(issueFixture([]byte(`{"number":55,"title":"Created Issue","body":"Body text","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","labels":[{"name":"bug"}]}`))),
		},
	}...)}

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
		!strings.Contains(stdout.String(), `"changed": true`) ||
		!strings.Contains(stdout.String(), `"number": 55`) ||
		!strings.Contains(stdout.String(), `"duplicateCheck"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[2/3] Creating issue") ||
		!strings.Contains(stderr.String(), "[3/3] Verified created issue") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestIssueEditPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}},
			output:  string(issueFixture([]byte(`{"number":55,"title":"Original Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`))),
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
		!strings.Contains(stdout.String(), `"applied": false`) ||
		!strings.Contains(stdout.String(), `"New Title"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestIssueEditApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}},
			output:  string(issueFixture([]byte(`{"number":55,"title":"Original Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`))),
		},
		{request: request{method: "PATCH", path: "/repos/octo-org/example/issues/55", body: map[string]any{"title": "New Title"}}, output: "https://github.com/octo-org/example/issues/55\n"},
		{
			request: request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}},
			output:  string(issueFixture([]byte(`{"number":55,"title":"New Title","body":"Original Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`))),
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
		!strings.Contains(stdout.String(), `"changed": true`) ||
		!strings.Contains(stdout.String(), `"New Title"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestProjectItemAddPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: cliProjectItemRequest(),
			output:  cliMissingProjectItemQueryJSON(),
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
		!strings.Contains(stdout.String(), `"applied": false`) ||
		!strings.Contains(stdout.String(), `"changed": true`) ||
		!strings.Contains(stdout.String(), "issues/55") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"contractPath"`) || strings.Contains(stdout.String(), `"fieldLocations"`) {
		t.Fatalf("plan includes unnecessary contract detail: %s", stdout.String())
	}
}

func TestProjectItemAddApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: cliProjectItemRequest(),
			output:  cliMissingProjectItemQueryJSON(),
		},
		{
			request: request{query: "mutation($projectId: ID!, $contentId: ID!) {\n  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) { item { id } }\n}", variables: map[string]any{"projectId": "PVT_12", "contentId": "I_55"}},
			output:  `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_ITEM_55"}}}}`,
		},
		{
			request: cliProjectItemRequest(),
			output:  cliProjectItemQueryJSON("PVTI_ITEM_55"),
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
		!strings.Contains(stdout.String(), `"changed": true`) ||
		!strings.Contains(stdout.String(), "PVTI_ITEM_55") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestProjectItemEditPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{method: "GET", path: "/" + "orgs/octo-org/issue-fields?per_page=100"},
			output:  `[{"id":1,"name":"Priority","data_type":"single_select","options":[{"id":2,"name":"High"}]}]`,
		},
		{request: cliProjectItemRequest(), output: cliProjectItemQueryJSON("PVTI_ITEM_55")},
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
		!strings.Contains(stdout.String(), `"applied": false`) ||
		!strings.Contains(stdout.String(), `"priority": "High"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"contractPath"`) || strings.Contains(stdout.String(), `"fieldLocations"`) {
		t.Fatalf("plan includes unnecessary contract detail: %s", stdout.String())
	}
}

func TestIssueCreateRejectsExactTitleDuplicate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: exactTitleCheck("octo-org/example", "Existing",
		`{"number":55,"title":"Existing","state":"open","html_url":"https://github.com/octo-org/example/issues/55","pull_request":null}`, "")}
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
	}, &stdout, &stderr, &fakeClient{t: t})
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
		exitCode := Run(context.Background(), args, &stdout, &stderr, &fakeClient{t: t})
		if exitCode != 2 || !strings.Contains(stderr.String(), "mutually exclusive") {
			t.Fatalf("%s exit code = %d, stderr = %s", command, exitCode, stderr.String())
		}
	}
}

func TestDispatcherIssueCreatePlanIncludesRoutingLabel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: exactTitleCheck("octo-user/issues", "Routed", "", "")}
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

func TestProjectItemAddApplyUnarchivesArchivedMember(t *testing.T) {
	var stdout, stderr bytes.Buffer
	archived := strings.Replace(cliProjectItemQueryJSON("PVTI_ITEM_55"), `"isArchived":false`, `"isArchived":true`, 1)
	fake := &fakeClient{t: t, responses: []response{
		{request: cliProjectItemRequest(), output: archived},
		{
			request: request{query: "mutation($projectId: ID!, $itemId: ID!) {\n  unarchiveProjectV2Item(input: {projectId: $projectId, itemId: $itemId}) { item { id } }\n}", variables: map[string]any{"projectId": "PVT_12", "itemId": "PVTI_ITEM_55"}},
			output:  `{"data":{"unarchiveProjectV2Item":{"item":{"id":"PVTI_ITEM_55"}}}}`,
		},
		{request: cliProjectItemRequest(), output: cliProjectItemQueryJSON("PVTI_ITEM_55")},
	}}
	exitCode := Run(context.Background(), []string{"project", "item-add", "--root", fixture(t, "single"), "--issue", "55", "--apply", "--json"}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"unarchived": true`) || !strings.Contains(stdout.String(), `"applied": true`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

const cliIssueViewJSONFields = "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"

func TestIssueCreatePlanResolvesSelfAssignee(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: append(exactTitleCheck("octo-org/example", "Mine", "", ""),
		response{request: request{method: "GET", path: "/" + "user"}, output: string(objectFixture("login", []byte("octocat\n"))), group: 1},
	)}
	exitCode := Run(
		context.Background(),
		[]string{"issue", "create", "--root", fixture(t, "single"), "--title", "Mine", "--assignee", "@me", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"octocat"`) || strings.Contains(stdout.String(), "@me") {
		t.Fatalf("stdout = %s, want @me resolved to octocat", stdout.String())
	}
}

func TestIssueEditApplyResolvesSelfAssignee(t *testing.T) {
	var stdout, stderr bytes.Buffer
	view := request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}}
	fake := &fakeClient{t: t, responses: []response{
		{request: request{method: "GET", path: "/" + "user"}, output: string(objectFixture("login", []byte("octocat\n")))},
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"T","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","assignees":[]}`)))},
		{request: request{method: "POST", path: "/repos/octo-org/example/issues/55/assignees", body: map[string]any{"assignees": []string{"octocat"}}}, output: "https://github.com/octo-org/example/issues/55\n"},
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"T","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","assignees":[{"login":"octocat"}]}`)))},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--add-assignee", "@me", "--apply", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if fake.index != len(fake.responses) {
		t.Fatalf("GitHub requests = %d, want %d", fake.index, len(fake.responses))
	}
}

func TestIssueEditTrimsTitle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	view := request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}}
	fake := &fakeClient{t: t, responses: []response{
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"Old","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`)))},
		{request: request{method: "PATCH", path: "/repos/octo-org/example/issues/55", body: map[string]any{"title": "New Title"}}, output: "https://github.com/octo-org/example/issues/55\n"},
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"New Title","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`)))},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "  New Title  ", "--apply", "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestIssueEditRejectsBlankTitle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(
		context.Background(),
		[]string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "   "},
		&stdout,
		&stderr,
		&fakeClient{t: t},
	)
	if exitCode != 2 || !strings.Contains(stderr.String(), "--title must not be empty") {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
}

func TestMutationsRejectNegativeProjectNumber(t *testing.T) {
	for _, args := range [][]string{
		{"issue", "create", "--title", "T", "--project-number", "-1"},
		{"project", "item-add", "--issue", "55", "--project-number", "-1"},
		{"project", "item-edit", "--issue", "55", "--status", "Todo", "--project-number", "-1"},
	} {
		t.Run(args[1], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			full := append(append([]string{}, args...), "--root", fixture(t, "single"))
			exitCode := Run(context.Background(), full, &stdout, &stderr, &fakeClient{t: t})
			if exitCode != 2 || !strings.Contains(stderr.String(), "--project-number must be a positive integer") {
				t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
			}
		})
	}
}

func localFieldsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyFixture(t, "single", root)
	path := filepath.Join(root, ".projects", "project.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.ReplaceAll(string(data), "organization issue field | Priority", "project field | Priority")
	content = strings.ReplaceAll(content, "organization issue type | Issue Type", "project field | Class")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
func localFieldsSchema() string {
	fields := []map[string]any{}
	for _, field := range []struct {
		id, name string
		options  []string
	}{{"priority", "Priority", []string{"High", "Medium"}}, {"class", "Class", []string{"Bug", "Task"}}, {"status", "Status", []string{"Done", "Todo"}}} {
		options := []map[string]string{}
		for _, name := range field.options {
			options = append(options, map[string]string{"id": "option-" + name, "name": name})
		}
		fields = append(fields, map[string]any{"__typename": "ProjectV2SingleSelectField", "id": field.id, "name": field.name, "dataType": "SINGLE_SELECT", "options": options})
	}
	output, _ := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"__typename": "Organization", "login": "octo-org", "projectV2": map[string]any{"id": "PVT_12", "number": 12, "title": "Example planning", "fields": map[string]any{"nodes": fields}}}}})
	return string(output)
}
func localFieldsItem(priority, class, status string) string {
	fields := []map[string]any{}
	for _, field := range []struct{ id, name, value string }{{"priority", "Priority", priority}, {"class", "Class", class}, {"status", "Status", status}} {
		fields = append(fields, map[string]any{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": field.value, "optionId": "option-" + field.value, "field": map[string]string{"id": field.id, "name": field.name, "dataType": "SINGLE_SELECT"}})
	}
	encoded, _ := json.Marshal(fields)
	return strings.Replace(cliProjectItemQueryJSON("PVTI_ITEM_55"), `"nodes":[]`, `"nodes":`+string(encoded), 1)
}
func localFieldsWriteRequest(names ...string) request {
	declarations := []string{"$projectId: ID!", "$itemId: ID!"}
	selections := []string{}
	variables := map[string]any{"projectId": "PVT_12", "itemId": "PVTI_ITEM_55"}
	for i, name := range names {
		alias := fmt.Sprintf("f%d", i)
		declarations = append(declarations, "$"+alias+"Field: ID!", "$"+alias+"Value: ProjectV2FieldValue!")
		selections = append(selections, fmt.Sprintf("  %s: updateProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $%sField, value: $%sValue}) { projectV2Item { id } }", alias, alias, alias))
		field := map[string]string{"High": "priority", "Bug": "class", "Done": "status"}[name]
		variables[alias+"Field"] = field
		variables[alias+"Value"] = map[string]string{"singleSelectOptionId": "option-" + name}
	}
	return request{query: "mutation(" + strings.Join(declarations, ", ") + ") {\n" + strings.Join(selections, "\n") + "\n}", variables: variables}
}
func localFieldsWriteOutput(count int) string {
	data := map[string]any{}
	for i := 0; i < count; i++ {
		data[fmt.Sprintf("f%d", i)] = map[string]any{"projectV2Item": map[string]string{"id": "PVTI_ITEM_55"}}
	}
	encoded, _ := json.Marshal(map[string]any{"data": data})
	return string(encoded)
}

func TestIssueCreateWithThreeProjectFieldsUsesTenRequests(t *testing.T) {
	root := localFieldsFixture(t)
	steps := exactTitleCheck("octo-org/example", "Created Issue", "", "")
	steps = append(steps,
		response{request: request{query: githubcli.ProjectSchemaQuery(), variables: map[string]any{"login": "octo-org", "number": 12}}, output: localFieldsSchema(), group: 1},
		response{request: request{method: "POST", path: "/repos/octo-org/example/issues", body: map[string]any{"title": "Created Issue", "body": ""}}, output: `{"html_url":"https://github.com/octo-org/example/issues/55"}`},
		response{request: request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}}, output: string(issueFixture([]byte(`{"number":55,"title":"Created Issue","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`)))},
		response{request: cliProjectItemRequest(), output: cliMissingProjectItemQueryJSON()},
		response{request: request{query: "mutation($projectId: ID!, $contentId: ID!) {\n  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) { item { id } }\n}", variables: map[string]any{"projectId": "PVT_12", "contentId": "I_55"}}, output: `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_ITEM_55"}}}}`},
		response{request: cliProjectItemRequest(), output: localFieldsItem("Medium", "Task", "Todo")},
		response{request: localFieldsWriteRequest("High", "Bug", "Done"), output: localFieldsWriteOutput(3)},
		response{request: cliProjectItemRequest(), output: localFieldsItem("High", "Bug", "Done")},
	)
	client := &fakeClient{t: t, responses: steps}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"issue", "create", "--root", root, "--title", "Created Issue", "--priority", "P1", "--class", "Bug", "--status", "Done", "--apply", "--json"}, &stdout, &stderr, client)
	if code != 0 || client.index != 10 {
		t.Fatalf("exit=%d requests=%d stderr=%s", code, client.index, stderr.String())
	}
}

// projectFieldsPlanSteps returns the plan-mode request sequence for an
// issue create that requests Project fields against the single fixture: the
// duplicate check followed by the Project schema preflight.
func projectFieldsPlanSteps(title string) []response {
	steps := exactTitleCheck("octo-org/example", title, "", "")
	return append(steps, response{request: request{query: githubcli.ProjectSchemaQuery(), variables: map[string]any{"login": "octo-org", "number": 12}}, output: localFieldsSchema(), group: 1})
}

func TestIssueCreatePlanValidatesProjectFields(t *testing.T) {
	client := &fakeClient{t: t, responses: projectFieldsPlanSteps("Planned Issue")}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"issue", "create", "--root", fixture(t, "single"), "--title", "Planned Issue", "--status", "Done", "--json"}, &stdout, &stderr, client)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if client.usedCount() != 3 {
		t.Fatalf("GitHub requests = %d, want 3 (no mutating request)", client.usedCount())
	}
	if !strings.Contains(stderr.String(), "[1/2] Inspecting duplicates and Project configuration") || !strings.Contains(stderr.String(), "[2/2] Planning issue creation") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "Done"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestIssueCreatePlanRejectsUnknownProjectStatusOption(t *testing.T) {
	client := &fakeClient{t: t, responses: projectFieldsPlanSteps("In review issue")}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"issue", "create", "--root", fixture(t, "single"), "--title", "In review issue", "--status", "In review", "--json"}, &stdout, &stderr, client)
	if code == 0 {
		t.Fatalf("exit=%d, want non-zero; stdout=%s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), `option "In review" for Status is absent from Project field "Status"`) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if client.usedCount() != 3 {
		t.Fatalf("GitHub requests = %d, want 3 (no mutating request)", client.usedCount())
	}
}

// concurrentReadsClient blocks the duplicate-check search until the Project
// preflight has been requested; a sequential implementation would time out.
type concurrentReadsClient struct {
	*fakeClient
	preflightOnce    sync.Once
	preflightArrived chan struct{}
}

func (c *concurrentReadsClient) GraphQL(ctx context.Context, query string, variables map[string]any) (githubcli.GraphQLResponse, error) {
	if query == githubcli.ProjectSchemaQuery() {
		c.preflightOnce.Do(func() { close(c.preflightArrived) })
	}
	return c.fakeClient.GraphQL(ctx, query, variables)
}

func (c *concurrentReadsClient) REST(ctx context.Context, method, path string, body any) (githubcli.RESTResponse, error) {
	if method == "GET" && strings.HasPrefix(path, "/search/issues") {
		select {
		case <-c.preflightArrived:
		case <-time.After(5 * time.Second):
			c.fakeClient.t.Errorf("duplicate-check search did not run concurrently with the Project preflight")
		}
	}
	return c.fakeClient.REST(ctx, method, path, body)
}

func TestIssueCreateReadsRunConcurrently(t *testing.T) {
	client := &concurrentReadsClient{
		fakeClient:       &fakeClient{t: t, responses: projectFieldsPlanSteps("Concurrent Issue")},
		preflightArrived: make(chan struct{}),
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"issue", "create", "--root", fixture(t, "single"), "--title", "Concurrent Issue", "--status", "Done", "--json"}, &stdout, &stderr, client)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if client.usedCount() != 3 {
		t.Fatalf("GitHub requests = %d, want 3", client.usedCount())
	}
}
func TestIssueEditAddLabelUsesFourRequests(t *testing.T) {
	view := request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}}
	client := &fakeClient{t: t, responses: []response{
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"T","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`)))},
		{request: request{method: "GET", path: "/repos/octo-org/example/labels/x"}, output: `{}`},
		{request: request{method: "POST", path: "/repos/octo-org/example/issues/55/labels", body: map[string]any{"labels": []string{"x"}}}, output: `[]`},
		{request: view, output: string(issueFixture([]byte(`{"number":55,"title":"T","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","labels":[{"name":"x"}]}`)))},
	}}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--add-label", "x", "--apply"}, &stdout, &stderr, client)
	if code != 0 || client.index != 4 {
		t.Fatalf("exit=%d requests=%d stderr=%s", code, client.index, stderr.String())
	}
}
func TestProjectItemEditPriorityAndStatusUsesFourRequests(t *testing.T) {
	client := &fakeClient{t: t, responses: []response{
		{request: request{query: githubcli.ProjectSchemaQuery(), variables: map[string]any{"login": "octo-org", "number": 12}}, output: localFieldsSchema()},
		{request: cliProjectItemRequest(), output: localFieldsItem("Medium", "Task", "Todo")},
		{request: localFieldsWriteRequest("High", "Done"), output: localFieldsWriteOutput(2)},
		{request: cliProjectItemRequest(), output: localFieldsItem("High", "Task", "Done")},
	}}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"project", "item-edit", "--root", localFieldsFixture(t), "--issue", "55", "--priority", "P1", "--status", "Done", "--apply"}, &stdout, &stderr, client)
	if code != 0 || client.index != 4 {
		t.Fatalf("exit=%d requests=%d stderr=%s", code, client.index, stderr.String())
	}
}

func TestIssueEditApplyNoOpReportsNoChange(t *testing.T) {
	view := request{query: githubcli.IssueViewQuery, variables: map[string]any{"owner": "octo-org", "name": "example", "number": 55}}
	body := string(issueFixture([]byte(`{"number":55,"title":"Same Title","body":"Body","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`)))

	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{{request: view, output: body}}}
	exitCode := Run(context.Background(), []string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "Same Title", "--apply", "--json"}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) || !strings.Contains(stdout.String(), `"changed": false`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if fake.index != 1 {
		t.Fatalf("GitHub requests = %d, want 1 (no write on a no-op)", fake.index)
	}

	var textOut, textErr bytes.Buffer
	fake = &fakeClient{t: t, responses: []response{{request: view, output: body}}}
	exitCode = Run(context.Background(), []string{"issue", "edit", "--root", fixture(t, "single"), "--issue", "55", "--title", "Same Title", "--apply"}, &textOut, &textErr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, textErr.String())
	}
	if !strings.Contains(textOut.String(), "No change needed for issue octo-org/example#55: https://github.com/octo-org/example/issues/55") {
		t.Fatalf("stdout = %s", textOut.String())
	}
	if strings.Contains(textOut.String(), "Updated issue") {
		t.Fatalf("stdout = %s, want no 'Updated issue'", textOut.String())
	}
}

func TestProjectItemAddApplyAlreadyMember(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{{request: cliProjectItemRequest(), output: cliProjectItemQueryJSON("PVTI_ITEM_55")}}}
	exitCode := Run(context.Background(), []string{"project", "item-add", "--root", fixture(t, "single"), "--issue", "55", "--apply", "--json"}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"applied": true`) ||
		!strings.Contains(stdout.String(), `"changed": false`) ||
		!strings.Contains(stdout.String(), `"alreadyMember": true`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestProjectItemEditApplyNoOpReportsNoChange(t *testing.T) {
	responses := []response{
		{request: request{query: githubcli.ProjectSchemaQuery(), variables: map[string]any{"login": "octo-org", "number": 12}}, output: localFieldsSchema()},
		{request: cliProjectItemRequest(), output: localFieldsItem("High", "Task", "Done")},
	}
	args := []string{"project", "item-edit", "--root", localFieldsFixture(t), "--issue", "55", "--priority", "P1", "--status", "Done", "--apply", "--json"}

	var stdout, stderr bytes.Buffer
	client := &fakeClient{t: t, responses: responses}
	code := Run(context.Background(), args, &stdout, &stderr, client)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if client.index != 2 {
		t.Fatalf("GitHub requests = %d, want 2 (no write on a no-op); stderr=%s", client.index, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"action": "project_item_edit"`) ||
		!strings.Contains(stdout.String(), `"applied": true`) ||
		!strings.Contains(stdout.String(), `"changed": false`) ||
		!strings.Contains(stdout.String(), `"projectItem"`) ||
		!strings.Contains(stdout.String(), `"itemId": "PVTI_ITEM_55"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}

	var textOut, textErr bytes.Buffer
	client = &fakeClient{t: t, responses: responses}
	textArgs := []string{"project", "item-edit", "--root", localFieldsFixture(t), "--issue", "55", "--priority", "P1", "--status", "Done", "--apply"}
	code = Run(context.Background(), textArgs, &textOut, &textErr, client)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, textErr.String())
	}
	if !strings.Contains(textOut.String(), "No change needed for Project item PVTI_ITEM_55 on Project octo-org/12:") {
		t.Fatalf("stdout = %s", textOut.String())
	}
}
