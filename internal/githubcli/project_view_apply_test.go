package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const backlogRESTFieldsJSON = `[[{"id":1,"node_id":"title","name":"Title","data_type":"title"},{"id":2,"node_id":"status","name":"Status","data_type":"single_select"},{"id":3,"node_id":"priority","name":"Priority","data_type":"single_select"},{"id":4,"node_id":"class","name":"Class","data_type":"single_select"}]]`

func viewsJSON(t *testing.T, views ...projectViewNode) string {
	t.Helper()
	if views == nil {
		views = []projectViewNode{}
	}
	project := userSetupProject()
	data := map[string]any{
		"number": project.Number, "title": project.Title,
		"views": map[string]any{"nodes": views, "pageInfo": map[string]any{"hasNextPage": false}},
	}
	encoded, err := json.Marshal(map[string]any{"data": map[string]any{"user": map[string]any{"projectV2": data}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func descendingBacklogView(id string, number int) projectViewNode {
	view := standardBacklogTestView(id, number)
	view.SortByFields.Nodes[0].Direction = "DESC"
	return view
}

func backlogInspectSteps(t *testing.T, views ...projectViewNode) []seqStep {
	return []seqStep{
		{want: "users/octo-user/projectsV2/40/fields", out: backlogRESTFieldsJSON},
		{want: "views(first", out: viewsJSON(t, views...)},
	}
}

func TestApplyStandardBacklogViewReplacesAndVerifiesWithoutOwnerRediscovery(t *testing.T) {
	steps := backlogInspectSteps(t, descendingBacklogView("v1", 1))
	steps = append(steps,
		seqStep{want: "users/octo-user/projectsV2/40/views", out: `{"node_id":"v2"}`},
		seqStep{want: "views(first", out: viewsJSON(t, descendingBacklogView("v1", 1), standardBacklogTestView("v2", 2))},
		seqStep{want: `"viewId":"v1"`, out: `{}`},
	)
	steps = append(steps, backlogInspectSteps(t, standardBacklogTestView("v2", 2))...)
	runner := &seqRunner{t: t, steps: steps}
	result, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err != nil {
		t.Fatal(err)
	}
	runner.done()
	if !result.Verified || len(result.Applied) != 1 || result.Applied[0].Action != "replace_view" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyStandardBacklogViewDeletesUnverifiedReplacement(t *testing.T) {
	steps := backlogInspectSteps(t, descendingBacklogView("v1", 1))
	steps = append(steps,
		seqStep{want: "users/octo-user/projectsV2/40/views", out: `{"node_id":"v2"}`},
		seqStep{want: "views(first", out: viewsJSON(t, descendingBacklogView("v1", 1), descendingBacklogView("v2", 2))},
		seqStep{want: `"viewId":"v2"`, out: `{}`},
	)
	runner := &seqRunner{t: t, steps: steps}
	_, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err == nil {
		t.Fatal("expected verification failure")
	}
	runner.done()
	for _, want := range []string{"deleted again", "original Backlog #1 (v1) was preserved", "applied: nothing; failed at replace_view v1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestApplyStandardBacklogViewNamesBothViewsWhenCleanupFails(t *testing.T) {
	steps := backlogInspectSteps(t, descendingBacklogView("v1", 1))
	steps = append(steps,
		seqStep{want: "users/octo-user/projectsV2/40/views", out: `{"node_id":"v2"}`},
		seqStep{want: "views(first", err: errors.New("HTTP 502")},
		seqStep{want: `"viewId":"v2"`, err: errors.New("HTTP 502")},
	)
	runner := &seqRunner{t: t, steps: steps}
	_, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err == nil {
		t.Fatal("expected failure")
	}
	runner.done()
	for _, want := range []string{"two Backlog views", "original Backlog #1 (v1)", "replacement Backlog (v2)", `deleteProjectV2View(input: {viewId: "v2"})`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestApplyStandardBacklogViewOldDeleteFailureIsRecoverable(t *testing.T) {
	steps := backlogInspectSteps(t, descendingBacklogView("v1", 1))
	steps = append(steps,
		seqStep{want: "users/octo-user/projectsV2/40/views", out: `{"node_id":"v2"}`},
		seqStep{want: "views(first", out: viewsJSON(t, descendingBacklogView("v1", 1), standardBacklogTestView("v2", 2))},
		seqStep{want: `"viewId":"v1"`, err: errors.New("HTTP 502")},
	)
	runner := &seqRunner{t: t, steps: steps}
	_, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err == nil || !strings.Contains(err.Error(), "will plan deleting the non-standard duplicate") {
		t.Fatalf("error = %v, want a recoverable rerun instruction", err)
	}
	runner.done()

	// The rerun must finish the replacement rather than refuse the duplicate.
	steps = backlogInspectSteps(t, descendingBacklogView("v1", 1), standardBacklogTestView("v2", 2))
	steps = append(steps, seqStep{want: `"viewId":"v1"`, out: `{}`})
	steps = append(steps, backlogInspectSteps(t, standardBacklogTestView("v2", 2))...)
	runner = &seqRunner{t: t, steps: steps}
	result, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err != nil {
		t.Fatal(err)
	}
	runner.done()
	if !result.Verified || len(result.Applied) != 1 || result.Applied[0].Action != "delete_view" || result.Applied[0].ViewID != "v1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPlanBacklogViewDeletesNonStandardDuplicate(t *testing.T) {
	views := []projectViewNode{standardBacklogTestView("v2", 2), descendingBacklogView("v1", 1)}
	changes, err := planBacklogViewChanges(views, backlogTestSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != "delete_view" || changes[0].ViewID != "v1" || !strings.Contains(changes[0].Detail, "keep standard Backlog #2 (v2)") {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestPlanBacklogViewRefusesTwoNonStandardBacklogsWithRemedy(t *testing.T) {
	views := []projectViewNode{descendingBacklogView("v1", 1), descendingBacklogView("v2", 2)}
	_, err := planBacklogViewChanges(views, backlogTestSpec())
	if err == nil || !strings.Contains(err.Error(), "Backlog #1 (v1), Backlog #2 (v2)") || !strings.Contains(err.Error(), "rename or delete all but one") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyStandardBacklogViewReportsReadbackFailureWithAppliedChanges(t *testing.T) {
	view := standardBacklogTestView("v1", 1)
	view.Layout = "BOARD_LAYOUT"
	steps := backlogInspectSteps(t, view)
	steps = append(steps, seqStep{want: "updateProjectV2View", out: `{}`})
	steps = append(steps, backlogInspectSteps(t, view)...)
	runner := &seqRunner{t: t, steps: steps}
	result, err := ApplyStandardBacklogView(context.Background(), runner, userSetupProject())
	if err == nil || !strings.Contains(err.Error(), "applied: update_view v1; failed at readback") {
		t.Fatalf("error = %v", err)
	}
	runner.done()
	if len(result.Applied) != 1 {
		t.Fatalf("applied = %#v", result.Applied)
	}
}
