package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

// StandardProjectSetupChange is one bounded schema change proposed by the
// standard Project setup operation.
type StandardProjectSetupChange struct {
	Scope  string `json:"scope"`
	Action string `json:"action"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// StandardProjectSetupPlan describes the current delta without mutating GitHub.
type StandardProjectSetupPlan struct {
	Project                    ProjectIdentity              `json:"project"`
	OwnerType                  string                       `json:"ownerType"`
	Changes                    []StandardProjectSetupChange `json:"changes"`
	RequiresOrganizationSchema bool                         `json:"requiresOrganizationSchema"`
}

// StandardProjectSetupResult is returned after an independently verified apply.
type StandardProjectSetupResult struct {
	Project   ProjectIdentity              `json:"project"`
	OwnerType string                       `json:"ownerType"`
	Applied   []StandardProjectSetupChange `json:"applied"`
	Verified  bool                         `json:"verified"`
}

type standardOption struct {
	Name        string
	Color       string
	LegacyNames []string
}

var standardClassOptions = []standardOption{
	{Name: "Task", Color: "GRAY"},
	{Name: "Bug", Color: "RED"},
	{Name: "Enhancement", Color: "GREEN"},
	{Name: "Data", Color: "PINK"},
	{Name: "Analysis", Color: "PURPLE"},
	{Name: "Deliverable", Color: "ORANGE"},
	{Name: "Documentation", Color: "YELLOW"},
	{Name: "Epic", Color: "BLUE"},
}

var standardPriorityOptions = []standardOption{
	{Name: "P0", Color: "RED", LegacyNames: []string{"Urgent"}},
	{Name: "P1", Color: "ORANGE", LegacyNames: []string{"High"}},
	{Name: "P2", Color: "YELLOW", LegacyNames: []string{"Medium"}},
	{Name: "P3", Color: "PURPLE", LegacyNames: []string{"Low"}},
}

type detailedProjectOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type detailedProjectField struct {
	Typename string
	ID       string
	Name     string
	DataType string
	Options  []detailedProjectOption
}

type detailedProjectSchema struct {
	ID     string
	Number int
	Title  string
	Fields map[string]detailedProjectField
}

type detailedProjectFieldNode struct {
	Typename string `json:"__typename"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Options  []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	} `json:"options"`
}

type detailedProjectData struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Fields struct {
		Nodes    []detailedProjectFieldNode `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"fields"`
}

type detailedProjectResponse struct {
	Data struct {
		User struct {
			ProjectV2 *detailedProjectData `json:"projectV2"`
		} `json:"user"`
		Organization struct {
			ProjectV2 *detailedProjectData `json:"projectV2"`
		} `json:"organization"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func queryDetailedProjectSchema(ctx context.Context, runner Runner, project contract.Project, ownerType string) (detailedProjectSchema, error) {
	query := fmt.Sprintf(`query($login: String!, $number: Int!) {
  %s(login: $login) {
    projectV2(number: $number) {
      id
      number
      title
      fields(first: 100) {
        nodes {
          __typename
          ... on ProjectV2FieldCommon { id name dataType }
          ... on ProjectV2SingleSelectField { options { id name color description } }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, ownerType)
	out, err := runner.Run(ctx, "api", "graphql", "-f", "query="+query, "-f", "login="+project.Owner, "-F", "number="+strconv.Itoa(project.Number))
	if err != nil {
		return detailedProjectSchema{}, fmt.Errorf("query Project schema: %w", err)
	}
	var resp detailedProjectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return detailedProjectSchema{}, fmt.Errorf("decode Project schema: %w", err)
	}
	if len(resp.Errors) > 0 {
		return detailedProjectSchema{}, fmt.Errorf("GraphQL error querying Project schema: %s", resp.Errors[0].Message)
	}
	var data *detailedProjectData
	if ownerType == "organization" {
		data = resp.Data.Organization.ProjectV2
	} else {
		data = resp.Data.User.ProjectV2
	}
	if data == nil {
		return detailedProjectSchema{}, fmt.Errorf("Project %s/%d not found at %s root", project.Owner, project.Number, ownerType)
	}
	if data.Number != project.Number || data.Title != project.Title {
		return detailedProjectSchema{}, fmt.Errorf("Project identity changed: got %s/%d %q, expected %s/%d %q", project.Owner, data.Number, data.Title, project.Owner, project.Number, project.Title)
	}
	if data.Fields.PageInfo.HasNextPage {
		return detailedProjectSchema{}, fmt.Errorf("Project %s/%d has more than 100 fields; refusing an incomplete setup read", project.Owner, project.Number)
	}
	schema := detailedProjectSchema{ID: data.ID, Number: data.Number, Title: data.Title, Fields: make(map[string]detailedProjectField)}
	for _, node := range data.Fields.Nodes {
		field := detailedProjectField{Typename: node.Typename, ID: node.ID, Name: node.Name, DataType: node.DataType}
		for _, option := range node.Options {
			field.Options = append(field.Options, detailedProjectOption{ID: option.ID, Name: option.Name, Color: strings.ToUpper(option.Color), Description: option.Description})
		}
		key := strings.ToLower(strings.TrimSpace(node.Name))
		if _, exists := schema.Fields[key]; exists {
			return detailedProjectSchema{}, fmt.Errorf("Project %s/%d has more than one field named %q", project.Owner, project.Number, node.Name)
		}
		schema.Fields[key] = field
	}
	return schema, nil
}

type organizationIssueTypeDefinition struct {
	ID          int    `json:"id"`
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Enabled     bool   `json:"is_enabled"`
}

type organizationIssueFieldOptionDefinition struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Priority    int    `json:"priority"`
}

type organizationIssueFieldDefinition struct {
	ID          int                                      `json:"id"`
	NodeID      string                                   `json:"node_id"`
	Name        string                                   `json:"name"`
	Description string                                   `json:"description"`
	DataType    string                                   `json:"data_type"`
	Options     []organizationIssueFieldOptionDefinition `json:"options"`
}

func queryOrganizationIssueTypes(ctx context.Context, runner Runner, owner string) ([]organizationIssueTypeDefinition, error) {
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, "orgs/"+owner+"/issue-types?per_page=100")
	out, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list organization issue types for %s: %w", owner, err)
	}
	var pages [][]organizationIssueTypeDefinition
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("decode organization issue types: %w", err)
	}
	var result []organizationIssueTypeDefinition
	for _, page := range pages {
		result = append(result, page...)
	}
	return result, nil
}

func queryOrganizationIssueFieldDefinitions(ctx context.Context, runner Runner, owner string) ([]organizationIssueFieldDefinition, error) {
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, "orgs/"+owner+"/issue-fields?per_page=100")
	out, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list organization issue fields for %s: %w", owner, err)
	}
	var pages [][]organizationIssueFieldDefinition
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("decode organization issue fields: %w", err)
	}
	var result []organizationIssueFieldDefinition
	for _, page := range pages {
		result = append(result, page...)
	}
	return result, nil
}

type setupState struct {
	ownerType     string
	project       detailedProjectSchema
	issueTypes    []organizationIssueTypeDefinition
	issueFields   []organizationIssueFieldDefinition
	priorityField *organizationIssueFieldDefinition
}

func inspectStandardProjectSetup(ctx context.Context, runner Runner, project contract.Project) (setupState, error) {
	ownerType := project.OwnerType
	if ownerType == "" {
		var err error
		ownerType, err = discoverOwnerType(ctx, runner, project.Owner)
		if err != nil {
			return setupState{}, fmt.Errorf("discover Project owner type: %w", err)
		}
	}
	if ownerType != "user" && ownerType != "organization" {
		return setupState{}, fmt.Errorf("unsupported Project owner type %q", ownerType)
	}
	projectSchema, err := queryDetailedProjectSchema(ctx, runner, project, ownerType)
	if err != nil {
		return setupState{}, err
	}
	status, ok := projectSchema.Fields["status"]
	if !ok || status.DataType != "SINGLE_SELECT" {
		return setupState{}, fmt.Errorf("Project %s/%d does not expose the standard Status single-select field", project.Owner, project.Number)
	}
	state := setupState{ownerType: ownerType, project: projectSchema}
	if ownerType == "organization" {
		state.issueTypes, err = queryOrganizationIssueTypes(ctx, runner, project.Owner)
		if err != nil {
			return setupState{}, err
		}
		state.issueFields, err = queryOrganizationIssueFieldDefinitions(ctx, runner, project.Owner)
		if err != nil {
			return setupState{}, err
		}
		for i := range state.issueFields {
			if strings.EqualFold(state.issueFields[i].Name, "Priority") {
				if state.priorityField != nil {
					return setupState{}, fmt.Errorf("organization %s has more than one issue field named Priority", project.Owner)
				}
				state.priorityField = &state.issueFields[i]
			}
		}
	}
	return state, nil
}

func standardOptionByCurrentName(name string, desired []standardOption) (standardOption, bool) {
	for _, option := range desired {
		if strings.EqualFold(option.Name, name) {
			return option, true
		}
	}
	for _, option := range desired {
		for _, legacy := range option.LegacyNames {
			if strings.EqualFold(legacy, name) {
				return option, true
			}
		}
	}
	return standardOption{}, false
}

func reconcileProjectOptions(current []detailedProjectOption, desired []standardOption) ([]detailedProjectOption, bool, error) {
	used := make(map[int]bool)
	result := make([]detailedProjectOption, 0, len(current)+len(desired))
	changed := false
	for _, target := range desired {
		exact := -1
		legacy := -1
		for i, existing := range current {
			if used[i] {
				continue
			}
			if strings.EqualFold(existing.Name, target.Name) {
				exact = i
				break
			}
			for _, old := range target.LegacyNames {
				if strings.EqualFold(existing.Name, old) {
					legacy = i
				}
			}
		}
		index := exact
		if index < 0 {
			index = legacy
		}
		if index < 0 {
			result = append(result, detailedProjectOption{Name: target.Name, Color: target.Color})
			changed = true
			continue
		}
		used[index] = true
		option := current[index]
		if option.Name != target.Name || strings.ToUpper(option.Color) != target.Color {
			option.Name = target.Name
			option.Color = target.Color
			changed = true
		}
		result = append(result, option)
	}
	for i, option := range current {
		if used[i] {
			continue
		}
		if _, standard := standardOptionByCurrentName(option.Name, desired); standard {
			return nil, false, fmt.Errorf("field contains duplicate standard/legacy option %q", option.Name)
		}
		result = append(result, option)
	}
	if !changed && !reflect.DeepEqual(optionIdentity(current), optionIdentity(result)) {
		changed = true
	}
	return result, changed, nil
}

func optionIdentity(options []detailedProjectOption) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.ID+"\x00"+option.Name+"\x00"+strings.ToUpper(option.Color)+"\x00"+option.Description)
	}
	return values
}

func reconcileOrganizationPriorityOptions(current []organizationIssueFieldOptionDefinition) ([]organizationIssueFieldOptionDefinition, bool, error) {
	projectCurrent := make([]detailedProjectOption, 0, len(current))
	for _, option := range current {
		projectCurrent = append(projectCurrent, detailedProjectOption{ID: strconv.Itoa(option.ID), Name: option.Name, Color: strings.ToUpper(option.Color), Description: option.Description})
	}
	reconciled, changed, err := reconcileProjectOptions(projectCurrent, standardPriorityOptions)
	if err != nil {
		return nil, false, err
	}
	result := make([]organizationIssueFieldOptionDefinition, 0, len(reconciled))
	for i, option := range reconciled {
		id := 0
		if option.ID != "" {
			id, err = strconv.Atoi(option.ID)
			if err != nil {
				return nil, false, fmt.Errorf("invalid organization option id %q", option.ID)
			}
		}
		result = append(result, organizationIssueFieldOptionDefinition{ID: id, Name: option.Name, Description: option.Description, Color: strings.ToLower(option.Color), Priority: i + 1})
	}
	for i := range current {
		if current[i].Priority != i+1 && current[i].Priority != 0 {
			changed = true
			break
		}
	}
	return result, changed, nil
}

func planProjectField(changes *[]StandardProjectSetupChange, state setupState, name, dataType string, options []standardOption, ownerType string) error {
	field, exists := state.project.Fields[strings.ToLower(name)]
	if !exists {
		*changes = append(*changes, StandardProjectSetupChange{Scope: "project", Action: "create_field", Name: name, Detail: dataType})
		return nil
	}
	if field.DataType != dataType {
		return fmt.Errorf("Project field %q has type %s, want %s", name, field.DataType, dataType)
	}
	if len(options) == 0 {
		return nil
	}
	if field.Typename != "ProjectV2SingleSelectField" {
		return fmt.Errorf("Project field %q is %s, want a project single-select field", name, field.Typename)
	}
	_, changed, err := reconcileProjectOptions(field.Options, options)
	if err != nil {
		return fmt.Errorf("reconcile Project field %q: %w", name, err)
	}
	if changed {
		*changes = append(*changes, StandardProjectSetupChange{Scope: "project", Action: "reconcile_options", Name: name})
	}
	return nil
}

// PlanStandardProjectSetup inspects live Project and organization schema and
// returns only the changes needed for the shared standard profile.
func PlanStandardProjectSetup(ctx context.Context, runner Runner, project contract.Project) (StandardProjectSetupPlan, error) {
	state, err := inspectStandardProjectSetup(ctx, runner, project)
	if err != nil {
		return StandardProjectSetupPlan{}, err
	}
	plan := StandardProjectSetupPlan{
		Project:   ProjectIdentity{Number: project.Number, Owner: project.Owner, Title: project.Title},
		OwnerType: state.ownerType,
	}
	if err := planProjectField(&plan.Changes, state, "Due date", "DATE", nil, state.ownerType); err != nil {
		return StandardProjectSetupPlan{}, err
	}
	if err := planProjectField(&plan.Changes, state, "Target date", "DATE", nil, state.ownerType); err != nil {
		return StandardProjectSetupPlan{}, err
	}
	if state.ownerType == "user" {
		if err := planProjectField(&plan.Changes, state, "Class", "SINGLE_SELECT", standardClassOptions, state.ownerType); err != nil {
			return StandardProjectSetupPlan{}, err
		}
		if err := planProjectField(&plan.Changes, state, "Priority", "SINGLE_SELECT", standardPriorityOptions, state.ownerType); err != nil {
			return StandardProjectSetupPlan{}, err
		}
		return plan, nil
	}

	seenTypes := make(map[string]organizationIssueTypeDefinition)
	for _, issueType := range state.issueTypes {
		key := strings.ToLower(issueType.Name)
		if _, exists := seenTypes[key]; exists {
			return StandardProjectSetupPlan{}, fmt.Errorf("organization %s has duplicate issue type name %q", project.Owner, issueType.Name)
		}
		seenTypes[key] = issueType
	}
	for _, desired := range standardClassOptions {
		current, exists := seenTypes[strings.ToLower(desired.Name)]
		if !exists {
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "create_issue_type", Name: desired.Name, Detail: desired.Color})
			plan.RequiresOrganizationSchema = true
			continue
		}
		if !current.Enabled || !strings.EqualFold(current.Color, desired.Color) {
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "reconcile_issue_type", Name: desired.Name, Detail: desired.Color})
			plan.RequiresOrganizationSchema = true
		}
	}
	if state.priorityField == nil {
		plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "create_issue_field", Name: "Priority", Detail: "P0,P1,P2,P3"})
		plan.RequiresOrganizationSchema = true
	} else {
		if state.priorityField.DataType != "single_select" {
			return StandardProjectSetupPlan{}, fmt.Errorf("organization issue field Priority has type %s, want single_select", state.priorityField.DataType)
		}
		_, changed, err := reconcileOrganizationPriorityOptions(state.priorityField.Options)
		if err != nil {
			return StandardProjectSetupPlan{}, fmt.Errorf("reconcile organization Priority: %w", err)
		}
		if changed {
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "reconcile_issue_field", Name: "Priority", Detail: "P0,P1,P2,P3"})
			plan.RequiresOrganizationSchema = true
		}
	}
	if field, exists := state.project.Fields["priority"]; !exists {
		plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "project", Action: "attach_issue_field", Name: "Priority"})
	} else if field.Typename != "ProjectV2IssueField" {
		return StandardProjectSetupPlan{}, fmt.Errorf("organization Project already has non-issue field %q named Priority", field.Typename)
	}
	return plan, nil
}

func runJSONInput(ctx context.Context, runner Runner, body any, args ...string) ([]byte, error) {
	stdinRunner, ok := runner.(inputRunner)
	if !ok {
		return nil, errors.New("GitHub runner does not support JSON request bodies")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode GitHub request: %w", err)
	}
	return stdinRunner.RunInput(ctx, encoded, args...)
}

func createProjectField(ctx context.Context, runner Runner, projectID, name, dataType string, options []standardOption) error {
	variables := map[string]any{"projectId": projectID, "name": name, "dataType": dataType}
	if len(options) > 0 {
		items := make([]map[string]any, 0, len(options))
		for _, option := range options {
			items = append(items, map[string]any{"name": option.Name, "color": option.Color, "description": ""})
		}
		variables["options"] = items
	}
	query := `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!, $options: [ProjectV2SingleSelectFieldOptionInput!]) {
  createProjectV2Field(input: {projectId: $projectId, name: $name, dataType: $dataType, singleSelectOptions: $options}) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`
	_, err := runJSONInput(ctx, runner, map[string]any{"query": query, "variables": variables}, "api", "graphql", "--input", "-")
	if err != nil {
		return fmt.Errorf("create Project field %q: %w", name, err)
	}
	return nil
}

func updateProjectSingleSelect(ctx context.Context, runner Runner, field detailedProjectField, desired []standardOption) error {
	options, changed, err := reconcileProjectOptions(field.Options, desired)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	payloadOptions := make([]map[string]any, 0, len(options))
	for _, option := range options {
		item := map[string]any{"name": option.Name, "color": strings.ToUpper(option.Color), "description": option.Description}
		if option.ID != "" {
			item["id"] = option.ID
		}
		payloadOptions = append(payloadOptions, item)
	}
	query := `mutation($fieldId: ID!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
  updateProjectV2Field(input: {fieldId: $fieldId, singleSelectOptions: $options}) {
    projectV2Field { ... on ProjectV2SingleSelectField { id name options { id name color description } } }
  }
}`
	_, err = runJSONInput(ctx, runner, map[string]any{"query": query, "variables": map[string]any{"fieldId": field.ID, "options": payloadOptions}}, "api", "graphql", "--input", "-")
	if err != nil {
		return fmt.Errorf("update Project field %q: %w", field.Name, err)
	}
	return nil
}

func createOrganizationIssueType(ctx context.Context, runner Runner, owner string, desired standardOption) error {
	body := map[string]any{"name": desired.Name, "is_enabled": true, "description": "", "color": strings.ToLower(desired.Color)}
	args := []string{"api", "--method", "POST"}
	args = append(args, apiHeaders()...)
	args = append(args, "orgs/"+owner+"/issue-types", "--input", "-")
	_, err := runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return fmt.Errorf("create organization issue type %q: %w", desired.Name, err)
	}
	return nil
}

func updateOrganizationIssueType(ctx context.Context, runner Runner, owner string, current organizationIssueTypeDefinition, desired standardOption) error {
	body := map[string]any{"name": desired.Name, "is_enabled": true, "description": current.Description, "color": strings.ToLower(desired.Color)}
	args := []string{"api", "--method", "PUT"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("orgs/%s/issue-types/%d", owner, current.ID), "--input", "-")
	_, err := runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return fmt.Errorf("update organization issue type %q: %w", desired.Name, err)
	}
	return nil
}

func organizationPriorityBody(options []organizationIssueFieldOptionDefinition, description string) map[string]any {
	payload := make([]map[string]any, 0, len(options))
	for _, option := range options {
		item := map[string]any{"name": option.Name, "description": option.Description, "color": strings.ToLower(option.Color), "priority": option.Priority}
		if option.ID != 0 {
			item["id"] = option.ID
		}
		payload = append(payload, item)
	}
	return map[string]any{"name": "Priority", "description": description, "options": payload}
}

func createOrganizationPriority(ctx context.Context, runner Runner, owner string) error {
	options := make([]organizationIssueFieldOptionDefinition, 0, len(standardPriorityOptions))
	for i, option := range standardPriorityOptions {
		options = append(options, organizationIssueFieldOptionDefinition{Name: option.Name, Color: strings.ToLower(option.Color), Priority: i + 1})
	}
	body := organizationPriorityBody(options, "Level of importance")
	body["data_type"] = "single_select"
	args := []string{"api", "--method", "POST"}
	args = append(args, apiHeaders()...)
	args = append(args, "orgs/"+owner+"/issue-fields", "--input", "-")
	_, err := runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return fmt.Errorf("create organization Priority issue field: %w", err)
	}
	return nil
}

func updateOrganizationPriority(ctx context.Context, runner Runner, owner string, field organizationIssueFieldDefinition) error {
	options, changed, err := reconcileOrganizationPriorityOptions(field.Options)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	body := organizationPriorityBody(options, field.Description)
	args := []string{"api", "--method", "PATCH"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("orgs/%s/issue-fields/%d", owner, field.ID), "--input", "-")
	_, err = runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return fmt.Errorf("update organization Priority issue field: %w", err)
	}
	return nil
}

func attachOrganizationIssueField(ctx context.Context, runner Runner, project contract.Project, field organizationIssueFieldDefinition) error {
	body := map[string]any{"issue_field_id": field.ID}
	args := []string{"api", "--method", "POST"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("orgs/%s/projectsV2/%d/fields", project.Owner, project.Number), "--input", "-")
	_, err := runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return fmt.Errorf("attach organization Priority issue field to Project: %w", err)
	}
	return nil
}

func applyProjectFieldChange(ctx context.Context, runner Runner, project contract.Project, state setupState, change StandardProjectSetupChange) error {
	switch change.Action {
	case "create_field":
		var options []standardOption
		if change.Name == "Class" {
			options = standardClassOptions
		} else if change.Name == "Priority" {
			options = standardPriorityOptions
		}
		return createProjectField(ctx, runner, state.project.ID, change.Name, change.Detail, options)
	case "reconcile_options":
		field := state.project.Fields[strings.ToLower(change.Name)]
		if change.Name == "Class" {
			return updateProjectSingleSelect(ctx, runner, field, standardClassOptions)
		}
		return updateProjectSingleSelect(ctx, runner, field, standardPriorityOptions)
	default:
		return fmt.Errorf("unsupported project field setup action %q", change.Action)
	}
}

// ApplyStandardProjectSetup performs the planned setup and then independently
// re-inspects the live schema. Organization-wide schema changes require the
// explicit allowOrganizationSchema flag before any mutation occurs.
func ApplyStandardProjectSetup(ctx context.Context, runner Runner, project contract.Project, allowOrganizationSchema bool) (StandardProjectSetupResult, error) {
	plan, err := PlanStandardProjectSetup(ctx, runner, project)
	if err != nil {
		return StandardProjectSetupResult{}, err
	}
	if plan.RequiresOrganizationSchema && !allowOrganizationSchema {
		return StandardProjectSetupResult{}, errors.New("standard setup requires organization-wide Issue Type or Priority changes; rerun with explicit organization-schema authority")
	}
	if len(plan.Changes) == 0 {
		return StandardProjectSetupResult{Project: plan.Project, OwnerType: plan.OwnerType, Verified: true}, nil
	}
	state, err := inspectStandardProjectSetup(ctx, runner, project)
	if err != nil {
		return StandardProjectSetupResult{}, fmt.Errorf("stale pre-write inspection: %w", err)
	}

	for _, change := range plan.Changes {
		if change.Scope != "organization" {
			continue
		}
		switch change.Action {
		case "create_issue_type":
			desired, _ := standardOptionByCurrentName(change.Name, standardClassOptions)
			if err := createOrganizationIssueType(ctx, runner, project.Owner, desired); err != nil {
				return StandardProjectSetupResult{}, err
			}
		case "reconcile_issue_type":
			var current organizationIssueTypeDefinition
			found := false
			for _, value := range state.issueTypes {
				if strings.EqualFold(value.Name, change.Name) {
					current, found = value, true
					break
				}
			}
			if !found {
				return StandardProjectSetupResult{}, fmt.Errorf("organization issue type %q disappeared after preflight", change.Name)
			}
			desired, _ := standardOptionByCurrentName(change.Name, standardClassOptions)
			if err := updateOrganizationIssueType(ctx, runner, project.Owner, current, desired); err != nil {
				return StandardProjectSetupResult{}, err
			}
		case "create_issue_field":
			if err := createOrganizationPriority(ctx, runner, project.Owner); err != nil {
				return StandardProjectSetupResult{}, err
			}
		case "reconcile_issue_field":
			if state.priorityField == nil {
				return StandardProjectSetupResult{}, errors.New("organization Priority issue field disappeared after preflight")
			}
			if err := updateOrganizationPriority(ctx, runner, project.Owner, *state.priorityField); err != nil {
				return StandardProjectSetupResult{}, err
			}
		}
	}

	for _, change := range plan.Changes {
		if change.Scope != "project" || change.Action == "attach_issue_field" {
			continue
		}
		if err := applyProjectFieldChange(ctx, runner, project, state, change); err != nil {
			return StandardProjectSetupResult{}, err
		}
	}

	if plan.OwnerType == "organization" {
		needsAttach := false
		for _, change := range plan.Changes {
			if change.Action == "attach_issue_field" && change.Name == "Priority" {
				needsAttach = true
			}
		}
		if needsAttach {
			fields, err := queryOrganizationIssueFieldDefinitions(ctx, runner, project.Owner)
			if err != nil {
				return StandardProjectSetupResult{}, err
			}
			var priority *organizationIssueFieldDefinition
			for i := range fields {
				if strings.EqualFold(fields[i].Name, "Priority") {
					priority = &fields[i]
					break
				}
			}
			if priority == nil {
				return StandardProjectSetupResult{}, errors.New("organization Priority issue field is missing after setup")
			}
			if err := attachOrganizationIssueField(ctx, runner, project, *priority); err != nil {
				return StandardProjectSetupResult{}, err
			}
		}
	}

	finalPlan, err := PlanStandardProjectSetup(ctx, runner, project)
	if err != nil {
		return StandardProjectSetupResult{}, fmt.Errorf("read back standard Project setup: %w", err)
	}
	if len(finalPlan.Changes) != 0 {
		names := make([]string, 0, len(finalPlan.Changes))
		for _, change := range finalPlan.Changes {
			names = append(names, change.Scope+":"+change.Action+":"+change.Name)
		}
		sort.Strings(names)
		return StandardProjectSetupResult{}, fmt.Errorf("standard Project setup readback is incomplete: %s", strings.Join(names, ", "))
	}
	return StandardProjectSetupResult{Project: plan.Project, OwnerType: plan.OwnerType, Applied: plan.Changes, Verified: true}, nil
}
