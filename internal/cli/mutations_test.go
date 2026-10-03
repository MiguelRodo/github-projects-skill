package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		"-f", "projectOwner=octo-org",
		"-F", "projectNumber=12",
	}
}

const cliProjectOwnerJSON = `"projectOwner":{"__typename":"Organization","login":"octo-org","projectV2":{"id":"PVT_12","number":12,"title":"Example planning"}}`

func cliProjectItemQueryJSON(itemID string) string {
	return fmt.Sprintf(`{"data":{`+cliProjectOwnerJSON+`,"repository":{"target":{"id":"I_55","url":"https://github.com/octo-org/example/issues/55","projectItems":{"nodes":[{"id":%q,"isArchived":false,"project":{"id":"PVT_12","number":12,"title":"Example planning","owner":{"login":"octo-org"}},"fieldValues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}}}}}}`, itemID)
}

func cliMissingProjectItemQueryJSON() string {
	return `{"data":{` + cliProjectOwnerJSON + `,"repository":{"target":{"id":"I_55","url":"https://github.com/octo-org/example/issues/55","projectItems":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`
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

func exactTitleSearchArgs(repo, title string) []string {
	return []string{
		"api", "-X", "GET",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2026-03-10",
		"search/issues",
		"-f", fmt.Sprintf(`q=repo:%s is:issue in:title "%s"`, repo, strings.TrimSpace(title)),
		"-f", "per_page=100",
		"-f", "page=1",
		"--jq", `{total_count, incomplete_results, items: [.items[] | {number, title, state, url: .html_url, pull_request: (.pull_request != null)}]}`,
	}
}

// exactTitleTitlesPage returns the fake response for one titles-only page of
// the newest open issues in repo, requested after cursor ("" for the first page).
func exactTitleTitlesPage(repo, cursor string, hasNext bool, endCursor, nodes string) response {
	owner, name, _ := strings.Cut(repo, "/")
	args := []string{"api", "graphql", "-f", "query=" + exactTitleRecentQuery, "-f", "owner=" + owner, "-f", "name=" + name}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	return response{
		args:   args,
		output: fmt.Sprintf(`{"data":{"repository":{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}`, nodes, hasNext, endCursor),
	}
}

// exactTitleCheck returns the fake responses for the search-based duplicate
// check: one Search API page and one titles-only page of the newest issues.
func exactTitleCheck(repo, title, searchItems, titleNodes string) []response {
	count := strings.Count(searchItems, `"number"`)
	return []response{
		{
			args:   exactTitleSearchArgs(repo, title),
			output: fmt.Sprintf(`{"total_count":%d,"incomplete_results":false,"items":[%s]}`, count, searchItems),
		},
		exactTitleTitlesPage(repo, "", false, "", titleNodes),
	}
}

// exactTitleCappedFallback returns the fake responses for a failed search
// followed by the recent-open scan stopping at its 500-issue cap while still
// inside the window (the issues are dated far in the future).
func exactTitleCappedFallback(repo, title string) []response {
	responses := []response{{args: exactTitleSearchArgs(repo, title), err: errors.New("gh: API rate limit exceeded (HTTP 403)")}}
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
	fake := &runner{t: t, responses: exactTitleCheck("octo-org/example", "Sample Plan Issue", "", "")}
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
	fake := &runner{t: t, responses: exactTitleCappedFallback("octo-org/example", "Capped")}
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
	fake = &runner{t: t, responses: exactTitleCappedFallback("octo-org/example", "Capped")}
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
	if strings.Contains(stdout.String(), `"exactTitleMatches"`) || strings.Contains(stdout.String(), `"duplicateCheck"`) || !strings.Contains(stderr.String(), "Skipping duplicate inspection") {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
}

func TestIssueCreateApply(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &runner{t: t, responses: append(exactTitleCheck("octo-org/example", "Created Issue", "", ""), []response{
		{
			args:   []string{"issue", "create", "--repo", "octo-org/example", "--title", "Created Issue", "--body", "Body text", "--label", "bug"},
			output: "https://github.com/octo-org/example/issues/55\n",
		},
		{
			args:   []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: `{"number":55,"title":"Created Issue","body":"Body text","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","labels":[{"name":"bug"}]}`,
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
			args:   []string{"api", "graphql", "-f", "query=mutation($projectId: ID!, $contentId: ID!) {\n  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) { item { id } }\n}", "-f", "projectId=PVT_12", "-f", "contentId=I_55"},
			output: `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_ITEM_55"}}}}`,
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
	fake := &runner{t: t, responses: exactTitleCheck("octo-org/example", "Existing",
		`{"number":55,"title":"Existing","state":"open","url":"https://github.com/octo-org/example/issues/55","pull_request":false}`, "")}
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
	fake := &runner{t: t, responses: exactTitleCheck("octo-user/issues", "Routed", "", "")}
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
	fake := &runner{t: t, responses: []response{
		{args: cliProjectItemQueryArgs(), output: archived},
		{
			args:   []string{"api", "graphql", "-f", "query=mutation($projectId: ID!, $itemId: ID!) {\n  unarchiveProjectV2Item(input: {projectId: $projectId, itemId: $itemId}) { item { id } }\n}", "-f", "projectId=PVT_12", "-f", "itemId=PVTI_ITEM_55"},
			output: `{"data":{"unarchiveProjectV2Item":{"item":{"id":"PVTI_ITEM_55"}}}}`,
		},
		{args: cliProjectItemQueryArgs(), output: cliProjectItemQueryJSON("PVTI_ITEM_55")},
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
	fake := &runner{t: t, responses: append(exactTitleCheck("octo-org/example", "Mine", "", ""),
		response{args: []string{"api", "user", "--jq", ".login"}, output: "octocat\n"},
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
	view := []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", cliIssueViewJSONFields}
	fake := &runner{t: t, responses: []response{
		{args: []string{"api", "user", "--jq", ".login"}, output: "octocat\n"},
		{args: view, output: `{"number":55,"title":"T","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","assignees":[]}`},
		{args: []string{"issue", "edit", "55", "--repo", "octo-org/example", "--add-assignee", "octocat"}, output: "https://github.com/octo-org/example/issues/55\n"},
		{args: view, output: `{"number":55,"title":"T","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55","assignees":[{"login":"octocat"}]}`},
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
		t.Fatalf("gh calls = %d, want %d", fake.index, len(fake.responses))
	}
}

func TestIssueEditTrimsTitle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	view := []string{"issue", "view", "55", "--repo", "octo-org/example", "--json", cliIssueViewJSONFields}
	fake := &runner{t: t, responses: []response{
		{args: view, output: `{"number":55,"title":"Old","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`},
		{args: []string{"issue", "edit", "55", "--repo", "octo-org/example", "--title", "New Title"}, output: "https://github.com/octo-org/example/issues/55\n"},
		{args: view, output: `{"number":55,"title":"New Title","body":"","state":"OPEN","url":"https://github.com/octo-org/example/issues/55"}`},
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
		&runner{t: t},
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
			exitCode := Run(context.Background(), full, &stdout, &stderr, &runner{t: t})
			if exitCode != 2 || !strings.Contains(stderr.String(), "--project-number must be a positive integer") {
				t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
			}
		})
	}
}
