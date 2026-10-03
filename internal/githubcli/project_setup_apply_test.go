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

// seqClient asserts the complete query, typed variables or REST request in order.
type seqStep struct {
	request request
	out     string
	err     error
}
type seqClient struct {
	t     *testing.T
	steps []seqStep
	calls int
}

func (r *seqClient) next(actual request) ([]byte, error) {
	r.t.Helper()
	if r.calls >= len(r.steps) {
		r.t.Fatalf("unexpected request: %s", canonicalRequest(actual))
	}
	step := r.steps[r.calls]
	r.calls++
	if canonicalRequest(actual) != canonicalRequest(step.request) {
		r.t.Fatalf("request %d = %s, want %s", r.calls, canonicalRequest(actual), canonicalRequest(step.request))
	}
	return []byte(step.out), step.err
}
func (r *seqClient) GraphQL(_ context.Context, query string, variables map[string]any) (GraphQLResponse, error) {
	output, err := r.next(request{query: query, variables: variables})
	if err != nil {
		return GraphQLResponse{}, err
	}
	var response GraphQLResponse
	err = json.Unmarshal(output, &response)
	return response, err
}
func (r *seqClient) REST(_ context.Context, method, path string, body any) (RESTResponse, error) {
	output, err := r.next(request{method: method, path: path, body: body})
	return RESTResponse{Status: 200, Body: output}, err
}
func (r *seqClient) done() {
	r.t.Helper()
	if r.calls != len(r.steps) {
		r.t.Fatalf("made %d requests, want %d", r.calls, len(r.steps))
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
	runner := &seqClient{t: t, steps: []seqStep{
		{request: detailedSchemaRequest("user"), out: userSchemaWithPriority(t, legacyPriorityNames, originalPriorityIDs)},
		{request: priorityOptionsRequest(), out: `{}`},
		{request: detailedSchemaRequest("user"), out: userSchemaWithPriority(t, standardPriorityNames, originalPriorityIDs)},
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
	runner := &seqClient{t: t, steps: []seqStep{
		{request: detailedSchemaRequest("user"), out: userSchemaWithPriority(t, legacyPriorityNames, originalPriorityIDs)},
		{request: priorityOptionsRequest(), out: `{}`},
		{request: detailedSchemaRequest("user"), out: userSchemaWithPriority(t, standardPriorityNames, []string{"n0", "n1", "n2", "n3"})},
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
	runner := &seqClient{t: t, steps: []seqStep{
		{request: detailedSchemaRequest("user"), out: schema},
		{request: createDateRequest("Due date"), out: `{}`},
		{request: createDateRequest("Target date"), err: errors.New("HTTP 502")},
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
	listJSON, _ := json.Marshal(list)
	nodesJSON, _ := json.Marshal(map[string]any{"data": map[string]any{"organization": map[string]any{"id": "org-node"}, "nodes": nodes}})
	return []seqStep{
		{request: request{method: "GET", path: "/orgs/octo-org/issue-types?per_page=100"}, out: string(listJSON)},
		{request: issueTypesRequest(), out: string(nodesJSON)},
	}
}

func orgPriorityFieldsJSON(ids []int, names []string) string {
	options := make([]string, 0, len(ids))
	for index, id := range ids {
		options = append(options, fmt.Sprintf(`{"id":%d,"name":%q,"color":%q,"priority":%d}`, id, names[index], strings.ToLower(standardPriorityOptions[index].Color), index+1))
	}
	return fmt.Sprintf(`[{"id":7,"node_id":"IF_7","name":"Priority","data_type":"single_select","options":[%s]}]`, strings.Join(options, ","))
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
	steps := []seqStep{{request: request{method: "GET", path: "/users/octo-org"}, out: `{"type":"Organization"}`}, {request: detailedSchemaRequest("organization"), out: orgSchema(t)}}
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps, seqStep{request: request{method: "GET", path: "/orgs/octo-org/issue-fields?per_page=100"}, out: orgPriorityFieldsJSON([]int{11, 12, 13, 14}, legacyPriorityNames)})
	runner := &seqClient{t: t, steps: steps}
	_, err := ApplyStandardProjectSetup(context.Background(), runner, orgSetupProject(), false)
	if err == nil || !strings.Contains(err.Error(), "--allow-organization-schema") {
		t.Fatalf("error = %v, want it to name --allow-organization-schema", err)
	}
	runner.done()
}

func TestApplyStandardProjectSetupFailsWhenOrganizationPriorityOptionIDsChange(t *testing.T) {
	// Owner type is discovered once; the readback must reuse it.
	steps := []seqStep{{request: request{method: "GET", path: "/users/octo-org"}, out: `{"type":"Organization"}`}, {request: detailedSchemaRequest("organization"), out: orgSchema(t)}}
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps,
		seqStep{request: request{method: "GET", path: "/orgs/octo-org/issue-fields?per_page=100"}, out: orgPriorityFieldsJSON([]int{11, 12, 13, 14}, legacyPriorityNames)},
		seqStep{request: organizationPriorityRequest(), out: `{}`},
		seqStep{request: detailedSchemaRequest("organization"), out: orgSchema(t)},
	)
	steps = append(steps, orgIssueTypeSteps(t)...)
	steps = append(steps, seqStep{request: request{method: "GET", path: "/orgs/octo-org/issue-fields?per_page=100"}, out: orgPriorityFieldsJSON([]int{21, 22, 23, 24}, standardPriorityNames)})
	runner := &seqClient{t: t, steps: steps}
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

func detailedSchemaRequest(root string) request {
	login, number := "octo-user", 40
	if root == "organization" {
		login, number = "octo-org", 12
	}
	return request{query: fmt.Sprintf(`query($login: String!, $number: Int!) {
  %s(login: $login) {
    projectV2(number: $number) {
      id
      number
      title
      fields(first: 100) {
        nodes {
          __typename
          ... on ProjectV2FieldCommon { id name dataType isIssueField }
          ... on ProjectV2SingleSelectField { options { id name color description } }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, root), variables: map[string]any{"login": login, "number": number}}
}
func viewsRequest() request {
	return request{query: fmt.Sprintf(`query($login: String!, $number: Int!) {
  %s(login: $login) {
    projectV2(number: $number) {
      number title
      views(first: 100) {
        nodes {
          id number name layout filter
          configuration {
            visibleFields(first: 100) {
              nodes { ... on ProjectV2FieldCommon { id name } }
              pageInfo { hasNextPage }
            }
          }
          groupByFields(first: 10) {
            nodes { ... on ProjectV2FieldCommon { id name } }
            pageInfo { hasNextPage }
          }
          verticalGroupByFields(first: 10) {
            nodes { ... on ProjectV2FieldCommon { id name } }
            pageInfo { hasNextPage }
          }
          sortByFields(first: 10) {
            nodes { direction field { ... on ProjectV2FieldCommon { id name } } }
            pageInfo { hasNextPage }
          }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, "user"), variables: map[string]any{"login": "octo-user", "number": 40}}
}
func priorityOptionsRequest() request {
	options := []map[string]any{}
	for i, opt := range standardPriorityOptions {
		options = append(options, map[string]any{"id": fmt.Sprintf("p%d", i), "name": opt.Name, "color": opt.Color, "description": ""})
	}
	return request{query: `mutation($fieldId: ID!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
  updateProjectV2Field(input: {fieldId: $fieldId, singleSelectOptions: $options}) {
    projectV2Field { ... on ProjectV2SingleSelectField { id name options { id name color description } } }
  }
}`, variables: map[string]any{"fieldId": "priority-field", "options": options}}
}
func createDateRequest(name string) request {
	return request{query: `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {
  createProjectV2Field(input: {projectId: $projectId, name: $name, dataType: $dataType}) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`, variables: map[string]any{"projectId": "project-node", "name": name, "dataType": "DATE"}}
}
func issueTypesRequest() request {
	ids := []string{}
	for i := range standardClassOptions {
		ids = append(ids, fmt.Sprintf("it%d", i))
	}
	return request{query: `query($login: String!, $ids: [ID!]!) {
  organization(login: $login) { id }
  nodes(ids: $ids) {
    ... on IssueType { id name description color isEnabled }
  }
}`, variables: map[string]any{"login": "octo-org", "ids": ids}}
}
func organizationPriorityRequest() request {
	options := []map[string]any{}
	for i, opt := range standardPriorityOptions {
		options = append(options, map[string]any{"id": 11 + i, "name": opt.Name, "color": strings.ToLower(opt.Color), "description": "", "priority": i + 1})
	}
	return request{method: "PATCH", path: "/orgs/octo-org/issue-fields/7", body: map[string]any{"name": "Priority", "description": "", "options": options}}
}
func createViewRequest() request {
	return request{method: "POST", path: "/users/octo-user/projectsV2/40/views", body: map[string]any{"name": "Backlog", "layout": "table", "filter": "", "visible_fields": []int{1, 2, 3, 4}, "sort_by": []any{[]any{3, "asc"}}, "group_by": []int{2}}}
}
func deleteViewRequest(id string) request {
	return request{query: `mutation($viewId: ID!) {
  deleteProjectV2View(input: {viewId: $viewId}) { projectV2View { id } }
}`, variables: map[string]any{"viewId": id}}
}
func updateViewRequest() request {
	return request{query: `mutation($input: UpdateProjectV2ViewInput!) {
  updateProjectV2View(input: $input) { projectV2View { id name layout filter } }
}`, variables: map[string]any{"input": map[string]any{"viewId": "v1", "name": "Backlog", "layout": "TABLE_LAYOUT", "filter": "", "configuration": map[string]any{"visibleFieldIds": []string{"title", "status", "priority", "class"}}}}}
}
