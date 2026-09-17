package githubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

const standardBacklogViewName = "Backlog"

// StandardBacklogViewChange is one bounded change proposed for the shared
// human-facing Backlog view.
type StandardBacklogViewChange struct {
	Action string `json:"action"`
	ViewID string `json:"viewId,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// StandardBacklogViewPlan describes the live Backlog-view delta without
// mutating GitHub.
type StandardBacklogViewPlan struct {
	Project       ProjectIdentity             `json:"project"`
	OwnerType     string                      `json:"ownerType"`
	VisibleFields []string                    `json:"visibleFields"`
	Changes       []StandardBacklogViewChange `json:"changes"`
}

// StandardBacklogViewResult is returned after an independently verified apply.
type StandardBacklogViewResult struct {
	Project       ProjectIdentity             `json:"project"`
	OwnerType     string                      `json:"ownerType"`
	VisibleFields []string                    `json:"visibleFields"`
	Applied       []StandardBacklogViewChange `json:"applied"`
	Verified      bool                        `json:"verified"`
}

type restProjectField struct {
	ID       int    `json:"id"`
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}

type backlogField struct {
	Name   string
	RESTID int
	NodeID string
}

type backlogViewSpec struct {
	Visible  []backlogField
	Status   backlogField
	Priority backlogField
}

type projectViewFieldRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectViewFieldConnection struct {
	Nodes    []projectViewFieldRef `json:"nodes"`
	PageInfo struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"pageInfo"`
}

type projectViewSortNode struct {
	Direction string              `json:"direction"`
	Field     projectViewFieldRef `json:"field"`
}

type projectViewSortConnection struct {
	Nodes    []projectViewSortNode `json:"nodes"`
	PageInfo struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"pageInfo"`
}

type projectViewNode struct {
	ID            string  `json:"id"`
	Number        int     `json:"number"`
	Name          string  `json:"name"`
	Layout        string  `json:"layout"`
	Filter        *string `json:"filter"`
	Configuration struct {
		VisibleFields projectViewFieldConnection `json:"visibleFields"`
	} `json:"configuration"`
	GroupByFields         projectViewFieldConnection `json:"groupByFields"`
	VerticalGroupByFields projectViewFieldConnection `json:"verticalGroupByFields"`
	SortByFields          projectViewSortConnection  `json:"sortByFields"`
}

type projectViewsData struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Views  struct {
		Nodes    []projectViewNode `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"views"`
}

type projectViewsResponse struct {
	Data struct {
		User struct {
			ProjectV2 *projectViewsData `json:"projectV2"`
		} `json:"user"`
		Organization struct {
			ProjectV2 *projectViewsData `json:"projectV2"`
		} `json:"organization"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type backlogViewState struct {
	ownerType  string
	schema     detailedProjectSchema
	restFields []restProjectField
	views      []projectViewNode
	spec       backlogViewSpec
}

func queryRESTProjectFields(ctx context.Context, runner Runner, project contract.Project, ownerType string) ([]restProjectField, error) {
	endpoint := fmt.Sprintf("users/%s/projectsV2/%d/fields?per_page=100", project.Owner, project.Number)
	if ownerType == "organization" {
		endpoint = fmt.Sprintf("orgs/%s/projectsV2/%d/fields?per_page=100", project.Owner, project.Number)
	}
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, endpoint)
	out, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list Project fields through REST: %w", err)
	}
	var pages [][]restProjectField
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("decode REST Project fields: %w", err)
	}
	var fields []restProjectField
	for _, page := range pages {
		fields = append(fields, page...)
	}
	return fields, nil
}

func queryProjectViews(ctx context.Context, runner Runner, project contract.Project, ownerType string) ([]projectViewNode, error) {
	query := fmt.Sprintf(`query($login: String!, $number: Int!) {
  %s(login: $login) {
    projectV2(number: $number) {
      id
      number
      title
      views(first: 100) {
        nodes {
          id
          number
          name
          layout
          filter
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
            nodes {
              direction
              field { ... on ProjectV2FieldCommon { id name } }
            }
            pageInfo { hasNextPage }
          }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, ownerType)
	out, err := runner.Run(ctx, "api", "graphql", "-f", "query="+query, "-f", "login="+project.Owner, "-F", "number="+strconv.Itoa(project.Number))
	if err != nil {
		return nil, fmt.Errorf("query Project views: %w", err)
	}
	var resp projectViewsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode Project views: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error querying Project views: %s", resp.Errors[0].Message)
	}
	var data *projectViewsData
	if ownerType == "organization" {
		data = resp.Data.Organization.ProjectV2
	} else {
		data = resp.Data.User.ProjectV2
	}
	if data == nil {
		return nil, fmt.Errorf("Project %s/%d not found at %s root", project.Owner, project.Number, ownerType)
	}
	if data.Number != project.Number || data.Title != project.Title {
		return nil, fmt.Errorf("Project identity changed: got %s/%d %q, expected %s/%d %q", project.Owner, data.Number, data.Title, project.Owner, project.Number, project.Title)
	}
	if data.Views.PageInfo.HasNextPage {
		return nil, fmt.Errorf("Project %s/%d has more than 100 views; refusing an incomplete view read", project.Owner, project.Number)
	}
	for _, view := range data.Views.Nodes {
		if view.Configuration.VisibleFields.PageInfo.HasNextPage || view.GroupByFields.PageInfo.HasNextPage || view.VerticalGroupByFields.PageInfo.HasNextPage || view.SortByFields.PageInfo.HasNextPage {
			return nil, fmt.Errorf("Project view %q has paginated configuration; refusing an incomplete read", view.Name)
		}
	}
	return data.Views.Nodes, nil
}

func buildBacklogViewSpec(project contract.Project, ownerType string, schema detailedProjectSchema, restFields []restProjectField) (backlogViewSpec, error) {
	restByName := make(map[string]restProjectField)
	for _, field := range restFields {
		key := strings.ToLower(strings.TrimSpace(field.Name))
		if _, exists := restByName[key]; exists {
			return backlogViewSpec{}, fmt.Errorf("Project %s/%d has more than one REST field named %q", project.Owner, project.Number, field.Name)
		}
		restByName[key] = field
	}

	resolve := func(name string, required bool) (backlogField, bool, error) {
		key := strings.ToLower(name)
		restField, restOK := restByName[key]
		graphField, graphOK := schema.Fields[key]
		if restOK != graphOK {
			return backlogField{}, false, fmt.Errorf("Project field %q is inconsistent between REST and GraphQL reads", name)
		}
		if !restOK {
			if required {
				return backlogField{}, false, fmt.Errorf("Project %s/%d is missing required field %q; run standard field setup first", project.Owner, project.Number, name)
			}
			return backlogField{}, false, nil
		}
		if restField.ID == 0 || graphField.ID == "" {
			return backlogField{}, false, fmt.Errorf("Project field %q is missing a provider ID", name)
		}
		return backlogField{Name: graphField.Name, RESTID: restField.ID, NodeID: graphField.ID}, true, nil
	}

	className := "Class"
	if ownerType == "organization" {
		className = "Issue Type"
	}
	orderedNames := []struct {
		name     string
		required bool
	}{
		{name: "Title", required: true},
		{name: "Status", required: true},
		{name: "Due date"},
		{name: "Target date"},
		{name: "Assignees"},
		{name: "Linked pull requests"},
		{name: "Sub-issues progress"},
		{name: "Priority", required: true},
		{name: className, required: true},
	}
	var spec backlogViewSpec
	for _, wanted := range orderedNames {
		field, ok, err := resolve(wanted.name, wanted.required)
		if err != nil {
			return backlogViewSpec{}, err
		}
		if !ok {
			continue
		}
		spec.Visible = append(spec.Visible, field)
		if strings.EqualFold(wanted.name, "Status") {
			spec.Status = field
		}
		if strings.EqualFold(wanted.name, "Priority") {
			spec.Priority = field
		}
	}
	return spec, nil
}

func inspectStandardBacklogView(ctx context.Context, runner Runner, project contract.Project) (backlogViewState, error) {
	ownerType, err := discoverOwnerType(ctx, runner, project.Owner)
	if err != nil {
		return backlogViewState{}, fmt.Errorf("discover Project owner type: %w", err)
	}
	if ownerType != "user" && ownerType != "organization" {
		return backlogViewState{}, fmt.Errorf("unsupported Project owner type %q", ownerType)
	}
	schema, err := queryDetailedProjectSchema(ctx, runner, project, ownerType)
	if err != nil {
		return backlogViewState{}, err
	}
	restFields, err := queryRESTProjectFields(ctx, runner, project, ownerType)
	if err != nil {
		return backlogViewState{}, err
	}
	views, err := queryProjectViews(ctx, runner, project, ownerType)
	if err != nil {
		return backlogViewState{}, err
	}
	spec, err := buildBacklogViewSpec(project, ownerType, schema, restFields)
	if err != nil {
		return backlogViewState{}, err
	}
	return backlogViewState{ownerType: ownerType, schema: schema, restFields: restFields, views: views, spec: spec}, nil
}

func idsFromFields(fields []backlogField) []string {
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		ids = append(ids, field.NodeID)
	}
	return ids
}

func idsFromViewFields(fields []projectViewFieldRef) []string {
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		ids = append(ids, field.ID)
	}
	return ids
}

func filterIsEmpty(filter *string) bool {
	return filter == nil || strings.TrimSpace(*filter) == ""
}

func backlogViewGroupSortMatches(view projectViewNode, spec backlogViewSpec) bool {
	if len(view.GroupByFields.Nodes) != 1 || view.GroupByFields.Nodes[0].ID != spec.Status.NodeID {
		return false
	}
	if len(view.VerticalGroupByFields.Nodes) != 0 {
		return false
	}
	if len(view.SortByFields.Nodes) != 1 {
		return false
	}
	sortBy := view.SortByFields.Nodes[0]
	return sortBy.Field.ID == spec.Priority.NodeID && strings.EqualFold(sortBy.Direction, "ASC")
}

func backlogViewBasicMatches(view projectViewNode, spec backlogViewSpec) bool {
	return strings.EqualFold(view.Layout, "TABLE_LAYOUT") && filterIsEmpty(view.Filter) && reflect.DeepEqual(idsFromViewFields(view.Configuration.VisibleFields.Nodes), idsFromFields(spec.Visible))
}

func backlogViewMatches(view projectViewNode, spec backlogViewSpec) bool {
	return backlogViewBasicMatches(view, spec) && backlogViewGroupSortMatches(view, spec)
}

func backlogViews(views []projectViewNode) []projectViewNode {
	var matches []projectViewNode
	for _, view := range views {
		if strings.EqualFold(strings.TrimSpace(view.Name), standardBacklogViewName) {
			matches = append(matches, view)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Number < matches[j].Number })
	return matches
}

func planBacklogViewChanges(views []projectViewNode, spec backlogViewSpec) ([]StandardBacklogViewChange, error) {
	backlogs := backlogViews(views)
	if len(backlogs) == 0 {
		return []StandardBacklogViewChange{{Action: "create_view", Detail: "table; group Status; sort Priority ascending"}}, nil
	}
	var exact []projectViewNode
	for _, view := range backlogs {
		if backlogViewMatches(view, spec) {
			exact = append(exact, view)
		}
	}
	if len(backlogs) > 1 {
		if len(exact) == 0 {
			return nil, fmt.Errorf("Project has %d Backlog views and none matches the standard profile; refusing ambiguous replacement", len(backlogs))
		}
		canonical := exact[0]
		var changes []StandardBacklogViewChange
		for _, view := range backlogs {
			if view.ID == canonical.ID {
				continue
			}
			changes = append(changes, StandardBacklogViewChange{Action: "delete_duplicate_view", ViewID: view.ID, Detail: fmt.Sprintf("keep standard Backlog view #%d", canonical.Number)})
		}
		return changes, nil
	}
	view := backlogs[0]
	if backlogViewMatches(view, spec) {
		return nil, nil
	}
	if backlogViewGroupSortMatches(view, spec) {
		return []StandardBacklogViewChange{{Action: "update_view", ViewID: view.ID, Detail: "table/filter/visible fields"}}, nil
	}
	return []StandardBacklogViewChange{{Action: "replace_view", ViewID: view.ID, Detail: "provider API cannot update group/sort configuration in place"}}, nil
}

func planStandardBacklogViewFromState(project contract.Project, state backlogViewState) (StandardBacklogViewPlan, error) {
	changes, err := planBacklogViewChanges(state.views, state.spec)
	if err != nil {
		return StandardBacklogViewPlan{}, err
	}
	visible := make([]string, 0, len(state.spec.Visible))
	for _, field := range state.spec.Visible {
		visible = append(visible, field.Name)
	}
	return StandardBacklogViewPlan{
		Project:       ProjectIdentity{Owner: project.Owner, Number: project.Number, Title: project.Title},
		OwnerType:     state.ownerType,
		VisibleFields: visible,
		Changes:       changes,
	}, nil
}

// PlanStandardBacklogView inspects the live Project view and fields and returns
// only the changes required for the shared Backlog table.
func PlanStandardBacklogView(ctx context.Context, runner Runner, project contract.Project) (StandardBacklogViewPlan, error) {
	state, err := inspectStandardBacklogView(ctx, runner, project)
	if err != nil {
		return StandardBacklogViewPlan{}, err
	}
	return planStandardBacklogViewFromState(project, state)
}

func createBacklogView(ctx context.Context, runner Runner, project contract.Project, state backlogViewState) (string, error) {
	visible := make([]int, 0, len(state.spec.Visible))
	for _, field := range state.spec.Visible {
		visible = append(visible, field.RESTID)
	}
	body := map[string]any{
		"name":           standardBacklogViewName,
		"layout":         "table",
		"filter":         "",
		"visible_fields": visible,
		"sort_by":        []any{[]any{state.spec.Priority.RESTID, "asc"}},
		"group_by":       []int{state.spec.Status.RESTID},
	}
	endpoint := fmt.Sprintf("orgs/%s/projectsV2/%d/views", project.Owner, project.Number)
	if state.ownerType == "user" {
		out, err := runner.Run(ctx, "api", "users/"+project.Owner, "--jq", ".id")
		if err != nil {
			return "", fmt.Errorf("resolve user database ID for Project view creation: %w", err)
		}
		userID := strings.TrimSpace(string(out))
		if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
			return "", fmt.Errorf("invalid user database ID %q for %s", userID, project.Owner)
		}
		endpoint = fmt.Sprintf("users/%s/projectsV2/%d/views", userID, project.Number)
	}
	args := []string{"api", "--method", "POST"}
	args = append(args, apiHeaders()...)
	args = append(args, endpoint, "--input", "-")
	out, err := runJSONInput(ctx, runner, body, args...)
	if err != nil {
		return "", fmt.Errorf("create Backlog Project view: %w", err)
	}
	var response struct {
		Value struct {
			NodeID string `json:"node_id"`
		} `json:"value"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("decode created Backlog view: %w", err)
	}
	viewID := response.Value.NodeID
	if viewID == "" {
		viewID = response.NodeID
	}
	if viewID == "" {
		return "", fmt.Errorf("created Backlog view response did not include node_id")
	}
	return viewID, nil
}

func updateBacklogView(ctx context.Context, runner Runner, viewID string, spec backlogViewSpec) error {
	query := `mutation($input: UpdateProjectV2ViewInput!) {
  updateProjectV2View(input: $input) { projectV2View { id name layout filter } }
}`
	body := map[string]any{
		"query": query,
		"variables": map[string]any{"input": map[string]any{
			"viewId":        viewID,
			"name":          standardBacklogViewName,
			"layout":        "TABLE_LAYOUT",
			"filter":        "",
			"configuration": map[string]any{"visibleFieldIds": idsFromFields(spec.Visible)},
		}},
	}
	if _, err := runJSONInput(ctx, runner, body, "api", "graphql", "--input", "-"); err != nil {
		return fmt.Errorf("update Backlog Project view: %w", err)
	}
	return nil
}

func deleteProjectView(ctx context.Context, runner Runner, viewID string) error {
	query := `mutation($viewId: ID!) {
  deleteProjectV2View(input: {viewId: $viewId}) { projectV2View { id } }
}`
	body := map[string]any{"query": query, "variables": map[string]any{"viewId": viewID}}
	if _, err := runJSONInput(ctx, runner, body, "api", "graphql", "--input", "-"); err != nil {
		return fmt.Errorf("delete Project view %s: %w", viewID, err)
	}
	return nil
}

func findViewByID(views []projectViewNode, id string) (projectViewNode, bool) {
	for _, view := range views {
		if view.ID == id {
			return view, true
		}
	}
	return projectViewNode{}, false
}

func unrelatedViews(views []projectViewNode) map[string]projectViewNode {
	result := make(map[string]projectViewNode)
	for _, view := range views {
		if strings.EqualFold(strings.TrimSpace(view.Name), standardBacklogViewName) {
			continue
		}
		result[view.ID] = view
	}
	return result
}

func verifyUnrelatedViews(before map[string]projectViewNode, after []projectViewNode) error {
	afterMap := unrelatedViews(after)
	for id, old := range before {
		current, ok := afterMap[id]
		if !ok {
			return fmt.Errorf("unrelated Project view %q (%s) disappeared during Backlog reconciliation", old.Name, id)
		}
		if !reflect.DeepEqual(old, current) {
			return fmt.Errorf("unrelated Project view %q (%s) changed during Backlog reconciliation", old.Name, id)
		}
	}
	return nil
}

func verifyCreatedBacklog(ctx context.Context, runner Runner, project contract.Project, ownerType, viewID string, spec backlogViewSpec) error {
	views, err := queryProjectViews(ctx, runner, project, ownerType)
	if err != nil {
		return err
	}
	view, ok := findViewByID(views, viewID)
	if !ok {
		return fmt.Errorf("created Backlog view %s was not present in readback", viewID)
	}
	if !backlogViewMatches(view, spec) {
		return fmt.Errorf("created Backlog view %s did not read back with the standard configuration", viewID)
	}
	return nil
}

// ApplyStandardBacklogView performs a fresh stale-state check, applies only the
// standard Backlog-view delta and independently verifies the final state.
func ApplyStandardBacklogView(ctx context.Context, runner Runner, project contract.Project) (StandardBacklogViewResult, error) {
	initialState, err := inspectStandardBacklogView(ctx, runner, project)
	if err != nil {
		return StandardBacklogViewResult{}, err
	}
	initialPlan, err := planStandardBacklogViewFromState(project, initialState)
	if err != nil {
		return StandardBacklogViewResult{}, err
	}

	freshState, err := inspectStandardBacklogView(ctx, runner, project)
	if err != nil {
		return StandardBacklogViewResult{}, fmt.Errorf("fresh pre-write inspection: %w", err)
	}
	freshPlan, err := planStandardBacklogViewFromState(project, freshState)
	if err != nil {
		return StandardBacklogViewResult{}, err
	}
	if !reflect.DeepEqual(initialPlan, freshPlan) {
		return StandardBacklogViewResult{}, fmt.Errorf("Backlog view state changed after planning; inspect again before applying")
	}
	beforeUnrelated := unrelatedViews(freshState.views)

	for _, change := range freshPlan.Changes {
		switch change.Action {
		case "create_view":
			createdID, err := createBacklogView(ctx, runner, project, freshState)
			if err != nil {
				return StandardBacklogViewResult{}, err
			}
			if err := verifyCreatedBacklog(ctx, runner, project, freshState.ownerType, createdID, freshState.spec); err != nil {
				return StandardBacklogViewResult{}, err
			}
		case "update_view":
			if err := updateBacklogView(ctx, runner, change.ViewID, freshState.spec); err != nil {
				return StandardBacklogViewResult{}, err
			}
		case "replace_view":
			createdID, err := createBacklogView(ctx, runner, project, freshState)
			if err != nil {
				return StandardBacklogViewResult{}, err
			}
			if err := verifyCreatedBacklog(ctx, runner, project, freshState.ownerType, createdID, freshState.spec); err != nil {
				return StandardBacklogViewResult{}, fmt.Errorf("replacement Backlog view was created but could not be verified; old view was preserved: %w", err)
			}
			if err := deleteProjectView(ctx, runner, change.ViewID); err != nil {
				return StandardBacklogViewResult{}, fmt.Errorf("replacement Backlog view %s is verified but old view %s could not be removed; rerun reconciliation: %w", createdID, change.ViewID, err)
			}
		case "delete_duplicate_view":
			if err := deleteProjectView(ctx, runner, change.ViewID); err != nil {
				return StandardBacklogViewResult{}, err
			}
		default:
			return StandardBacklogViewResult{}, fmt.Errorf("unsupported Backlog view change %q", change.Action)
		}
	}

	verifiedState, err := inspectStandardBacklogView(ctx, runner, project)
	if err != nil {
		return StandardBacklogViewResult{}, fmt.Errorf("post-write inspection: %w", err)
	}
	verifiedPlan, err := planStandardBacklogViewFromState(project, verifiedState)
	if err != nil {
		return StandardBacklogViewResult{}, fmt.Errorf("post-write verification: %w", err)
	}
	if len(verifiedPlan.Changes) != 0 {
		return StandardBacklogViewResult{}, fmt.Errorf("Backlog view still requires %d changes after apply", len(verifiedPlan.Changes))
	}
	if len(backlogViews(verifiedState.views)) != 1 {
		return StandardBacklogViewResult{}, fmt.Errorf("Backlog view readback did not resolve to exactly one standard view")
	}
	if err := verifyUnrelatedViews(beforeUnrelated, verifiedState.views); err != nil {
		return StandardBacklogViewResult{}, err
	}
	return StandardBacklogViewResult{
		Project:       initialPlan.Project,
		OwnerType:     initialPlan.OwnerType,
		VisibleFields: initialPlan.VisibleFields,
		Applied:       freshPlan.Changes,
		Verified:      true,
	}, nil
}
