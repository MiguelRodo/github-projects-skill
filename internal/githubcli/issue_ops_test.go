package githubcli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
			args:   []string{"issue", "create", "--repo", "owner/repo", "--title", "Title", "--label", "bug"},
			output: []byte("https://github.com/owner/repo/issues/42\n"),
		},
		{
			args:   []string{"issue", "view", "42", "--repo", "owner/repo", "--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"},
			output: []byte(`{"number":42,"title":"Title","body":"","state":"OPEN","url":"https://github.com/owner/repo/issues/42","labels":[]}`),
		},
	}}
	_, err := CreateIssue(context.Background(), fake, CreateIssueInput{Repo: "owner/repo", Title: "Title", Labels: []string{"bug"}})
	if err == nil || !strings.Contains(err.Error(), "labels readback disagrees") {
		t.Fatalf("error = %v, want label readback disagreement", err)
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

func TestFindIssuesByExactTitleIsCompleteAndExcludesPullRequests(t *testing.T) {
	fake := &fakeRunner{t: t, responses: []fakeResponse{{
		args: []string{
			"api", "--paginate",
			"-H", "Accept: application/vnd.github+json",
			"-H", "X-GitHub-Api-Version: 2026-03-10",
			"repos/owner/repo/issues?state=all&per_page=100",
			"--jq", `.[] | select((.pull_request == null) and (.title == "Same")) | {number, title, state, url: .html_url}`,
		},
		output: []byte("{\"number\":3,\"title\":\"Same\",\"state\":\"open\",\"url\":\"https://github.com/owner/repo/issues/3\"}\n{\"number\":2,\"title\":\"Same\",\"state\":\"closed\",\"url\":\"https://github.com/owner/repo/issues/2\"}\n"),
	}}}
	matches, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "Same")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].Number != 2 || matches[1].Number != 3 {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestExactTitleFilterUsesJSONQuotingAndTrimsOuterWhitespace(t *testing.T) {
	for _, title := range []string{"title \a\v value", "quote\"backslash\\", "\U000e0001", "plain title"} {
		t.Run(title, func(t *testing.T) {
			encoded, err := json.Marshal(title)
			if err != nil {
				t.Fatal(err)
			}
			fake := &fakeRunner{t: t, responses: []fakeResponse{{
				args: []string{
					"api", "--paginate", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10",
					"repos/owner/repo/issues?state=all&per_page=100", "--jq",
					`.[] | select((.pull_request == null) and (.title == ` + string(encoded) + `)) | {number, title, state, url: .html_url}`,
				}, output: []byte(""),
			}}}
			if _, err := FindIssuesByExactTitle(context.Background(), fake, "owner/repo", "  "+title+"  "); err != nil {
				t.Fatal(err)
			}
		})
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
