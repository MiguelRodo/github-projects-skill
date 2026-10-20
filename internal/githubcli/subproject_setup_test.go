package githubcli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

func TestSubprojectSetupPreservesIDsColoursDescriptionsAndExtras(t *testing.T) {
	project := userSetupProject()
	project.Subproject = contract.SubprojectBinding{Storage: "field", Field: "Sub-project", Vocabulary: map[string]string{"tools": "Tools"}}
	current := []detailedProjectOption{{ID: "tools-id", Name: "Tools", Color: "BLUE", Description: "keep"}, {ID: "extra-id", Name: "Extra", Color: "RED", Description: "custom"}}
	state := setupState{project: detailedProjectSchema{Fields: map[string]detailedProjectField{"sub-project": {Options: current}}}}
	_, desired, err := subprojectSetupProfile(project, state)
	if err != nil {
		t.Fatal(err)
	}
	got, changed, err := reconcileProjectOptions(current, desired)
	if err != nil || !changed || len(got) != 3 {
		t.Fatalf("got=%+v changed=%v err=%v", got, changed, err)
	}
	if !reflect.DeepEqual(got[:2], current) || got[2].Name != "Not assigned" {
		t.Fatalf("existing options changed: %+v", got)
	}
	state.project.Fields["sub-project"] = detailedProjectField{Options: got}
	_, desired, err = subprojectSetupProfile(project, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err = reconcileProjectOptions(got, desired); err != nil || changed {
		t.Fatalf("repeat changed=%v err=%v", changed, err)
	}
}

func TestSubprojectSetupEmptyVocabularyCreatesOnlySchemaOption(t *testing.T) {
	project := userSetupProject()
	project.Subproject = contract.SubprojectBinding{Storage: "field"}
	state := setupState{project: detailedProjectSchema{ID: "project-node", Fields: map[string]detailedProjectField{}}}
	name, desired, err := subprojectSetupProfile(project, state)
	if err != nil || name != "Sub-project" || !reflect.DeepEqual(desired, []standardOption{{Name: "Not assigned", Color: "GRAY"}}) {
		t.Fatalf("name=%q options=%+v err=%v", name, desired, err)
	}
	client := &captureClient{}
	_, err = applyProjectFieldChange(context.Background(), client, project, state, StandardProjectSetupChange{Action: "create_field", Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].query, "createProjectV2Field") {
		t.Fatalf("unexpected writes: %+v", client.requests)
	}
	variables := client.requests[0].variables
	options, ok := variables["options"].([]map[string]any)
	if !ok || len(options) != 1 || options[0]["name"] != "Not assigned" {
		t.Fatalf("options payload = %#v", variables)
	}
	for _, request := range client.requests {
		if strings.Contains(request.query, "ItemFieldValue") {
			t.Fatal("setup must never assign item values")
		}
	}
}

func TestSubprojectSetupNeverConvertsLegacyLabels(t *testing.T) {
	project := userSetupProject()
	project.Subproject = contract.SubprojectBinding{Storage: "labels", Vocabulary: map[string]string{"tools": "subproject:tools"}}
	name, options, err := subprojectSetupProfile(project, setupState{})
	if err != nil || name != "" || len(options) != 0 {
		t.Fatalf("legacy binding changed: %q %+v %v", name, options, err)
	}
}

func TestSubprojectSetupPlansConfiguredFieldWithEmptyVocabulary(t *testing.T) {
	project := userSetupProject()
	project.Subproject = contract.SubprojectBinding{Storage: "field", Field: "Sub-project"}
	state := setupState{ownerType: "user", project: detailedProjectSchema{Fields: map[string]detailedProjectField{}}}
	plan, err := planStandardProjectSetupFromState(project, state)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, change := range plan.Changes {
		if change.Name == "Sub-project" {
			count++
			if change.Action != "create_field" || change.Detail != "SINGLE_SELECT" {
				t.Fatalf("unexpected field plan: %+v", change)
			}
		}
	}
	if count != 1 {
		t.Fatalf("Sub-project changes = %d, want one", count)
	}
}
