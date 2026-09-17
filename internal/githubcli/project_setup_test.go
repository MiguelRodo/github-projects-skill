package githubcli

import (
	"reflect"
	"testing"
)

func TestReconcileProjectOptionsRenamesLegacyPriorityAndPreservesExtras(t *testing.T) {
	current := []detailedProjectOption{
		{ID: "urgent", Name: "Urgent", Color: "RED", Description: "keep"},
		{ID: "high", Name: "High", Color: "YELLOW"},
		{ID: "medium", Name: "Medium", Color: "BLUE"},
		{ID: "low", Name: "Low", Color: "GRAY"},
		{ID: "later", Name: "Maybe later", Color: "BLUE", Description: "custom"},
	}

	got, changed, err := reconcileProjectOptions(current, standardPriorityOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	wantNames := []string{"P0", "P1", "P2", "P3", "Maybe later"}
	wantIDs := []string{"urgent", "high", "medium", "low", "later"}
	wantColours := []string{"RED", "ORANGE", "YELLOW", "PURPLE", "BLUE"}
	for index := range wantNames {
		if got[index].Name != wantNames[index] || got[index].ID != wantIDs[index] || got[index].Color != wantColours[index] {
			t.Fatalf("option %d = %+v, want name=%q id=%q colour=%q", index, got[index], wantNames[index], wantIDs[index], wantColours[index])
		}
	}
	if got[0].Description != "keep" || got[4].Description != "custom" {
		t.Fatalf("descriptions not preserved: %+v", got)
	}
}

func TestReconcileProjectOptionsNoopForStandardOptions(t *testing.T) {
	current := make([]detailedProjectOption, 0, len(standardClassOptions))
	for index, option := range standardClassOptions {
		current = append(current, detailedProjectOption{ID: string(rune('a' + index)), Name: option.Name, Color: option.Color})
	}
	got, changed, err := reconcileProjectOptions(current, standardClassOptions)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("changed = true, got %+v", got)
	}
	if !reflect.DeepEqual(got, current) {
		t.Fatalf("got %+v, want %+v", got, current)
	}
}

func TestReconcileProjectOptionsRejectsDuplicateStandardMeaning(t *testing.T) {
	current := []detailedProjectOption{
		{ID: "p0", Name: "P0", Color: "RED"},
		{ID: "urgent", Name: "Urgent", Color: "RED"},
	}
	if _, _, err := reconcileProjectOptions(current, standardPriorityOptions); err == nil {
		t.Fatal("expected duplicate standard/legacy option error")
	}
}

func TestReconcileOrganizationPriorityPreservesIDs(t *testing.T) {
	current := []organizationIssueFieldOptionDefinition{
		{ID: 11, Name: "Urgent", Color: "red", Priority: 1},
		{ID: 12, Name: "High", Color: "orange", Priority: 2},
		{ID: 13, Name: "Medium", Color: "yellow", Priority: 3},
		{ID: 14, Name: "Low", Color: "purple", Priority: 4},
	}
	got, changed, err := reconcileOrganizationPriorityOptions(current)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false, want true for legacy names")
	}
	for index, name := range []string{"P0", "P1", "P2", "P3"} {
		if got[index].ID != 11+index || got[index].Name != name {
			t.Fatalf("option %d = %+v", index, got[index])
		}
	}
}

func TestStandardPalettes(t *testing.T) {
	classes := map[string]string{}
	for _, option := range standardClassOptions {
		classes[option.Name] = option.Color
	}
	for name, colour := range map[string]string{
		"Task": "GRAY", "Bug": "RED", "Enhancement": "GREEN", "Data": "PINK",
		"Analysis": "PURPLE", "Deliverable": "ORANGE", "Documentation": "YELLOW", "Epic": "BLUE",
	} {
		if classes[name] != colour {
			t.Fatalf("Class %s colour = %q, want %q", name, classes[name], colour)
		}
	}
	for index, colour := range []string{"RED", "ORANGE", "YELLOW", "PURPLE"} {
		if standardPriorityOptions[index].Color != colour {
			t.Fatalf("Priority %d colour = %q, want %q", index, standardPriorityOptions[index].Color, colour)
		}
	}
}
