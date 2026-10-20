package githubcli

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultAllCreateAndCustomBacklogPreservation(t *testing.T) {
	all := standardBacklogTestView("a", 1)
	backlog := standardBacklogTestView("b", 2)
	backlog.Name = "Backlog"
	filter := "status:Todo -is:closed"
	backlog.Filter = &filter
	changes, err := planBacklogViewChanges([]projectViewNode{all, backlog}, backlogTestSpec())
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
	changes, err = planBacklogViewChanges([]projectViewNode{backlog}, backlogTestSpec())
	if err != nil || len(changes) != 1 || changes[0].Action != "create_view" {
		t.Fatalf("missing all: %v %v", changes, err)
	}
	if _, err := planBacklogViewChanges([]projectViewNode{all, backlog, backlog}, backlogTestSpec()); err == nil {
		t.Fatal("duplicate legacy Backlogs must be preserved for explicit reconciliation")
	}
	client := &captureClient{}
	if _, err := createBacklogView(context.Background(), client, userSetupProject(), backlogViewState{ownerType: "user", spec: backlogTestSpec()}); err != nil {
		t.Fatal(err)
	}
	if client.requests[0].body.(map[string]any)["name"] != "all" {
		t.Fatal("new overview must be lowercase all")
	}
	wanted := overviewSpec(backlogTestSpec(), backlog)
	if err := updateBacklogView(context.Background(), client, "b", wanted); err != nil {
		t.Fatal(err)
	}
	input := client.requests[1].variables["input"].(map[string]any)
	if input["name"] != "Backlog" || input["filter"] != filter {
		t.Fatalf("rewrote custom Backlog: %v", input)
	}
	backlog.SortByFields.Nodes[0].Direction = "DESC"
	if _, err := planBacklogViewChanges([]projectViewNode{all, backlog}, backlogTestSpec()); err == nil || !strings.Contains(err.Error(), "no replacement Backlog") {
		t.Fatalf("need manual preservation boundary: %v", err)
	}
}

func TestApplyOverviewRejectsChangedFilterReadback(t *testing.T) {
	all := standardBacklogTestView("v1", 1)
	original := "status:Todo"
	all.Filter = &original
	all.Configuration.VisibleFields.Nodes = all.Configuration.VisibleFields.Nodes[:3]
	changed := standardBacklogTestView("v1", 1)
	wrong := "status:Done"
	changed.Filter = &wrong
	update := updateViewRequest()
	update.variables["input"].(map[string]any)["filter"] = original
	steps := backlogInspectSteps(t, all)
	steps = append(steps, seqStep{request: update, out: `{}`})
	steps = append(steps, backlogInspectSteps(t, changed)...)
	client := &seqClient{t: t, steps: steps}
	result, err := ApplyStandardBacklogView(context.Background(), client, userSetupProject())
	client.done()
	if err == nil || result.Verified || !strings.Contains(err.Error(), "preserve its filter") {
		t.Fatalf("wrong filter readback accepted: %v %v", result, err)
	}
}

func TestLayoutWidthBoundaryIsExplicit(t *testing.T) {
	checks := strings.Join(viewManualVerification("organization"), " ")
	for _, value := range []string{"Title 517", "Due date 131", "Target date 157", "Priority 130", "Class/Type 116", "Sub-issues progress 174", "Status 128", "Assignees 135", "Linked pull requests 154", "does not configure or verify"} {
		if !strings.Contains(checks, value) {
			t.Fatalf("missing %q in %q", value, checks)
		}
	}
}
