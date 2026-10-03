package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCreateIssue(t *testing.T) {
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Test Title", "--body", "Test Body", "--label", "bug", "--assignee", "monalisa"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Test Title","body":"Test Body","state":"OPEN","url":"https://github.com/owner/repo/issues/42","labels":[{"name":"bug"}],"assignees":[{"login":"monalisa"}]}`),
		},
	}}

	view, err := CreateIssue(context.Background(), fake, CreateIssueInput{
		Repo:      "owner/repo",
		Title:     "  Test Title  ",
		Body:      "Test Body",
		Labels:    []string{"bug"},
		Assignees: []string{"monalisa"},
	})
	if err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
	if view.Number != 42 || view.Title != "Test Title" || view.State != "OPEN" {
		t.Fatalf("view = %+v", view)
	}
	if len(view.Labels) != 1 || view.Labels[0].Name != "bug" {
		t.Fatalf("labels = %+v", view.Labels)
	}
}

func TestEditIssue(t *testing.T) {
	newTitle := "Updated Title"
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			// Initial view before edit
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Old Title","body":"Old Body","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`),
		},
		{
			// gh issue edit
			args:   []string{"issue", "edit", "42", "--repo", "owner/repo", "--title", "Updated Title", "--add-label", "enhancement"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			// gh issue close
			args:   []string{"issue", "close", "42", "--repo", "owner/repo", "--reason", "completed"},
			output: []byte("Closed issue #42\n"),
		},
		{
			// View after edit for readback
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Updated Title","body":"Old Body","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42","labels":[{"name":"enhancement"}]}`),
		},
	}}

	view, err := EditIssue(context.Background(), fake, EditIssueInput{
		Repo:        "owner/repo",
		Number:      42,
		Title:       &newTitle,
		AddLabels:   []string{"enhancement"},
		State:       &state,
		CloseReason: "completed",
	})
	if err != nil {
		t.Fatalf("EditIssue error = %v", err)
	}
	if view.Title != "Updated Title" || view.State != "CLOSED" {
		t.Fatalf("view = %+v", view)
	}
}

func TestCreateIssueRejectsEmptyTitle(t *testing.T) {
	fake := &fakeRunner{t: t}
	_, err := CreateIssue(context.Background(), fake, CreateIssueInput{
		Repo:  "owner/repo",
		Title: "   ",
	})
	if err == nil || !strings.Contains(err.Error(), "non-empty title") {
		t.Fatalf("error = %v, want non-empty title", err)
	}
}

func TestCreateIssueRejectsIncompleteReadback(t *testing.T) {
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Title", "--body", "", "--label", "bug"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","labels":[]}`),
		},
	}}
	_, err := CreateIssue(context.Background(), fake, CreateIssueInput{Repo: "owner/repo", Title: "Title", Labels: []string{"bug"}})
	if err == nil || !strings.Contains(err.Error(), "labels readback disagrees") ||
		!strings.Contains(err.Error(), "issue was created at https://github.com/owner/repo/issues/42; do not retry creation") {
		t.Fatalf("error = %v, want label readback disagreement that forbids retrying creation", err)
	}
}

func TestEditIssueVerifiesFullDeltaAndPreservation(t *testing.T) {
	body := "New Body"
	milestone := ""
	projectItems := `[{"title":"Planning","status":{"name":"Todo","optionId":"1"}}]`
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Title","body":"Old Body","state":"OPEN","url":"https://github.com/owner/repo/issues/42","labels":[{"name":"keep"},{"name":"old"}],"assignees":[{"login":"old-user"}],"milestone":{"title":"v1"},"issueType":{"name":"Task"},"projectItems":` + projectItems + `}`),
		},
		{
			args: []string{
				"issue", "edit", "42", "--repo", "owner/repo",
				"--body", "New Body",
				"--add-label", "enhancement",
				"--remove-label", "old",
				"--add-assignee", "new-user",
				"--remove-assignee", "old-user",
				"--remove-milestone",
			},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Title","body":"New Body","state":"OPEN","url":"https://github.com/owner/repo/issues/42","labels":[{"name":"keep"},{"name":"enhancement"}],"assignees":[{"login":"new-user"}],"issueType":{"name":"Task"},"projectItems":` + projectItems + `}`),
		},
	}}
	_, err := EditIssue(context.Background(), fake, EditIssueInput{
		Repo: "owner/repo", Number: 42, Body: &body,
		AddLabels: []string{"enhancement"}, RemoveLabels: []string{"old"},
		AddAssignees: []string{"new-user"}, RemoveAssignees: []string{"old-user"},
		Milestone: &milestone,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEditIssueRejectsCollateralBodyChange(t *testing.T) {
	title := "New Title"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Old Title","body":"Preserve me","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`),
		},
		{
			args:   []string{"issue", "edit", "42", "--repo", "owner/repo", "--title", "New Title"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"New Title","body":"Changed elsewhere","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`),
		},
	}}
	_, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, Title: &title})
	if err == nil || !strings.Contains(err.Error(), "body readback disagrees") {
		t.Fatalf("error = %v, want collateral body change", err)
	}
}

func TestEditIssueRejectsConflictingLabelsAndInvalidState(t *testing.T) {
	state := "finished"
	for _, input := range []EditIssueInput{
		{Repo: "owner/repo", Number: 42, AddLabels: []string{"Bug"}, RemoveLabels: []string{"bug"}},
		{Repo: "owner/repo", Number: 42, State: &state},
	} {
		_, err := EditIssue(context.Background(), &fakeRunner{t: t}, input)
		if err == nil {
			t.Fatalf("EditIssue(%+v) error = nil", input)
		}
	}
}

const exactTitleSearchJQ = `{total_count, incomplete_results, items: [.items[] | {number, title, state, url: .html_url, pull_request: (.pull_request != null)}]}`

func exactTitleSearchCall(repo, title string, page int, output string, err error) fakeResponse {
	return fakeResponse{
		args: []string{
			"api", "-X", "GET",
			"-H", "Accept: application/vnd.github+json",
			"-H", "X-GitHub-Api-Version: 2026-03-10",
			"search/issues",
			"-f", `q=repo:` + repo + ` is:issue in:title "` + title + `"`,
			"-f", "per_page=100",
			"-f", "page=" + strconv.Itoa(page),
			"--jq", exactTitleSearchJQ,
		},
		output: []byte(output),
		err:    err,
	}
}

// exactTitleTestNow is the fixed clock for recent-open window tests.
var exactTitleTestNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func fixExactTitleClock(t *testing.T) {
	t.Helper()
	previous := exactTitleNow
	exactTitleNow = func() time.Time { return exactTitleTestNow }
	t.Cleanup(func() { exactTitleNow = previous })
}

// exactTitleTitlesCall is one titles-only GraphQL page of the newest open
// issues in owner/repo, requested after cursor ("" for the first page).
func exactTitleTitlesCall(cursor string, hasNext bool, endCursor string, nodes ...string) fakeResponse {
	args := []string{"api", "graphql", "-f", "query=" + exactTitleRecentQuery, "-f", "owner=owner", "-f", "name=repo"}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	return fakeResponse{
		args: args,
		output: []byte(fmt.Sprintf(`{"data":{"repository":{"issues":{"nodes":[%s],"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}`,
			strings.Join(nodes, ","), hasNext, endCursor)),
	}
}

// titleNode is an open issue created age before exactTitleTestNow.
func titleNode(number int, title string, age time.Duration) string {
	encoded, _ := json.Marshal(title)
	createdAt := exactTitleTestNow.Add(-age).Format(time.RFC3339)
	return fmt.Sprintf(`{"number":%d,"title":%s,"state":"OPEN","url":"https://github.com/owner/repo/issues/%d","createdAt":%q}`, number, encoded, number, createdAt)
}

// titleNodes returns count filler nodes numbered down from first, all
// created within the last hour.
func titleNodes(first, count int) []string {
	nodes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		nodes = append(nodes, titleNode(first-i, fmt.Sprintf("Other %d", first-i), time.Minute))
	}
	return nodes
}

func searchItem(number int, title string, pullRequest bool) string {
	encoded, _ := json.Marshal(title)
	return fmt.Sprintf(`{"number":%d,"title":%s,"state":"open","url":"https://github.com/owner/repo/issues/%d","pull_request":%t}`, number, encoded, number, pullRequest)
}

func searchPage(total int, incomplete bool, items ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"incomplete_results":%t,"items":[%s]}`, total, incomplete, strings.Join(items, ","))
}

func matchNumbers(matches []IssueSummary) []int {
	numbers := make([]int, 0, len(matches))
	for _, match := range matches {
		numbers = append(numbers, match.Number)
	}
	return numbers
}

func TestFindIssuesByExactTitleUsesSearchAndKeepsOnlyExactIssueTitles(t *testing.T) {
	fixExactTitleClock(t)
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		exactTitleSearchCall("owner/repo", "Fix the build", 1, searchPage(6, false,
			searchItem(9, "Fix the build", false),
			searchItem(8, "Fix the build", true),      // pull request
			searchItem(7, "fix the build", false),     // differs by case
			searchItem(6, "Fix the  build", false),    // differs by inner whitespace
			searchItem(5, "Fix the build now", false), // phrase match only
			searchItem(3, "Fix the build", false),
		), nil),
		// More recent open issues exist, but search was complete, so one page suffices.
		exactTitleTitlesCall("", true, "c1", titleNodes(300, 100)...),
	}}
	check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "  Fix the build  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := matchNumbers(check.Matches); !reflect.DeepEqual(got, []int{3, 9}) {
		t.Fatalf("matches = %v, want [3 9]", got)
	}
	if check.Matches[0] != (IssueSummary{Number: 3, Title: "Fix the build", State: "open", URL: "https://github.com/owner/repo/issues/3"}) {
		t.Fatalf("summary = %+v", check.Matches[0])
	}
	if check.Method != ExactTitleMethodSearchRecent || check.RecentWindow != "7d" || !check.Complete || check.Unchecked != "" {
		t.Fatalf("check = %+v, want complete search+recent with nothing unchecked", check)
	}
	if fake.calls != 2 {
		t.Fatalf("calls = %d, want 2", fake.calls)
	}
}

func TestFindIssuesByExactTitleFindsRecentIssueMissingFromSearchIndex(t *testing.T) {
	fixExactTitleClock(t)
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		exactTitleSearchCall("owner/repo", "Same", 1, searchPage(1, false, searchItem(2, "Same", false)), nil),
		exactTitleTitlesCall("", true, "c1",
			titleNode(40, "Same", time.Second),
			titleNode(39, "same", time.Minute),
			titleNode(38, " Same", time.Hour),
			titleNode(2, "Same", 2*time.Hour),
		),
	}}
	check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same")
	if err != nil {
		t.Fatal(err)
	}
	if got := matchNumbers(check.Matches); !reflect.DeepEqual(got, []int{2, 40}) {
		t.Fatalf("matches = %v, want deduplicated [2 40]", got)
	}
	if check.Matches[1].State != "open" {
		t.Fatalf("state = %q, want lower-case like search", check.Matches[1].State)
	}
	if !check.Complete || check.Unchecked != "" {
		t.Fatalf("check = %+v", check)
	}
}

func TestFindIssuesByExactTitlePaginatesSearch(t *testing.T) {
	fixExactTitleClock(t)
	first := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		first = append(first, searchItem(1000+i, "Same thing", false))
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		exactTitleSearchCall("owner/repo", "Same thing", 1, searchPage(101, false, first...), nil),
		exactTitleSearchCall("owner/repo", "Same thing", 2, searchPage(101, false, searchItem(4, "Same thing", false)), nil),
		exactTitleTitlesCall("", false, ""),
	}}
	check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same thing")
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Matches) != 101 || check.Matches[0].Number != 4 {
		t.Fatalf("matches = %d, first = %+v", len(check.Matches), check.Matches[0])
	}
}

func TestFindIssuesByExactTitleFallsBackToRecentOpenIssues(t *testing.T) {
	fixExactTitleClock(t)
	for name, searchResponses := range map[string][]fakeResponse{
		"incomplete results": {exactTitleSearchCall("owner/repo", "Same", 1, searchPage(1, true, searchItem(3, "Same", false)), nil)},
		"above search cap":   {exactTitleSearchCall("owner/repo", "Same", 1, searchPage(1001, false, searchItem(3, "Same", false)), nil)},
		"search error":       {exactTitleSearchCall("owner/repo", "Same", 1, "", errors.New("gh: API rate limit exceeded (HTTP 403)"))},
		"malformed response": {exactTitleSearchCall("owner/repo", "Same", 1, "not json", nil)},
		"short page":         {exactTitleSearchCall("owner/repo", "Same", 1, searchPage(5, false), nil)},
	} {
		t.Run(name, func(t *testing.T) {
			// 150 open issues; the window ends on the second page.
			page1 := append([]string{titleNode(150, "Same", time.Hour)}, titleNodes(149, 99)...)
			page2 := append(titleNodes(50, 10),
				titleNode(40, "Same", ExactTitleRecentWindow),               // exactly on the boundary: checked
				titleNode(39, "Same", ExactTitleRecentWindow+time.Second),   // just outside: scan stops
				titleNode(38, "Same", ExactTitleRecentWindow+2*time.Second), // never read
			)
			responses := append(append([]fakeResponse{}, searchResponses...),
				exactTitleTitlesCall("", true, "c1", page1...),
				exactTitleTitlesCall("c1", true, "c2", page2...),
			)
			fake := &fakeRunner{t: t, responses: responses}
			check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same")
			if err != nil {
				t.Fatal(err)
			}
			if got := matchNumbers(check.Matches); !reflect.DeepEqual(got, []int{40, 150}) {
				t.Fatalf("matches = %v, want [40 150]", got)
			}
			if check.Method != ExactTitleMethodRecentOpen || check.RecentWindow != "7d" || !check.Complete {
				t.Fatalf("check = %+v, want complete recent-open over 7d", check)
			}
			if check.Unchecked != "closed issues and issues created more than 7d ago" {
				t.Fatalf("unchecked = %q", check.Unchecked)
			}
			if fake.calls != len(responses) {
				t.Fatalf("calls = %d, want %d", fake.calls, len(responses))
			}
		})
	}
}

func TestFindIssuesByExactTitleFallbackStopsAtCapInsideWindow(t *testing.T) {
	fixExactTitleClock(t)
	responses := []fakeResponse{
		exactTitleSearchCall("owner/repo", "Same", 1, "", errors.New("search unavailable")),
	}
	cursor := ""
	for page := 0; page < ExactTitleRecentScanLimit/100; page++ {
		next := fmt.Sprintf("c%d", page+1)
		nodes := titleNodes(10000-page*100, 100)
		if page == 2 {
			nodes[7] = titleNode(10000-page*100-7, "Same", time.Hour)
		}
		responses = append(responses, exactTitleTitlesCall(cursor, true, next, nodes...))
		cursor = next
	}
	// A sixth page would fail the fake runner as an unexpected call.
	fake := &fakeRunner{t: t, responses: responses}
	check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same")
	if err != nil {
		t.Fatal(err)
	}
	if got := matchNumbers(check.Matches); !reflect.DeepEqual(got, []int{9793}) {
		t.Fatalf("matches = %v, want [9793]", got)
	}
	if check.Method != ExactTitleMethodRecentOpen || check.Complete {
		t.Fatalf("check = %+v, want incomplete recent-open", check)
	}
	if !strings.Contains(check.Unchecked, "closed issues") || !strings.Contains(check.Unchecked, "beyond the newest 500") {
		t.Fatalf("unchecked = %q", check.Unchecked)
	}
	if fake.calls != 6 {
		t.Fatalf("calls = %d, want 1 search + 5 titles pages", fake.calls)
	}
}

func TestFindIssuesByExactTitleRecentScanQueriesOnlyOpenIssues(t *testing.T) {
	// Closed issues are excluded by the query itself, not by later filtering.
	if !strings.Contains(exactTitleRecentQuery, "states: [OPEN])") || strings.Contains(exactTitleRecentQuery, "CLOSED") {
		t.Fatalf("query = %s", exactTitleRecentQuery)
	}
	if !strings.Contains(exactTitleRecentQuery, "createdAt") || !strings.Contains(exactTitleRecentQuery, "direction: DESC") {
		t.Fatalf("query = %s", exactTitleRecentQuery)
	}
}

func TestFindIssuesByExactTitleSkipsSearchForUnsearchableTitles(t *testing.T) {
	fixExactTitleClock(t)
	for name, title := range map[string]string{
		"long title":       strings.Repeat("word ", 50) + "end",
		"double quote":     `Say "hello"`,
		"backslash":        `path\\name`,
		"control":          "title \a\v value",
		"punctuation only": "!!",
		"format character": "\U000e0001",
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeRunner{t: t, responses: []fakeResponse{exactTitleTitlesCall("", false, "", titleNode(7, title, time.Hour))}}
			check, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "  "+title+"  ")
			if err != nil {
				t.Fatal(err)
			}
			if got := matchNumbers(check.Matches); !reflect.DeepEqual(got, []int{7}) {
				t.Fatalf("matches = %v, want [7]", got)
			}
			if fake.calls != 1 || check.Method != ExactTitleMethodRecentOpen || !check.Complete || check.Unchecked == "" {
				t.Fatalf("calls = %d, check = %+v, want only the recent-open scan", fake.calls, check)
			}
		})
	}
}

func TestFindIssuesByExactTitleTitlesErrorIsReported(t *testing.T) {
	fixExactTitleClock(t)
	titlesErr := exactTitleTitlesCall("", false, "")
	titlesErr.output, titlesErr.err = nil, errors.New("titles unavailable")
	for name, responses := range map[string][]fakeResponse{
		"after search": {exactTitleSearchCall("owner/repo", "Same", 1, searchPage(0, false), nil), titlesErr},
		"fallback":     {exactTitleSearchCall("owner/repo", "Same", 1, "", errors.New("search unavailable")), titlesErr},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeRunner{t: t, responses: responses}
			if _, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same"); err == nil || !strings.Contains(err.Error(), "titles unavailable") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFindIssuesByExactTitleRejectsMissingRepository(t *testing.T) {
	fixExactTitleClock(t)
	missing := exactTitleTitlesCall("", false, "")
	missing.output = []byte(`{"data":{"repository":null}}`)
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		exactTitleSearchCall("owner/repo", "Same", 1, searchPage(0, false), nil),
		missing,
	}}
	if _, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same"); err == nil || !strings.Contains(err.Error(), "repository not found") {
		t.Fatalf("err = %v", err)
	}
	if _, err := FindIssuesByExactTitle(context.Background(), &fakeRunner{t: t}, "owner", "Same"); err == nil {
		t.Fatal("invalid repository accepted")
	}
}

func TestEditIssueNoOpNeedsOnlyOneRead(t *testing.T) {
	title := "Same title"
	fake := &fakeRunner{t: t, responses: []fakeResponse{{
		args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
		output: []byte(`{"number":42,"title":"Same title","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`),
	}}}
	if _, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, Title: &title}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Fatalf("calls=%d, want only the initial read for an unchanged issue", fake.calls)
	}
}

const issueViewJSONFields = "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"

func TestEditIssueMapsNotPlannedForGhClose(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`)},
		{args: []string{"issue", "close", "42", "--repo", "owner/repo", "--reason", "not planned"}, output: []byte("Closed\n")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"NOT_PLANNED","url":"https://github.com/owner/repo/issues/42"}`)},
	}}
	if _, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state, CloseReason: "not_planned"}); err != nil {
		t.Fatal(err)
	}
}

func TestEditIssueChangesReasonOnAlreadyClosedIssue(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42"}`)},
		{args: []string{
			"api", "--method", "PATCH", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10",
			"repos/owner/repo/issues/42", "-f", "state=closed", "-f", "state_reason=not_planned", "--jq", ".state_reason",
		}, output: []byte("not_planned\n")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"NOT_PLANNED","url":"https://github.com/owner/repo/issues/42"}`)},
	}}
	view, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state, CloseReason: "not_planned"})
	if err != nil || view.StateReason != "NOT_PLANNED" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestEditIssueRejectsUnappliedCloseReason(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42"}`)},
		{args: []string{
			"api", "--method", "PATCH", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10",
			"repos/owner/repo/issues/42", "-f", "state=closed", "-f", "state_reason=not_planned", "--jq", ".state_reason",
		}, output: []byte("completed\n")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42"}`)},
	}}
	_, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state, CloseReason: "not_planned"})
	if err == nil || !strings.Contains(err.Error(), "close reason readback disagrees") {
		t.Fatalf("error = %v, want close reason disagreement", err)
	}
}

func TestEditIssueKeepsExistingReasonWhenNoneRequested(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"NOT_PLANNED","url":"https://github.com/owner/repo/issues/42"}`)},
	}}
	if _, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state}); err != nil {
		t.Fatal(err)
	}
}

func TestEditIssueReportsStatusSetByItemClosedWorkflow(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"OPEN","url":"https://github.com/owner/repo/issues/42","projectItems":[{"title":"Planning","status":{"name":"In Progress","optionId":"2"}}]}`)},
		{args: []string{"issue", "close", "42", "--repo", "owner/repo", "--reason", "completed"}, output: []byte("Closed\n")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42","projectItems":[{"title":"Planning","status":{"name":"Done","optionId":"3"}}]}`)},
	}}
	view, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state, CloseReason: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.AutomationSideEffects) != 1 || !strings.Contains(view.AutomationSideEffects[0], `"Done"`) {
		t.Fatalf("AutomationSideEffects = %v", view.AutomationSideEffects)
	}
}

func TestEditIssueStillRejectsProjectMembershipChangeOnClose(t *testing.T) {
	state := "closed"
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"OPEN","url":"https://github.com/owner/repo/issues/42","projectItems":[{"title":"Planning","status":{"name":"Todo","optionId":"1"}}]}`)},
		{args: []string{"issue", "close", "42", "--repo", "owner/repo"}, output: []byte("Closed\n")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}, output: []byte(`{"number":42,"title":"T","state":"CLOSED","stateReason":"COMPLETED","url":"https://github.com/owner/repo/issues/42","projectItems":[]}`)},
	}}
	_, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, State: &state})
	if err == nil || !strings.Contains(err.Error(), "Project membership") {
		t.Fatalf("error = %v, want membership preservation failure", err)
	}
}

func TestCreateIssueAlwaysPassesBodyEvenWhenEmpty(t *testing.T) {
	// gh rejects a non-interactive issue create without --body.
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Title", "--body", ""},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields},
			output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42"}`),
		},
	}}
	if _, err := CreateIssue(context.Background(), fake, CreateIssueInput{Repo: "owner/repo", Title: "Title"}); err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
}

func TestCreateIssueReadbackFailureForbidsRetry(t *testing.T) {
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Title", "--body", "Body"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields},
			err:  errors.New("HTTP 502"),
		},
	}}
	_, err := CreateIssue(context.Background(), fake, CreateIssueInput{Repo: "owner/repo", Title: "Title", Body: "Body"})
	if err == nil || !strings.Contains(err.Error(), "issue was created at https://github.com/owner/repo/issues/42; do not retry creation") ||
		!strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want created-issue retry warning", err)
	}
}

func TestCreateIssueResolvesSelfAssigneeBeforeVerification(t *testing.T) {
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"api", "user", "--jq", ".login"},
			output: []byte("octocat\n"),
		},
		{
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Title", "--body", "", "--assignee", "octocat", "--assignee", "monalisa"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields},
			output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","assignees":[{"login":"octocat"},{"login":"monalisa"}]}`),
		},
	}}
	if _, err := CreateIssue(context.Background(), fake, CreateIssueInput{Repo: "owner/repo", Title: "Title", Assignees: []string{"@me", "monalisa"}}); err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
}

func TestEditIssueResolvesSelfAssigneeOnceForAddAndRemove(t *testing.T) {
	view := []string{"issue", "view", "42", "--repo", "owner/repo", "--json", issueViewJSONFields}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "user", "--jq", ".login"}, output: []byte("octocat\n")},
		{args: view, output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","assignees":[{"login":"octocat"}]}`)},
		{
			args:   []string{"issue", "edit", "42", "--repo", "owner/repo", "--add-assignee", "monalisa", "--remove-assignee", "octocat"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{args: view, output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","assignees":[{"login":"monalisa"}]}`)},
	}}
	if _, err := EditIssue(context.Background(), fake, EditIssueInput{
		Repo:            "owner/repo",
		Number:          42,
		AddAssignees:    []string{"monalisa"},
		RemoveAssignees: []string{"@me"},
	}); err != nil {
		t.Fatalf("EditIssue error = %v", err)
	}

	fake = &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "user", "--jq", ".login"}, output: []byte("octocat\n")},
		{args: view, output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","assignees":[]}`)},
		{
			args:   []string{"issue", "edit", "42", "--repo", "owner/repo", "--add-assignee", "octocat"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{args: view, output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","assignees":[{"login":"octocat"}]}`)},
	}}
	if _, err := EditIssue(context.Background(), fake, EditIssueInput{Repo: "owner/repo", Number: 42, AddAssignees: []string{"@me"}}); err != nil {
		t.Fatalf("EditIssue add @me error = %v", err)
	}
}
