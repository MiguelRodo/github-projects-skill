package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

// seqStep is one expected gh call. want must appear in the joined arguments
// plus request body; the call returns out or err. Calls are strictly ordered,
// so an unexpected extra call (for example owner rediscovery) fails the test.
type seqStep struct {
	want string
	out  string
	err  error
}

type seqRunner struct {
	t     *testing.T
	steps []seqStep
	calls int
}

func (r *seqRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.t.Helper()
	return r.next(nil, args)
}

func (r *seqRunner) RunInput(_ context.Context, input []byte, args ...string) ([]byte, error) {
	r.t.Helper()
	return r.next(input, args)
}

func (r *seqRunner) next(input []byte, args []string) ([]byte, error) {
	r.t.Helper()
	joined := strings.Join(args, " ") + " " + string(input)
	if r.calls >= len(r.steps) {
		r.t.Fatalf("unexpected call %d: %s", r.calls+1, joined)
	}
	step := r.steps[r.calls]
	r.calls++
	if !strings.Contains(joined, step.want) {
		r.t.Fatalf("call %d = %s, want it to contain %q", r.calls, joined, step.want)
	}
	return []byte(step.out), step.err
}

func (r *seqRunner) done() {
	r.t.Helper()
	if r.calls != len(r.steps) {
		r.t.Fatalf("made %d calls, want %d", r.calls, len(r.steps))
	}
}

type schemaFieldFixture struct {
	Typename     string                  `json:"__typename"`
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	DataType     string                  `json:"dataType"`
	IsIssueField bool                    `json:"isIssueField"`
	Options      []detailedProjectOption `json:"options,omitempty"`
}

func singleSelectFixture(id, name string, options ...detailedProjectOption) schemaFieldFixture {
	return schemaFieldFixture{Typename: "ProjectV2SingleSelectField", ID: id, Name: name, DataType: "SINGLE_SELECT", Options: options}
}

func dateFixture(id, name string) schemaFieldFixture {
	return schemaFieldFixture{Typename: "ProjectV2Field", ID: id, Name: name, DataType: "DATE"}
}

func schemaJSON(t *testing.T, root string, project contract.Project, fields ...schemaFieldFixture) string {
	t.Helper()
	data := map[string]any{
		"id": "project-node", "number": project.Number, "title": project.Title,
		"fields": map[string]any{"nodes": fields, "pageInfo": map[string]any{"hasNextPage": false}},
	}
	encoded, err := json.Marshal(map[string]any{"data": map[string]any{root: map[string]any{"projectV2": data}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func standardClassFixture() schemaFieldFixture {
	options := make([]detailedProjectOption, 0, len(standardClassOptions))
	for index, option := range standardClassOptions {
		options = append(options, detailedProjectOption{ID: fmt.Sprintf("c%d", index), Name: option.Name, Color: option.Color})
	}
	return singleSelectFixture("class-field", "Class", options...)
}

func priorityFixture(names []string, ids []string) schemaFieldFixture {
	options := make([]detailedProjectOption, 0, len(names))
	for index, name := range names {
		options = append(options, detailedProjectOption{ID: ids[index], Name: name, Color: standardPriorityOptions[index].Color})
	}
	return singleSelectFixture("priority-field", "Priority", options...)
}

func userSetupProject() contract.Project {
	project := testProject()
	project.OwnerType = "user"
	return project
}

func userSchemaWithPriority(t *testing.T, names, ids []string) string {
	project := userSetupProject()
	return schemaJSON(t, "user", project,
		singleSelectFixture("status-field", "Status"),
		dateFixture("due-field", "Due date"),
		dateFixture("target-field", "Target date"),
		standardClassFixture(),
		priorityFixture(names, ids),
	)
}

var legacyPriorityNames = []string{"Urgent", "High", "Medium", "Low"}
var standardPriorityNames = []string{"P0", "P1", "P2", "P3"}
var originalPriorityIDs = []string{"p0", "p1", "p2", "p3"}

func TestApplyStandardProjectSetupVerifiesKeptOptionIDsAndDescribesRenames(t *testing.T) {
	runner := &seqRunner{t: t, steps: []seqStep{
		{want: "projectV2(number", out: userSchemaWithPriority(t, legacyPriorityNames, originalPriorityIDs)},
		{want: `"id":"p1","name":"P1"`, out: `{}`},
		{want: "projectV2(number", out: userSchemaWithPriority(t, standardPriorityNames, originalPriorityIDs)},
	}}
	result, err := ApplyStandardProjectSetup(context.Background(), runner, userSetupProject(), false)
	if err != nil {
		t.Fatal(err)
	}
	runner.done()
	if !result.Verified || len(result.Applied) != 1 || result.Applied[0].Action != "reconcile_options" {
		t.Fatalf("result = %#v", result)
	}
	if detail := result.Applied[0].Detail; !strings.Contains(detail, "rename High->P1") || !strings.Contains(detail, "IDs kept") {
		t.Fatalf("detail = %q, want the actual renames", detail)
	}
}

func TestApplyStandardProjectSetupFailsWhenOptionIDsAreRegenerated(t *testing.T) {
	runner := &seqRunner{t: t, steps: []seqStep{
		{want: "projectV2(number", out: userSchemaWithPriority(t, legacyPriorityNames, originalPriorityIDs)},
		{want: "updateProjectV2Field", out: `{}`},
		{want: "projectV2(number", out: userSchemaWithPriority(t, standardPriorityNames, []string{"n0", "n1", "n2", "n3"})},
	}}
	_, err := ApplyStandardProjectSetup(context.Background(), runner, userSetupProject(), false)
	if err == nil {
		t.Fatal("expected option-ID loss to fail verification")
	}
	for _, want := range []string{"option IDs p0, p1, p2, p3 disappeared", "applied: project:reconcile_options:Priority", "failed at readback"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	runner.done()
}

func TestApplyStandardProjectSetupReportsPartialFailure(t *testing.T) {
	project := userSetupProject()
	schema := schemaJSON(t, "user", project,
		singleSelectFixture("status-field", "Status"),
		standardClassFixture(),
		priorityFixture(standardPriorityNames, originalPriorityIDs),
	)
	runner := &seqRunner{t: t, steps: []seqStep{
		{want: "projectV2(number", out: schema},
		{want: `"name":"Due date"`, out: `{}`},
		{want: `"name":"Target date"`, err: errors.New("HTTP 502")},
	}}
	result, err := ApplyStandardProjectSetup(context.Background(), runner, project, false)
	if err == nil {
		t.Fatal("expected failure")
	}
	runner.done()
	for _, want := range []string{"applied: project:create_field:Due date", "failed at project:create_field:Target date", "HTTP 502", "run the plan again"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	if len(result.Applied) != 1 || result.Applied[0].Name != "Due date" {
		t.Fatalf("applied = %#v", result.Applied)
	}
}

func orgSetupProject() contract.Project {
	return contract.Project{Owner: "octo-org", Number: 12, Title: "Planning"}
}

func orgIssueTypeSteps(t *testing.T) []seqStep {
	t.Helper()
	var list []map[string]string
	var nodes []map[string]any
	for index, option := range standardClassOptions {
		id := fmt.Sprintf("it%d", index)
		list = append(list, map[string]string{"node_id": id, "name": option.Name})
		nodes = append(nodes, map[string]any{"id": id, "name": option.Name, "color": option.Color, "isEnabled": true})
	}
	listJSON, _ := json.Marshal([][]map[string]string{list})
	nodesJSON, _ := json.Marshal(map[string]any{"data": map[string]any{"organization": map[string]any{"id": "org-node"}, "nodes": nodes}})
	return []seqStep{
		{want: "orgs/octo-org/issue-types", out: string(listJSON)},
		{want: "nodes(ids", out: string(nodesJSON)},
	}
}

func orgPriorityFieldsJSON(ids []int, names []string) string {
	options := make([]string, 0, len(ids))
	for index, id := range ids {
		options = append(options, fmt.Sprintf(`{"id":%d,"name":%q,"color":%q,"priority":%d}`, id, names[index], strings.ToLower(standardPriorityOptions[index].Color), index+1))
	}
	return fmt.Sprintf(`[[{"id":7,"node_id":"IF_7","name":"Priority","data_type":"single_select","options":[%s]}]]`, strings.Join(options, ","))
}

func orgSchema(t *testing.T) string {
	issuePriority := singleSelectFixture("priority-field", "Priority")
	issuePriority.IsIssueField = true
	return schemaJSON(t, "organization", orgSetupProject(),
		singleSelectFixture("status-field", "Status"),
		dateFixture("due-field", "Due date"),
		dateFixture("target-field", "Target date"),
		issuePriority,
	)
}

func TestApplyStandardProjectSetupRefusalNamesOrganizationSchemaFlag(t *testing.T) {
	steps := []seqStep{{want: "api users/octo-org", out: "Organization\n"}, {want: "organization(login", out: orgSchema(t)}}
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps, seqStep{want: "orgs/octo-org/issue-fields", out: orgPriorityFieldsJSON([]int{11, 12, 13, 14}, legacyPriorityNames)})
	runner := &seqRunner{t: t, steps: steps}
	_, err := ApplyStandardProjectSetup(context.Background(), runner, orgSetupProject(), false)
	if err == nil || !strings.Contains(err.Error(), "--allow-organization-schema") {
		t.Fatalf("error = %v, want it to name --allow-organization-schema", err)
	}
	runner.done()
}

func TestApplyStandardProjectSetupFailsWhenOrganizationPriorityOptionIDsChange(t *testing.T) {
	// Owner type is discovered once; the readback must reuse it.
	steps := []seqStep{{want: "api users/octo-org", out: "Organization\n"}, {want: "organization(login", out: orgSchema(t)}}
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps,
		seqStep{want: "orgs/octo-org/issue-fields?per_page", out: orgPriorityFieldsJSON([]int{11, 12, 13, 14}, legacyPriorityNames)},
		seqStep{want: "--method PATCH", out: `{}`},
		seqStep{want: "organization(login", out: orgSchema(t)},
	)
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps, seqStep{want: "orgs/octo-org/issue-fields?per_page", out: orgPriorityFieldsJSON([]int{21, 22, 23, 24}, standardPriorityNames)})
	runner := &seqRunner{t: t, steps: steps}
	_, err := ApplyStandardProjectSetup(context.Background(), runner, orgSetupProject(), true)
	if err == nil || !strings.Contains(err.Error(), "organization Priority issue field option IDs 11, 12, 13, 14 disappeared") {
		t.Fatalf("error = %v, want organization option-ID loss", err)
	}
	runner.done()
}

func TestPlanStandardProjectSetupRejectsProjectLocalPriorityOnOrganizationWithRemedy(t *testing.T) {
	state := setupState{ownerType: "organization", priorityField: &organizationIssueFieldDefinition{ID: 7, Name: "Priority", DataType: "single_select"}}
	state.project.Fields = map[string]detailedProjectField{
		"due date":    {Name: "Due date", DataType: "DATE"},
		"target date": {Name: "Target date", DataType: "DATE"},
		"priority":    {Typename: "ProjectV2SingleSelectField", Name: "Priority", DataType: "SINGLE_SELECT"},
	}
	for index, option := range standardClassOptions {
		state.issueTypes = append(state.issueTypes, organizationIssueTypeDefinition{NodeID: fmt.Sprint(index), Name: option.Name, Color: option.Color, Enabled: true})
	}
	for index, option := range standardPriorityOptions {
		state.priorityField.Options = append(state.priorityField.Options, organizationIssueFieldOptionDefinition{ID: index + 1, Name: option.Name, Color: strings.ToLower(option.Color)})
	}
	_, err := planStandardProjectSetupFromState(orgSetupProject(), state)
	if err == nil || !strings.Contains(err.Error(), "rename or remove that Project field") || !strings.Contains(err.Error(), "will not migrate") {
		t.Fatalf("error = %v, want an actionable remedy", err)
	}
}

func TestPlanStandardProjectSetupDescribesIssueTypeAndPriorityDiffs(t *testing.T) {
	state := setupState{ownerType: "organization", priorityField: &organizationIssueFieldDefinition{ID: 7, Name: "Priority", DataType: "single_select"}}
	issuePriority := detailedProjectField{Typename: "ProjectV2SingleSelectField", Name: "Priority", DataType: "SINGLE_SELECT", IsIssueField: true}
	state.project.Fields = map[string]detailedProjectField{
		"due date": {Name: "Due date", DataType: "DATE"}, "target date": {Name: "Target date", DataType: "DATE"}, "priority": issuePriority,
	}
	for index, option := range standardClassOptions {
		definition := organizationIssueTypeDefinition{NodeID: fmt.Sprint(index), Name: option.Name, Color: option.Color, Enabled: true}
		if option.Name == "Bug" {
			definition.Color, definition.Enabled = "GRAY", false
		}
		state.issueTypes = append(state.issueTypes, definition)
	}
	state.priorityField.Options = []organizationIssueFieldOptionDefinition{
		{ID: 1, Name: "Urgent", Color: "red"}, {ID: 2, Name: "High", Color: "gray"}, {ID: 3, Name: "P2", Color: "yellow"},
	}
	plan, err := planStandardProjectSetupFromState(orgSetupProject(), state)
	if err != nil {
		t.Fatal(err)
	}
	details := map[string]string{}
	for _, change := range plan.Changes {
		details[change.Action] = change.Detail
	}
	for action, wants := range map[string][]string{
		"reconcile_issue_type":  {"recolour GRAY->RED", "re-enable"},
		"reconcile_issue_field": {"rename Urgent->P0", "rename High->P1", "recolour P1 GRAY->ORANGE", "add P3 (PURPLE)"},
	} {
		for _, want := range wants {
			if !strings.Contains(details[action], want) {
				t.Fatalf("%s detail = %q, want %q", action, details[action], want)
			}
		}
	}
}

func TestDescribeOptionChangesReportsReordering(t *testing.T) {
	current := []detailedProjectOption{{ID: "b", Name: "P1", Color: "ORANGE"}, {ID: "a", Name: "P0", Color: "RED"}}
	reconciled, changed, err := reconcileProjectOptions(current, standardPriorityOptions[:2])
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	if detail := describeOptionChanges(current, reconciled); !strings.Contains(detail, "reorder to P0, P1") {
		t.Fatalf("detail = %q", detail)
	}
}

func TestEmptySetupAndViewListsEncodeAsArrays(t *testing.T) {
	state := setupState{ownerType: "user"}
	state.project.Fields = map[string]detailedProjectField{
		"due date": {Name: "Due date", DataType: "DATE"}, "target date": {Name: "Target date", DataType: "DATE"},
		"class":    {Typename: "ProjectV2SingleSelectField", Name: "Class", DataType: "SINGLE_SELECT", Options: standardClassFixture().Options},
		"priority": {Typename: "ProjectV2SingleSelectField", Name: "Priority", DataType: "SINGLE_SELECT", Options: priorityFixture(standardPriorityNames, originalPriorityIDs).Options},
	}
	plan, err := planStandardProjectSetupFromState(userSetupProject(), state)
	if err != nil {
		t.Fatal(err)
	}
	viewPlan, err := planStandardBacklogViewFromState(userSetupProject(), backlogViewState{ownerType: "user", spec: backlogTestSpec(), views: []projectViewNode{standardBacklogTestView("v1", 1)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{plan, viewPlan, StandardProjectSetupResult{Applied: []StandardProjectSetupChange{}}} {
		encoded, _ := json.Marshal(value)
		if strings.Contains(string(encoded), "null") {
			t.Fatalf("encoded = %s, want empty arrays rather than null", encoded)
		}
	}
}
