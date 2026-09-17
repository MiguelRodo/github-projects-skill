package githubcli

import (
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

func TestPlanBacklogViewRecoversDuplicateAroundOneExactView(t *testing.T) {
	exact := standardBacklogTestView("v2", 2)
	old := standardBacklogTestView("v1", 1)
	old.GroupByFields.Nodes = nil
	changes, err := planBacklogViewChanges([]projectViewNode{old, exact}, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	want := []StandardBacklogViewChange{{Action: "delete_duplicate_view", ViewID: "v1", Detail: "keep standard Backlog view #2"}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestPlanBacklogViewRejectsAmbiguousDuplicates(t *testing.T) {
	first := standardBacklogTestView("v1", 1)
	first.GroupByFields.Nodes = nil
	second := standardBacklogTestView("v2", 2)
	second.SortByFields.Nodes = nil
	if _, err := planBacklogViewChanges([]projectViewNode{first, second}, backlogTestSpec()); err == nil || !strings.Contains(err.Error(), "ambiguous replacement") {
		t.Fatalf("error = %v, want ambiguous replacement", err)
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

func TestBuildBacklogViewSpecUsesIssueTypeForOrganization(t *testing.T) {
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
	if got := spec.Visible[len(spec.Visible)-1].Name; got != "Type" {
		t.Fatalf("last visible field = %q, want Type", got)
	}
}
