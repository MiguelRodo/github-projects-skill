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

func TestApplyOverviewKeepsEachViewsOwnExtras(t *testing.T) {
	all := standardBacklogTestView("v1", 1)
	backlog := standardBacklogTestView("v2", 2)
	backlog.Name = "Backlog"
	filter := "status:Todo -is:closed"
	backlog.Filter = &filter
	alpha := projectViewFieldRef{ID: "alpha", Name: "All-only"}
	beta := projectViewFieldRef{ID: "beta", Name: "Backlog-only"}
	all.Configuration.VisibleFields.Nodes = append(all.Configuration.VisibleFields.Nodes, alpha)
	backlog.Configuration.VisibleFields.Nodes = append(backlog.Configuration.VisibleFields.Nodes, beta)
	duplicate := backlog
	duplicate.Name = "all"
	duplicate.Filter = all.Filter
	if _, err := planBacklogViewChanges([]projectViewNode{all, duplicate}, backlogTestSpec()); err == nil {
		t.Fatal("different extras on duplicate all views must not be inferred as interrupted replacement")
	}
	fields := strings.TrimSuffix(backlogRESTFieldsJSON, "]") + `,{"id":5,"node_id":"alpha","name":"All-only","data_type":"text"},{"id":6,"node_id":"beta","name":"Backlog-only","data_type":"text"}]`
	inspect := func(views ...projectViewNode) []seqStep {
		return []seqStep{{request: viewsRequest(), out: viewsJSON(t, views...)}, {request: request{method: "GET", path: "/users/octo-user/projectsV2/40/fields?per_page=100"}, out: fields}}
	}
	// Already-standard views with different useful extras are independent no-ops.
	steps := append(inspect(all, backlog), inspect(all, backlog)...)
	client := &seqClient{t: t, steps: steps}
	result, err := ApplyStandardBacklogView(context.Background(), client, userSetupProject())
	client.done()
	if err != nil || !result.Verified || len(result.Applied) != 0 {
		t.Fatalf("independent no-op: %v %v", result, err)
	}

	// When both need Class, their PATCH payloads and readback still keep extras local.
	oldAll, oldBacklog := all, backlog
	oldAll.Configuration.VisibleFields.Nodes = []projectViewFieldRef{{ID: "title", Name: "Title"}, {ID: "priority", Name: "Priority"}, {ID: "status", Name: "Status"}, alpha}
	oldBacklog.Configuration.VisibleFields.Nodes = []projectViewFieldRef{{ID: "title", Name: "Title"}, {ID: "priority", Name: "Priority"}, {ID: "status", Name: "Status"}, beta}
	update := func(id, name, query, extra string) request {
		r := updateViewRequest()
		input := r.variables["input"].(map[string]any)
		input["viewId"], input["name"], input["filter"] = id, name, query
		input["configuration"] = map[string]any{"visibleFieldIds": []string{"title", "priority", "class", "status", extra}}
		return r
	}
	steps = inspect(oldAll, oldBacklog)
	steps = append(steps, seqStep{request: update("v1", "all", "", "alpha"), out: `{}`}, seqStep{request: update("v2", "Backlog", filter, "beta"), out: `{}`})
	steps = append(steps, inspect(all, backlog)...)
	client = &seqClient{t: t, steps: steps}
	result, err = ApplyStandardBacklogView(context.Background(), client, userSetupProject())
	client.done()
	if err != nil || !result.Verified || len(result.Applied) != 2 {
		t.Fatalf("independent apply: %v %v", result, err)
	}
}
