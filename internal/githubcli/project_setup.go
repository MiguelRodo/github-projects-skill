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
	Typename     string
	ID           string
	Name         string
	DataType     string
	IsIssueField bool
	Options      []detailedProjectOption
}

type detailedProjectSchema struct {
	ID     string
	Number int
	Title  string
	Fields map[string]detailedProjectField
}

type detailedProjectFieldNode struct {
	Typename     string `json:"__typename"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	IsIssueField bool   `json:"isIssueField"`
	Options      []struct {
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
		Owner *graphQLProjectOwner[detailedProjectData] `json:"owner"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ownerTypeFromTypename maps the repositoryOwner __typename to the contract's
// owner-type vocabulary.
func ownerTypeFromTypename(typename string) (string, error) {
	switch typename {
	case "User":
		return "user", nil
	case "Organization":
		return "organization", nil
	default:
		return "", fmt.Errorf("unsupported Project owner type %q", typename)
	}
}

// queryDetailedProjectSchema reads the Project fields through the
// repositoryOwner root, which resolves both users and organizations in one
// request, and returns the observed owner type.
func queryDetailedProjectSchema(ctx context.Context, client Client, project contract.Project) (detailedProjectSchema, string, error) {
	query := `query($login: String!, $number: Int!) {
  owner: repositoryOwner(login: $login) {
    __typename
    login
    ... on ProjectV2Owner {
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
  }
}`
	out, err := graphQLBytes(ctx, client, query, map[string]any{"login": project.Owner, "number": project.Number})
	if err != nil {
		return detailedProjectSchema{}, "", fmt.Errorf("query Project schema: %w", err)
	}
	var resp detailedProjectResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return detailedProjectSchema{}, "", fmt.Errorf("decode Project schema: %w", err)
	}
	if len(resp.Errors) > 0 {
		return detailedProjectSchema{}, "", fmt.Errorf("GraphQL error querying Project schema: %s", resp.Errors[0].Message)
	}
	if resp.Data.Owner == nil {
		return detailedProjectSchema{}, "", fmt.Errorf("Project owner %s was not found or is not accessible", project.Owner)
	}
	if err := verifyProjectOwner(project, resp.Data.Owner.Typename, resp.Data.Owner.Login); err != nil {
		return detailedProjectSchema{}, "", err
	}
	ownerType, err := ownerTypeFromTypename(resp.Data.Owner.Typename)
	if err != nil {
		return detailedProjectSchema{}, "", err
	}
	data := resp.Data.Owner.ProjectV2
	if data == nil {
		return detailedProjectSchema{}, "", fmt.Errorf("Project %s/%d not found at %s root", project.Owner, project.Number, ownerType)
	}
	if data.Number != project.Number || data.Title != project.Title {
		return detailedProjectSchema{}, "", fmt.Errorf("Project identity changed: got %s/%d %q, expected %s/%d %q", project.Owner, data.Number, data.Title, project.Owner, project.Number, project.Title)
	}
	if data.Fields.PageInfo.HasNextPage {
		return detailedProjectSchema{}, "", fmt.Errorf("Project %s/%d has more than 100 fields; refusing an incomplete setup read", project.Owner, project.Number)
	}
	schema := detailedProjectSchema{ID: data.ID, Number: data.Number, Title: data.Title, Fields: make(map[string]detailedProjectField)}
	for _, node := range data.Fields.Nodes {
		field := detailedProjectField{Typename: node.Typename, ID: node.ID, Name: node.Name, DataType: node.DataType, IsIssueField: node.IsIssueField}
		for _, option := range node.Options {
			field.Options = append(field.Options, detailedProjectOption{ID: option.ID, Name: option.Name, Color: strings.ToUpper(option.Color), Description: option.Description})
		}
		key := strings.ToLower(strings.TrimSpace(node.Name))
		if _, exists := schema.Fields[key]; exists {
			return detailedProjectSchema{}, "", fmt.Errorf("Project %s/%d has more than one field named %q", project.Owner, project.Number, node.Name)
		}
		schema.Fields[key] = field
	}
	return schema, ownerType, nil
}

type organizationIssueTypeDefinition struct {
	NodeID      string
	Name        string
	Description string
	Color       string
	Enabled     bool
}

type organizationIssueTypeNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	IsEnabled   bool   `json:"isEnabled"`
}

type organizationIssueTypeGraphQLResponse struct {
	Data struct {
		Organization *struct {
			ID         string `json:"id"`
			IssueTypes struct {
				Nodes    []organizationIssueTypeNode `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issueTypes"`
		} `json:"organization"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
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

func queryOrganizationIssueTypes(ctx context.Context, client Client, project contract.Project) (string, []organizationIssueTypeDefinition, error) {
	const query = `query($login: String!, $after: String) {
  organization(login: $login) {
    id
    issueTypes(first: 100, after: $after) {
      nodes { id name description color isEnabled }
      pageInfo { hasNextPage endCursor }
    }
  }
}`
	var organizationID string
	definitions := []organizationIssueTypeDefinition{}
	seen := make(map[string]bool)
	var after string
	for page := 0; ; page++ {
		if page >= 10 {
			return "", nil, errors.New("more than 1000 issue types; refusing an incomplete read")
		}
		variables := map[string]any{"login": project.Owner}
		if page > 0 {
			variables["after"] = after
		}
		graphOut, err := graphQLBytes(ctx, client, query, variables)
		if err != nil {
			return "", nil, fmt.Errorf("read organization issue type definitions: %w", err)
		}
		var resp organizationIssueTypeGraphQLResponse
		if err := json.Unmarshal(graphOut, &resp); err != nil {
			return "", nil, fmt.Errorf("decode organization issue type definitions: %w", err)
		}
		if len(resp.Errors) > 0 {
			return "", nil, fmt.Errorf("GraphQL error querying organization issue types: %s", resp.Errors[0].Message)
		}
		if resp.Data.Organization == nil || resp.Data.Organization.ID == "" {
			return "", nil, fmt.Errorf("organization %s was not returned by GraphQL", project.Owner)
		}
		if organizationID == "" {
			organizationID = resp.Data.Organization.ID
		}
		for _, node := range resp.Data.Organization.IssueTypes.Nodes {
			if node.ID == "" || node.Name == "" {
				return "", nil, errors.New("organization issue type GraphQL read returned an incomplete node")
			}
			if seen[node.ID] {
				return "", nil, fmt.Errorf("organization issue type node %s appeared more than once", node.ID)
			}
			seen[node.ID] = true
			definitions = append(definitions, organizationIssueTypeDefinition{
				NodeID: node.ID, Name: node.Name, Description: node.Description,
				Color: strings.ToUpper(node.Color), Enabled: node.IsEnabled,
			})
		}
		if !resp.Data.Organization.IssueTypes.PageInfo.HasNextPage {
			break
		}
		after = resp.Data.Organization.IssueTypes.PageInfo.EndCursor
	}
	return organizationID, definitions, nil
}

func queryOrganizationIssueFieldDefinitions(ctx context.Context, client Client, owner string) ([]organizationIssueFieldDefinition, error) {
	out, err := restPageBytes(ctx, client, "/"+"orgs/"+owner+"/issue-fields?per_page=100")
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
	ownerType      string
	project        detailedProjectSchema
	organizationID string
	issueTypes     []organizationIssueTypeDefinition
	issueFields    []organizationIssueFieldDefinition
	priorityField  *organizationIssueFieldDefinition
}

func inspectStandardProjectSetup(ctx context.Context, client Client, project contract.Project) (setupState, error) {
	projectSchema, ownerType, err := queryDetailedProjectSchema(ctx, client, project)
	if err != nil {
		return setupState{}, err
	}
	status, ok := projectSchema.Fields["status"]
	if !ok || status.DataType != "SINGLE_SELECT" {
		return setupState{}, fmt.Errorf("Project %s/%d does not expose the standard Status single-select field", project.Owner, project.Number)
	}
	state := setupState{ownerType: ownerType, project: projectSchema}
	if ownerType == "organization" {
		state.organizationID, state.issueTypes, err = queryOrganizationIssueTypes(ctx, client, project)
		if err != nil {
			return setupState{}, err
		}
		state.issueFields, err = queryOrganizationIssueFieldDefinitions(ctx, client, project.Owner)
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

// describeOptionChanges summarises a reconciliation from the actual diff so a
// plan shows renames, recolours, additions and reordering before any write.
func describeOptionChanges(current, reconciled []detailedProjectOption) string {
	byID := make(map[string]detailedProjectOption, len(current))
	for _, option := range current {
		if option.ID != "" {
			byID[option.ID] = option
		}
	}
	var parts []string
	kept := make(map[string]bool)
	var newOrder []string
	for _, option := range reconciled {
		if option.ID == "" {
			parts = append(parts, fmt.Sprintf("add %s (%s)", option.Name, strings.ToUpper(option.Color)))
			continue
		}
		kept[option.ID] = true
		newOrder = append(newOrder, option.ID)
		old, ok := byID[option.ID]
		if !ok {
			continue
		}
		if old.Name != option.Name {
			parts = append(parts, fmt.Sprintf("rename %s->%s", old.Name, option.Name))
		}
		if !strings.EqualFold(old.Color, option.Color) {
			parts = append(parts, fmt.Sprintf("recolour %s %s->%s", option.Name, strings.ToUpper(old.Color), strings.ToUpper(option.Color)))
		}
	}
	var oldOrder []string
	for _, option := range current {
		if kept[option.ID] {
			oldOrder = append(oldOrder, option.ID)
		}
	}
	if !reflect.DeepEqual(oldOrder, newOrder) {
		names := make([]string, 0, len(reconciled))
		for _, option := range reconciled {
			names = append(names, option.Name)
		}
		parts = append(parts, "reorder to "+strings.Join(names, ", "))
	}
	if len(parts) == 0 {
		return "normalise options"
	}
	if len(kept) > 0 {
		parts = append(parts, "existing option IDs kept so item values are preserved")
	}
	return strings.Join(parts, "; ")
}

// keptOptionIDs returns the pre-update option IDs a reconciliation intends to
// keep. Readback must find every one of them, otherwise GitHub regenerated the
// options and cleared the corresponding item values.
func keptOptionIDs(reconciled []detailedProjectOption) []string {
	var ids []string
	for _, option := range reconciled {
		if option.ID != "" {
			ids = append(ids, option.ID)
		}
	}
	return ids
}

func organizationOptionsAsProject(current []organizationIssueFieldOptionDefinition) []detailedProjectOption {
	result := make([]detailedProjectOption, 0, len(current))
	for _, option := range current {
		id := ""
		if option.ID != 0 {
			id = strconv.Itoa(option.ID)
		}
		result = append(result, detailedProjectOption{ID: id, Name: option.Name, Color: strings.ToUpper(option.Color), Description: option.Description})
	}
	return result
}

func reconcileOrganizationPriorityOptions(current []organizationIssueFieldOptionDefinition) ([]organizationIssueFieldOptionDefinition, bool, error) {
	projectCurrent := organizationOptionsAsProject(current)
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
	return result, changed, nil
}

func planProjectField(changes *[]StandardProjectSetupChange, state setupState, name, dataType string, options []standardOption) error {
	field, exists := state.project.Fields[strings.ToLower(name)]
	if !exists {
		*changes = append(*changes, StandardProjectSetupChange{Scope: "project", Action: "create_field", Name: name, Detail: dataType})
		return nil
	}
	if field.DataType != dataType {
		return fmt.Errorf("Project field %q has type %s, want %s; rename or delete the existing field in the Project settings so setup can create the standard %s field (existing values will not be migrated), or declare a deliberate local override in the contract", name, field.DataType, dataType, dataType)
	}
	if len(options) == 0 {
		return nil
	}
	if field.Typename != "ProjectV2SingleSelectField" || field.IsIssueField {
		return fmt.Errorf("Project field %q is not a Project-local single-select field; rename or remove it in the Project settings so setup can create the standard Project-local field (existing values will not be migrated)", name)
	}
	reconciled, changed, err := reconcileProjectOptions(field.Options, options)
	if err != nil {
		return fmt.Errorf("reconcile Project field %q: %w; rename or remove the duplicate option in the Project settings, then rerun", name, err)
	}
	if changed {
		*changes = append(*changes, StandardProjectSetupChange{Scope: "project", Action: "reconcile_options", Name: name, Detail: describeOptionChanges(field.Options, reconciled)})
	}
	return nil
}

func planStandardProjectSetupFromState(project contract.Project, state setupState) (StandardProjectSetupPlan, error) {
	plan := StandardProjectSetupPlan{
		Project:   ProjectIdentity{Number: project.Number, Owner: project.Owner, Title: project.Title},
		OwnerType: state.ownerType,
		Changes:   []StandardProjectSetupChange{},
	}
	if err := planProjectField(&plan.Changes, state, "Due date", "DATE", nil); err != nil {
		return StandardProjectSetupPlan{}, err
	}
	if err := planProjectField(&plan.Changes, state, "Target date", "DATE", nil); err != nil {
		return StandardProjectSetupPlan{}, err
	}
	if state.ownerType == "user" {
		if err := planProjectField(&plan.Changes, state, "Class", "SINGLE_SELECT", standardClassOptions); err != nil {
			return StandardProjectSetupPlan{}, err
		}
		if err := planProjectField(&plan.Changes, state, "Priority", "SINGLE_SELECT", standardPriorityOptions); err != nil {
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
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "create_issue_type", Name: desired.Name, Detail: "create enabled with colour " + desired.Color})
			plan.RequiresOrganizationSchema = true
			continue
		}
		if !current.Enabled || !strings.EqualFold(current.Color, desired.Color) {
			var parts []string
			if !strings.EqualFold(current.Color, desired.Color) {
				parts = append(parts, fmt.Sprintf("recolour %s->%s", strings.ToUpper(current.Color), desired.Color))
			}
			if !current.Enabled {
				parts = append(parts, "re-enable (currently disabled)")
			}
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "reconcile_issue_type", Name: desired.Name, Detail: strings.Join(parts, "; ")})
			plan.RequiresOrganizationSchema = true
		}
	}
	if state.priorityField == nil {
		plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "create_issue_field", Name: "Priority", Detail: "P0,P1,P2,P3"})
		plan.RequiresOrganizationSchema = true
	} else {
		if state.priorityField.DataType != "single_select" {
			return StandardProjectSetupPlan{}, fmt.Errorf("organization issue field Priority has type %s, want single_select; an organization owner must rename or replace that issue field before setup can continue", state.priorityField.DataType)
		}
		reconciled, changed, err := reconcileOrganizationPriorityOptions(state.priorityField.Options)
		if err != nil {
			return StandardProjectSetupPlan{}, fmt.Errorf("reconcile organization Priority: %w; rename or remove the duplicate option in the organization's issue field settings, then rerun", err)
		}
		if changed {
			plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "organization", Action: "reconcile_issue_field", Name: "Priority", Detail: describeOptionChanges(organizationOptionsAsProject(state.priorityField.Options), organizationOptionsAsProject(reconciled))})
			plan.RequiresOrganizationSchema = true
		}
	}
	if field, exists := state.project.Fields["priority"]; !exists {
		plan.Changes = append(plan.Changes, StandardProjectSetupChange{Scope: "project", Action: "attach_issue_field", Name: "Priority"})
	} else if !field.IsIssueField {
		return StandardProjectSetupPlan{}, fmt.Errorf("organization Project %s/%d has a Project-local field named Priority (type %s), which blocks attaching the organization Priority issue field; rename or remove that Project field in the Project settings, then rerun (its values will not migrate to the issue field)", project.Owner, project.Number, field.DataType)
	} else if field.DataType != "SINGLE_SELECT" {
		return StandardProjectSetupPlan{}, fmt.Errorf("organization Project %s/%d has a Priority issue field of type %s, want SINGLE_SELECT; an organization owner must replace that issue field before setup can continue", project.Owner, project.Number, field.DataType)
	}
	return plan, nil
}

// PlanStandardProjectSetup inspects live Project and organization schema and
// returns only the changes needed for the shared standard profile.
func PlanStandardProjectSetup(ctx context.Context, client Client, project contract.Project) (StandardProjectSetupPlan, error) {
	state, err := inspectStandardProjectSetup(ctx, client, project)
	if err != nil {
		return StandardProjectSetupPlan{}, err
	}
	return planStandardProjectSetupFromState(project, state)
}

func createProjectField(ctx context.Context, client Client, projectID, name, dataType string, options []standardOption) error {
	if len(options) == 0 {
		query := `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!) {
  createProjectV2Field(input: {projectId: $projectId, name: $name, dataType: $dataType}) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`
		body := map[string]any{"query": query, "variables": map[string]any{"projectId": projectID, "name": name, "dataType": dataType}}
		if err := graphQLWrite(ctx, client, body); err != nil {
			return fmt.Errorf("create Project field %q: %w", name, err)
		}
		return nil
	}
	items := make([]map[string]any, 0, len(options))
	for _, option := range options {
		items = append(items, map[string]any{"name": option.Name, "color": option.Color, "description": ""})
	}
	query := `mutation($projectId: ID!, $name: String!, $dataType: ProjectV2CustomFieldType!, $options: [ProjectV2SingleSelectFieldOptionInput!]!) {
  createProjectV2Field(input: {projectId: $projectId, name: $name, dataType: $dataType, singleSelectOptions: $options}) {
    projectV2Field { ... on ProjectV2FieldCommon { id name dataType } }
  }
}`
	body := map[string]any{"query": query, "variables": map[string]any{"projectId": projectID, "name": name, "dataType": dataType, "options": items}}
	if err := graphQLWrite(ctx, client, body); err != nil {
		return fmt.Errorf("create Project field %q: %w", name, err)
	}
	return nil
}

func updateProjectSingleSelect(ctx context.Context, client Client, field detailedProjectField, desired []standardOption) ([]string, error) {
	options, changed, err := reconcileProjectOptions(field.Options, desired)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
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
	body := map[string]any{"query": query, "variables": map[string]any{"fieldId": field.ID, "options": payloadOptions}}
	if err := graphQLWrite(ctx, client, body); err != nil {
		return nil, fmt.Errorf("update Project field %q: %w", field.Name, err)
	}
	return keptOptionIDs(options), nil
}

func createOrganizationIssueType(ctx context.Context, client Client, organizationID string, desired standardOption) error {
	query := `mutation($ownerId: ID!, $name: String!, $color: IssueTypeColor!) {
  createIssueType(input: {ownerId: $ownerId, name: $name, description: "", isEnabled: true, color: $color}) {
    issueType { id name color isEnabled }
  }
}`
	body := map[string]any{"query": query, "variables": map[string]any{"ownerId": organizationID, "name": desired.Name, "color": desired.Color}}
	if err := graphQLWrite(ctx, client, body); err != nil {
		return fmt.Errorf("create organization issue type %q: %w", desired.Name, err)
	}
	return nil
}

func updateOrganizationIssueType(ctx context.Context, client Client, current organizationIssueTypeDefinition, desired standardOption) error {
	query := `mutation($issueTypeId: ID!, $name: String!, $description: String, $color: IssueTypeColor!) {
  updateIssueType(input: {issueTypeId: $issueTypeId, name: $name, description: $description, isEnabled: true, color: $color}) {
    issueType { id name color isEnabled }
  }
}`
	body := map[string]any{"query": query, "variables": map[string]any{
		"issueTypeId": current.NodeID, "name": desired.Name,
		"description": current.Description, "color": desired.Color,
	}}
	if err := graphQLWrite(ctx, client, body); err != nil {
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

func createOrganizationPriority(ctx context.Context, client Client, owner string) error {
	options := make([]organizationIssueFieldOptionDefinition, 0, len(standardPriorityOptions))
	for i, option := range standardPriorityOptions {
		options = append(options, organizationIssueFieldOptionDefinition{Name: option.Name, Color: strings.ToLower(option.Color), Priority: i + 1})
	}
	body := organizationPriorityBody(options, "Level of importance")
	body["data_type"] = "single_select"
	if _, err := client.REST(ctx, "POST", "/"+"orgs/"+owner+"/issue-fields", body); err != nil {
		return fmt.Errorf("create organization Priority issue field: %w", err)
	}
	return nil
}

func updateOrganizationPriority(ctx context.Context, client Client, owner string, field organizationIssueFieldDefinition) ([]string, error) {
	options, changed, err := reconcileOrganizationPriorityOptions(field.Options)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	body := organizationPriorityBody(options, field.Description)
	if _, err := client.REST(ctx, "PATCH", "/"+fmt.Sprintf("orgs/%s/issue-fields/%d", owner, field.ID), body); err != nil {
		return nil, fmt.Errorf("update organization Priority issue field: %w", err)
	}
	return keptOptionIDs(organizationOptionsAsProject(options)), nil
}

func attachOrganizationIssueField(ctx context.Context, client Client, project contract.Project, field organizationIssueFieldDefinition) error {
	body := map[string]any{"issue_field_id": field.ID}
	if _, err := client.REST(ctx, "POST", "/"+fmt.Sprintf("orgs/%s/projectsV2/%d/fields", project.Owner, project.Number), body); err != nil {
		return fmt.Errorf("attach organization Priority issue field to Project: %w", err)
	}
	return nil
}

func applyProjectFieldChange(ctx context.Context, client Client, state setupState, change StandardProjectSetupChange) ([]string, error) {
	switch change.Action {
	case "create_field":
		var options []standardOption
		dataType := "DATE"
		if change.Name == "Class" {
			options, dataType = standardClassOptions, "SINGLE_SELECT"
		} else if change.Name == "Priority" {
			options, dataType = standardPriorityOptions, "SINGLE_SELECT"
		}
		return nil, createProjectField(ctx, client, state.project.ID, change.Name, dataType, options)
	case "reconcile_options":
		field := state.project.Fields[strings.ToLower(change.Name)]
		if change.Name == "Class" {
			return updateProjectSingleSelect(ctx, client, field, standardClassOptions)
		}
		return updateProjectSingleSelect(ctx, client, field, standardPriorityOptions)
	default:
		return nil, fmt.Errorf("unsupported project field setup action %q", change.Action)
	}
}

func setupChangeLabel(change StandardProjectSetupChange) string {
	return change.Scope + ":" + change.Action + ":" + change.Name
}

// partialApplyError reports exactly which changes reached GitHub before a
// failure, so an operator never has to guess what a failed apply left behind.
func partialApplyError(applied []string, failedAt string, err error) error {
	done := "nothing"
	if len(applied) > 0 {
		done = strings.Join(applied, ", ")
	}
	return fmt.Errorf("applied: %s; failed at %s: %w; run the plan again to see remaining work", done, failedAt, err)
}

func missingOptionIDs(kept []string, options []detailedProjectOption) []string {
	present := make(map[string]bool, len(options))
	for _, option := range options {
		present[option.ID] = true
	}
	var missing []string
	for _, id := range kept {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func optionIDLossError(field string, missing []string) error {
	return fmt.Errorf("%s option IDs %s disappeared after reconciliation: GitHub regenerated the options, which clears every item's value for them; the schema now has the standard names but existing item values must be checked and restored from history before relying on this field", field, strings.Join(missing, ", "))
}

// ApplyStandardProjectSetup performs standard setup and independently re-reads
// the live schema. Organization-wide changes need an explicit opt-in flag.
func ApplyStandardProjectSetup(ctx context.Context, client Client, project contract.Project, allowOrganizationSchema bool) (StandardProjectSetupResult, error) {
	state, err := inspectStandardProjectSetup(ctx, client, project)
	if err != nil {
		return StandardProjectSetupResult{}, err
	}
	plan, err := planStandardProjectSetupFromState(project, state)
	if err != nil {
		return StandardProjectSetupResult{}, err
	}
	if plan.RequiresOrganizationSchema && !allowOrganizationSchema {
		return StandardProjectSetupResult{}, errors.New("standard setup requires organization-wide Issue Type or Priority changes; after those organization-wide changes are explicitly authorised, rerun with --apply --allow-organization-schema")
	}
	result := StandardProjectSetupResult{Project: plan.Project, OwnerType: plan.OwnerType, Applied: []StandardProjectSetupChange{}}
	if len(plan.Changes) == 0 {
		result.Verified = true
		return result, nil
	}

	var appliedLabels []string
	fail := func(failedAt string, err error) (StandardProjectSetupResult, error) {
		return result, partialApplyError(appliedLabels, failedAt, err)
	}
	record := func(change StandardProjectSetupChange) {
		result.Applied = append(result.Applied, change)
		appliedLabels = append(appliedLabels, setupChangeLabel(change))
	}
	keptProjectOptions := make(map[string][]string)
	var keptPriorityOptions []string
	priorityCreated := false

	for _, change := range plan.Changes {
		if change.Scope != "organization" {
			continue
		}
		var err error
		switch change.Action {
		case "create_issue_type":
			desired, _ := standardOptionByCurrentName(change.Name, standardClassOptions)
			err = createOrganizationIssueType(ctx, client, state.organizationID, desired)
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
				err = fmt.Errorf("organization issue type %q disappeared after preflight", change.Name)
				break
			}
			desired, _ := standardOptionByCurrentName(change.Name, standardClassOptions)
			err = updateOrganizationIssueType(ctx, client, current, desired)
		case "create_issue_field":
			err = createOrganizationPriority(ctx, client, project.Owner)
			priorityCreated = err == nil
		case "reconcile_issue_field":
			if state.priorityField == nil {
				err = errors.New("organization Priority issue field disappeared after preflight")
				break
			}
			keptPriorityOptions, err = updateOrganizationPriority(ctx, client, project.Owner, *state.priorityField)
		default:
			err = fmt.Errorf("unsupported organization setup action %q", change.Action)
		}
		if err != nil {
			return fail(setupChangeLabel(change), err)
		}
		record(change)
	}

	for _, change := range plan.Changes {
		if change.Scope != "project" || change.Action == "attach_issue_field" {
			continue
		}
		kept, err := applyProjectFieldChange(ctx, client, state, change)
		if err != nil {
			return fail(setupChangeLabel(change), err)
		}
		if change.Action == "reconcile_options" {
			keptProjectOptions[change.Name] = kept
		}
		record(change)
	}

	for _, change := range plan.Changes {
		if change.Action != "attach_issue_field" || change.Name != "Priority" {
			continue
		}
		priority := state.priorityField
		if priorityCreated || priority == nil {
			// Only a just-created field needs a fresh listing to learn its ID.
			fields, err := queryOrganizationIssueFieldDefinitions(ctx, client, project.Owner)
			if err != nil {
				return fail(setupChangeLabel(change), err)
			}
			priority = nil
			for i := range fields {
				if strings.EqualFold(fields[i].Name, "Priority") {
					priority = &fields[i]
					break
				}
			}
		}
		if priority == nil {
			return fail(setupChangeLabel(change), errors.New("organization Priority issue field is missing after setup"))
		}
		if err := attachOrganizationIssueField(ctx, client, project, *priority); err != nil {
			return fail(setupChangeLabel(change), err)
		}
		record(change)
	}

	// The owner type is already known; do not rediscover it during readback.
	readProject := project
	readProject.OwnerType = state.ownerType
	finalState, err := inspectStandardProjectSetup(ctx, client, readProject)
	if err != nil {
		return fail("readback", fmt.Errorf("read back standard Project setup: %w", err))
	}
	finalPlan, err := planStandardProjectSetupFromState(project, finalState)
	if err != nil {
		return fail("readback", fmt.Errorf("read back standard Project setup: %w", err))
	}
	if len(finalPlan.Changes) != 0 {
		names := make([]string, 0, len(finalPlan.Changes))
		for _, change := range finalPlan.Changes {
			names = append(names, setupChangeLabel(change))
		}
		sort.Strings(names)
		return fail("readback", fmt.Errorf("standard Project setup readback is incomplete: %s", strings.Join(names, ", ")))
	}
	fieldNames := make([]string, 0, len(keptProjectOptions))
	for name := range keptProjectOptions {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	for _, name := range fieldNames {
		field := finalState.project.Fields[strings.ToLower(name)]
		if missing := missingOptionIDs(keptProjectOptions[name], field.Options); len(missing) > 0 {
			return fail("readback", optionIDLossError(fmt.Sprintf("Project field %q", name), missing))
		}
	}
	if len(keptPriorityOptions) > 0 {
		var options []detailedProjectOption
		if finalState.priorityField != nil {
			options = organizationOptionsAsProject(finalState.priorityField.Options)
		}
		if missing := missingOptionIDs(keptPriorityOptions, options); len(missing) > 0 {
			return fail("readback", optionIDLossError("organization Priority issue field", missing))
		}
	}
	result.Verified = true
	return result, nil
}
