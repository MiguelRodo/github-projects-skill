package githubcli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

func backlogTestSpec() backlogViewSpec {
	return backlogViewSpec{
		Visible: []backlogField{
			{Name: "Title", RESTID: 1, NodeID: "title"},
			{Name: "Status", RESTID: 2, NodeID: "status"},
			{Name: "Priority", RESTID: 3, NodeID: "priority"},
			{Name: "Class", RESTID: 4, NodeID: "class"},
		},
		Status:   backlogField{Name: "Status", RESTID: 2, NodeID: "status"},
		Priority: backlogField{Name: "Priority", RESTID: 3, NodeID: "priority"},
	}
}

func standardBacklogTestView(id string, number int) projectViewNode {
	filter := ""
	view := projectViewNode{ID: id, Number: number, Name: "Backlog", Layout: "TABLE_LAYOUT", Filter: &filter}
	view.Configuration.VisibleFields.Nodes = []projectViewFieldRef{{ID: "title", Name: "Title"}, {ID: "status", Name: "Status"}, {ID: "priority", Name: "Priority"}, {ID: "class", Name: "Class"}}
	view.GroupByFields.Nodes = []projectViewFieldRef{{ID: "status", Name: "Status"}}
	view.SortByFields.Nodes = []projectViewSortNode{{Direction: "ASC", Field: projectViewFieldRef{ID: "priority", Name: "Priority"}}}
	return view
}

func TestPlanBacklogViewCreateWhenMissing(t *testing.T) {
	changes, err := planBacklogViewChanges(nil, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != "create_view" {
		t.Fatalf("changes = %#v, want create_view", changes)
	}
}

func TestPlanBacklogViewNoopWhenStandard(t *testing.T) {
	changes, err := planBacklogViewChanges([]projectViewNode{standardBacklogTestView("v1", 1)}, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want no changes", changes)
	}
}

func TestPlanBacklogViewUpdatesBasicConfigurationInPlace(t *testing.T) {
	view := standardBacklogTestView("v1", 1)
	view.Layout = "BOARD_LAYOUT"
	changes, err := planBacklogViewChanges([]projectViewNode{view}, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != "update_view" || changes[0].ViewID != "v1" {
		t.Fatalf("changes = %#v, want update_view for v1", changes)
	}
}

func TestPlanBacklogViewReplacesWhenGroupOrSortDiffers(t *testing.T) {
	view := standardBacklogTestView("v1", 1)
	view.SortByFields.Nodes[0].Direction = "DESC"
	changes, err := planBacklogViewChanges([]projectViewNode{view}, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != "replace_view" || !strings.Contains(changes[0].Detail, "cannot update group/sort") {
		t.Fatalf("changes = %#v, want replace_view", changes)
	}
}

func TestPlanBacklogViewRejectsDuplicateBacklogs(t *testing.T) {
	views := []projectViewNode{standardBacklogTestView("v1", 1), standardBacklogTestView("v2", 2)}
	if _, err := planBacklogViewChanges(views, backlogTestSpec()); err == nil || !strings.Contains(err.Error(), "ambiguous reconciliation") {
		t.Fatalf("error = %v, want ambiguous reconciliation", err)
	}
}

func TestBuildBacklogViewSpecUsesStandardOrderAndSkipsUnavailableOptionalFields(t *testing.T) {
	project := contract.Project{Owner: "octo-user", Number: 4, Title: "Planning"}
	fields := []restProjectField{
		{ID: 1, NodeID: "node-title", Name: "Title", DataType: "title"},
		{ID: 2, NodeID: "node-status", Name: "Status", DataType: "single_select"},
		{ID: 3, NodeID: "node-assignees", Name: "People", DataType: "assignees"},
		{ID: 4, NodeID: "node-priority", Name: "Priority", DataType: "single_select"},
		{ID: 5, NodeID: "node-class", Name: "Class", DataType: "single_select"},
	}
	spec, err := buildBacklogViewSpec(project, "user", fields)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, field := range spec.Visible {
		names = append(names, field.Name)
	}
	want := []string{"Title", "Status", "People", "Priority", "Class"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("visible = %#v, want %#v", names, want)
	}
}

func TestBuildBacklogViewSpecUsesNativeIssueTypeForOrganization(t *testing.T) {
	project := contract.Project{Owner: "octo-org", Number: 12, Title: "Planning"}
	fields := []restProjectField{
		{ID: 1, NodeID: "node-title", Name: "Title", DataType: "title"},
		{ID: 2, NodeID: "node-status", Name: "Status", DataType: "single_select"},
		{ID: 3, NodeID: "node-priority", Name: "Priority", DataType: "single_select"},
		{ID: 4, NodeID: "node-issue-type", Name: "Type", DataType: "issue_type"},
	}
	spec, err := buildBacklogViewSpec(project, "organization", fields)
	if err != nil {
		t.Fatal(err)
	}
	// The native Issue Type must exist for the organisation Project, but GitHub
	// cannot make it a visible view column, so it is not part of the view spec.
	for _, field := range spec.Visible {
		if field.Name == "Type" {
			t.Fatalf("visible = %#v, want the native issue-type field excluded", spec.Visible)
		}
	}
	if len(spec.Visible) != 3 {
		t.Fatalf("visible = %#v, want only the three view-visible standard fields", spec.Visible)
	}
}

func TestBuildBacklogViewSpecRequiresNativeIssueTypeForOrganization(t *testing.T) {
	project := contract.Project{Owner: "octo-org", Number: 12, Title: "Planning"}
	fields := []restProjectField{
		{ID: 1, NodeID: "node-title", Name: "Title", DataType: "title"},
		{ID: 2, NodeID: "node-status", Name: "Status", DataType: "single_select"},
		{ID: 3, NodeID: "node-priority", Name: "Priority", DataType: "single_select"},
	}
	if _, err := buildBacklogViewSpec(project, "organization", fields); err == nil {
		t.Fatal("syntax: organisation Project without a native Issue Type must fail inspection")
	}
}

func TestBacklogViewMatchesIgnoresVisibleFieldOrder(t *testing.T) {
	view := standardBacklogTestView("v1", 1)
	view.Configuration.VisibleFields.Nodes = []projectViewFieldRef{
		{ID: "priority", Name: "Priority"},
		{ID: "class", Name: "Class"},
		{ID: "title", Name: "Title"},
		{ID: "status", Name: "Status"},
	}
	if !backlogViewMatches(view, backlogTestSpec()) {
		t.Fatal("a standard view with provider-ordered visible fields must match")
	}
}

type captureArgsRunner struct {
	args [][]string
}

func (r *captureArgsRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.args = append(r.args, args)
	return []byte(`{"node_id":"view-node-id"}`), nil
}

func (r *captureArgsRunner) RunInput(_ context.Context, input []byte, args ...string) ([]byte, error) {
	r.args = append(r.args, args)
	_ = input
	return []byte(`{"node_id":"view-node-id"}`), nil
}

func TestCreateBacklogViewUsesAccountLoginForUserProjects(t *testing.T) {
	state := backlogViewState{ownerType: "user", spec: backlogTestSpec()}
	for _, tc := range []struct {
		ownerType string
		owner     string
		wantPath  string
	}{
		{ownerType: "user", owner: "octo-user", wantPath: "users/octo-user/projectsV2/4/views"},
		{ownerType: "organization", owner: "octo-org", wantPath: "orgs/octo-org/projectsV2/4/views"},
	} {
		runner := &captureArgsRunner{}
		state.ownerType = tc.ownerType
		if _, err := createBacklogView(context.Background(), runner, contract.Project{Owner: tc.owner, Number: 4}, state); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(runner.args[len(runner.args)-1], " ")
		if !strings.Contains(joined, tc.wantPath) {
			t.Fatalf("create args = %s, want path %s", joined, tc.wantPath)
		}
		if strings.Contains(joined, "users/4/") {
			t.Fatalf("create args = %s, want the account login rather than a database ID", joined)
		}
	}
}

type captureInputRunner struct {
	input []byte
}

func (r *captureInputRunner) Run(context.Context, ...string) ([]byte, error) {
	return nil, nil
}

func (r *captureInputRunner) RunInput(_ context.Context, input []byte, _ ...string) ([]byte, error) {
	r.input = append([]byte(nil), input...)
	return []byte(`{}`), nil
}

func TestDeleteProjectViewUsesDeleteMutationReturnField(t *testing.T) {
	runner := &captureInputRunner{}
	if err := deleteProjectView(context.Background(), runner, "view-1"); err != nil {
		t.Fatal(err)
	}
	payload := string(runner.input)
	if !strings.Contains(payload, "projectV2View") || strings.Contains(payload, "projectV2 {") {
		t.Fatalf("delete mutation payload = %s", payload)
	}
}
