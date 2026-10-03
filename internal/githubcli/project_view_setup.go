package githubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

const standardBacklogViewName = "Backlog"

type StandardBacklogViewChange struct {
	Action string `json:"action"`
	ViewID string `json:"viewId,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type StandardBacklogViewPlan struct {
	Project       ProjectIdentity             `json:"project"`
	OwnerType     string                      `json:"ownerType"`
	VisibleFields []string                    `json:"visibleFields"`
	Changes       []StandardBacklogViewChange `json:"changes"`
}

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
	ownerType string
	views     []projectViewNode
	spec      backlogViewSpec
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
		return nil, fmt.Errorf("list Project fields: %w", err)
	}
	var pages [][]restProjectField
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("decode Project fields: %w", err)
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
		return nil, fmt.Errorf("Project %s/%d has more than 100 views; refusing an incomplete read", project.Owner, project.Number)
	}
	for _, view := range data.Views.Nodes {
		if view.Configuration.VisibleFields.PageInfo.HasNextPage || view.GroupByFields.PageInfo.HasNextPage || view.VerticalGroupByFields.PageInfo.HasNextPage || view.SortByFields.PageInfo.HasNextPage {
			return nil, fmt.Errorf("Project view %q has paginated configuration; refusing an incomplete read", view.Name)
		}
	}
	return data.Views.Nodes, nil
}

func buildBacklogViewSpec(project contract.Project, ownerType string, fields []restProjectField) (backlogViewSpec, error) {
	byName := make(map[string]restProjectField)
	byType := make(map[string][]restProjectField)
	for _, field := range fields {
		if field.ID == 0 || field.NodeID == "" {
			return backlogViewSpec{}, fmt.Errorf("Project field %q is missing a provider ID", field.Name)
		}
		key := strings.ToLower(strings.TrimSpace(field.Name))
		if _, exists := byName[key]; exists {
			return backlogViewSpec{}, fmt.Errorf("Project %s/%d has more than one field named %q", project.Owner, project.Number, field.Name)
		}
		byName[key] = field
		byType[strings.ToLower(field.DataType)] = append(byType[strings.ToLower(field.DataType)], field)
	}
	fromREST := func(field restProjectField) backlogField {
		return backlogField{Name: field.Name, RESTID: field.ID, NodeID: field.NodeID}
	}
	byExactName := func(name string, required bool) (backlogField, bool, error) {
		field, ok := byName[strings.ToLower(name)]
		if !ok {
			if required {
				return backlogField{}, false, fmt.Errorf("Project %s/%d is missing required field %q; run standard field setup first", project.Owner, project.Number, name)
			}
			return backlogField{}, false, nil
		}
		return fromREST(field), true, nil
	}
	byDataType := func(dataType string, required bool) (backlogField, bool, error) {
		matches := byType[strings.ToLower(dataType)]
		if len(matches) > 1 {
			return backlogField{}, false, fmt.Errorf("Project %s/%d has more than one %s field", project.Owner, project.Number, dataType)
		}
		if len(matches) == 0 {
			if required {
				return backlogField{}, false, fmt.Errorf("Project %s/%d is missing required %s field; run standard field setup first", project.Owner, project.Number, dataType)
			}
			return backlogField{}, false, nil
		}
		return fromREST(matches[0]), true, nil
	}

	type wantedField struct {
		name     string
		dataType string
		required bool
	}
	wanted := []wantedField{
		{dataType: "title", required: true},
		{name: "Status", required: true},
		{name: "Due date"},
		{name: "Target date"},
		{dataType: "assignees"},
		{dataType: "linked_pull_requests"},
		{dataType: "sub_issues_progress"},
		{name: "Priority", required: true},
	}
	if ownerType != "organization" {
		wanted = append(wanted, wantedField{name: "Class", required: true})
	}
	// Organisation Projects classify with native Issue Type, but GitHub's public
	// API cannot make that field visible in a Project view: the REST create
	// ignores it and the GraphQL update rejects it with "Visible fields must be
	// available in the view". It is therefore not part of the view spec and is
	// neither required nor located here.

	var spec backlogViewSpec
	for _, wantedField := range wanted {
		var field backlogField
		var ok bool
		var err error
		if wantedField.dataType != "" {
			field, ok, err = byDataType(wantedField.dataType, wantedField.required)
		} else {
			field, ok, err = byExactName(wantedField.name, wantedField.required)
		}
		if err != nil {
			return backlogViewSpec{}, err
		}
		if !ok {
			continue
		}
		spec.Visible = append(spec.Visible, field)
		if strings.EqualFold(field.Name, "Status") {
			spec.Status = field
		}
		if strings.EqualFold(field.Name, "Priority") {
			spec.Priority = field
		}
	}
	return spec, nil
}

func inspectStandardBacklogView(ctx context.Context, runner Runner, project contract.Project) (backlogViewState, error) {
	ownerType := project.OwnerType
	if ownerType == "" {
		var err error
		ownerType, err = discoverOwnerType(ctx, runner, project.Owner)
		if err != nil {
			return backlogViewState{}, fmt.Errorf("discover Project owner type: %w", err)
		}
	}
	if ownerType != "user" && ownerType != "organization" {
		return backlogViewState{}, fmt.Errorf("unsupported Project owner type %q", ownerType)
	}
	fields, err := queryRESTProjectFields(ctx, runner, project, ownerType)
	if err != nil {
		return backlogViewState{}, err
	}
	spec, err := buildBacklogViewSpec(project, ownerType, fields)
	if err != nil {
		return backlogViewState{}, err
	}
	views, err := queryProjectViews(ctx, runner, project, ownerType)
	if err != nil {
		return backlogViewState{}, err
	}
	return backlogViewState{ownerType: ownerType, views: views, spec: spec}, nil
}

func fieldNodeIDs(fields []backlogField) []string {
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		ids = append(ids, field.NodeID)
	}
	return ids
}

func viewFieldNodeIDs(fields []projectViewFieldRef) []string {
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		ids = append(ids, field.ID)
	}
	return ids
}

// sameFieldSet compares visible fields as an unordered set: GitHub returns a
// view's visible fields in its own display order rather than the requested one.
func sameFieldSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func backlogViewGroupSortMatches(view projectViewNode, spec backlogViewSpec) bool {
	if len(view.GroupByFields.Nodes) != 1 || view.GroupByFields.Nodes[0].ID != spec.Status.NodeID || len(view.VerticalGroupByFields.Nodes) != 0 || len(view.SortByFields.Nodes) != 1 {
		return false
	}
	sortBy := view.SortByFields.Nodes[0]
	return sortBy.Field.ID == spec.Priority.NodeID && strings.EqualFold(sortBy.Direction, "ASC")
}

func backlogViewMatchesBasic(view projectViewNode, spec backlogViewSpec) bool {
	filterEmpty := view.Filter == nil || strings.TrimSpace(*view.Filter) == ""
	return strings.EqualFold(view.Layout, "TABLE_LAYOUT") && filterEmpty && sameFieldSet(viewFieldNodeIDs(view.Configuration.VisibleFields.Nodes), fieldNodeIDs(spec.Visible))
}

func backlogViewMatches(view projectViewNode, spec backlogViewSpec) bool {
	return backlogViewMatchesBasic(view, spec) && backlogViewGroupSortMatches(view, spec)
}

func findBacklogViews(views []projectViewNode) []projectViewNode {
	var matches []projectViewNode
	for _, view := range views {
		if strings.EqualFold(strings.TrimSpace(view.Name), standardBacklogViewName) {
			matches = append(matches, view)
		}
	}
	return matches
}

func viewLabel(view projectViewNode) string {
	return fmt.Sprintf("Backlog #%d (%s)", view.Number, view.ID)
}

func fieldRefNames(fields []projectViewFieldRef) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

func describeGroupSort(view projectViewNode) (string, string) {
	group := "none"
	if names := fieldRefNames(view.GroupByFields.Nodes); len(names) > 0 {
		group = strings.Join(names, ", ")
	}
	if names := fieldRefNames(view.VerticalGroupByFields.Nodes); len(names) > 0 {
		group += " (vertical group " + strings.Join(names, ", ") + ")"
	}
	sortBy := "none"
	if len(view.SortByFields.Nodes) > 0 {
		parts := make([]string, 0, len(view.SortByFields.Nodes))
		for _, node := range view.SortByFields.Nodes {
			parts = append(parts, node.Field.Name+" "+strings.ToLower(node.Direction))
		}
		sortBy = strings.Join(parts, ", ")
	}
	return group, sortBy
}

// describeBasicViewDiff lists the layout, filter and visible-field differences
// between a live view and the standard spec.
func describeBasicViewDiff(view projectViewNode, spec backlogViewSpec) []string {
	var parts []string
	if !strings.EqualFold(view.Layout, "TABLE_LAYOUT") {
		parts = append(parts, fmt.Sprintf("layout %s->TABLE_LAYOUT", view.Layout))
	}
	if view.Filter != nil && strings.TrimSpace(*view.Filter) != "" {
		parts = append(parts, fmt.Sprintf("filter %q->none", *view.Filter))
	}
	current := make(map[string]bool)
	for _, field := range view.Configuration.VisibleFields.Nodes {
		current[field.ID] = true
	}
	desired := make(map[string]bool)
	var show []string
	for _, field := range spec.Visible {
		desired[field.NodeID] = true
		if !current[field.NodeID] {
			show = append(show, field.Name)
		}
	}
	var hide []string
	for _, field := range view.Configuration.VisibleFields.Nodes {
		if !desired[field.ID] {
			hide = append(hide, field.Name)
		}
	}
	if len(show) > 0 {
		parts = append(parts, "show "+strings.Join(show, ", "))
	}
	if len(hide) > 0 {
		parts = append(parts, "hide "+strings.Join(hide, ", "))
	}
	return parts
}

func planBacklogViewChanges(views []projectViewNode, spec backlogViewSpec) ([]StandardBacklogViewChange, error) {
	backlogs := findBacklogViews(views)
	if len(backlogs) == 0 {
		return []StandardBacklogViewChange{{Action: "create_view", Detail: fmt.Sprintf("table; no filter; group %s; sort %s ascending", spec.Status.Name, spec.Priority.Name)}}, nil
	}
	if len(backlogs) > 1 {
		var matching, other []projectViewNode
		labels := make([]string, 0, len(backlogs))
		for _, view := range backlogs {
			labels = append(labels, viewLabel(view))
			if backlogViewMatches(view, spec) {
				matching = append(matching, view)
			} else {
				other = append(other, view)
			}
		}
		// Exactly two Backlogs where one is already standard is the state an
		// interrupted replacement leaves behind; finishing it is unambiguous.
		if len(backlogs) == 2 && len(matching) == 1 {
			return []StandardBacklogViewChange{{Action: "delete_view", ViewID: other[0].ID, Detail: fmt.Sprintf("delete non-standard duplicate %s; keep standard %s", viewLabel(other[0]), viewLabel(matching[0]))}}, nil
		}
		return nil, fmt.Errorf("Project has %d Backlog views (%s); refusing ambiguous reconciliation: rename or delete all but one Backlog view in the GitHub Project, then rerun", len(backlogs), strings.Join(labels, ", "))
	}
	view := backlogs[0]
	if backlogViewMatches(view, spec) {
		return []StandardBacklogViewChange{}, nil
	}
	basic := describeBasicViewDiff(view, spec)
	if backlogViewGroupSortMatches(view, spec) {
		return []StandardBacklogViewChange{{Action: "update_view", ViewID: view.ID, Detail: fmt.Sprintf("update %s in place: %s", viewLabel(view), strings.Join(basic, "; "))}}, nil
	}
	group, sortBy := describeGroupSort(view)
	parts := []string{
		fmt.Sprintf("group %s->%s", group, spec.Status.Name),
		fmt.Sprintf("sort %s->%s asc", sortBy, spec.Priority.Name),
	}
	parts = append(parts, basic...)
	return []StandardBacklogViewChange{{Action: "replace_view", ViewID: view.ID, Detail: fmt.Sprintf("%s; GitHub API cannot update group/sort configuration in place, so create a standard replacement and delete old %s after verifying the replacement", strings.Join(parts, "; "), viewLabel(view))}}, nil
}

func planStandardBacklogViewFromState(project contract.Project, state backlogViewState) (StandardBacklogViewPlan, error) {
	changes, err := planBacklogViewChanges(state.views, state.spec)
	if err != nil {
		return StandardBacklogViewPlan{}, err
	}
	if changes == nil {
		changes = []StandardBacklogViewChange{}
	}
	visible := make([]string, 0, len(state.spec.Visible))
	for _, field := range state.spec.Visible {
		visible = append(visible, field.Name)
	}
	return StandardBacklogViewPlan{Project: ProjectIdentity{Owner: project.Owner, Number: project.Number, Title: project.Title}, OwnerType: state.ownerType, VisibleFields: visible, Changes: changes}, nil
}

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
		"name": standardBacklogViewName, "layout": "table", "filter": "",
		"visible_fields": visible,
		"sort_by":        []any{[]any{state.spec.Priority.RESTID, "asc"}},
		"group_by":       []int{state.spec.Status.RESTID},
	}
	endpoint := fmt.Sprintf("orgs/%s/projectsV2/%d/views", project.Owner, project.Number)
	if state.ownerType == "user" {
		// GitHub requires the account login here. The numeric database ID is
		// not accepted and returns 404.
		endpoint = fmt.Sprintf("users/%s/projectsV2/%d/views", project.Owner, project.Number)
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
	body := map[string]any{"query": query, "variables": map[string]any{"input": map[string]any{
		"viewId": viewID, "name": standardBacklogViewName, "layout": "TABLE_LAYOUT", "filter": "",
		"configuration": map[string]any{"visibleFieldIds": fieldNodeIDs(spec.Visible)},
	}}}
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

func unrelatedViewIDs(views []projectViewNode) map[string]string {
	result := make(map[string]string)
	for _, view := range views {
		if !strings.EqualFold(strings.TrimSpace(view.Name), standardBacklogViewName) {
			result[view.ID] = view.Name
		}
	}
	return result
}

func verifyUnrelatedViews(before map[string]string, after []projectViewNode) error {
	afterIDs := unrelatedViewIDs(after)
	for id, name := range before {
		if afterIDs[id] != name {
			return fmt.Errorf("unrelated Project view %q (%s) changed or disappeared during Backlog reconciliation", name, id)
		}
	}
	return nil
}

func verifyBacklogByID(views []projectViewNode, id string, spec backlogViewSpec) error {
	view, ok := findViewByID(views, id)
	if !ok || !backlogViewMatches(view, spec) {
		return fmt.Errorf("replacement Backlog view %s did not read back with the standard configuration", id)
	}
	return nil
}

func viewChangeLabel(change StandardBacklogViewChange) string {
	if change.ViewID == "" {
		return change.Action
	}
	return change.Action + " " + change.ViewID
}

func manualViewDeleteStep(viewID string) string {
	return fmt.Sprintf("delete it in the GitHub Project UI (view tab menu > Delete view) or run: gh api graphql -f query='mutation { deleteProjectV2View(input: {viewId: \"%s\"}) { projectV2View { id } } }'", viewID)
}

// replaceBacklogView creates a standard replacement, verifies it and only then
// deletes the old view. A replacement that cannot be verified is removed again
// so the Project is never left with two Backlog views that block later runs.
func replaceBacklogView(ctx context.Context, runner Runner, project contract.Project, state backlogViewState, change StandardBacklogViewChange) error {
	oldLabel := change.ViewID
	if old, ok := findViewByID(state.views, change.ViewID); ok {
		oldLabel = viewLabel(old)
	}
	createdID, err := createBacklogView(ctx, runner, project, state)
	if err != nil {
		return fmt.Errorf("old %s was preserved: %w", oldLabel, err)
	}
	createdLabel := "replacement Backlog (" + createdID + ")"
	views, verifyErr := queryProjectViews(ctx, runner, project, state.ownerType)
	if verifyErr != nil {
		verifyErr = fmt.Errorf("re-read Project views: %w", verifyErr)
	} else {
		if created, ok := findViewByID(views, createdID); ok {
			createdLabel = "replacement " + viewLabel(created)
		}
		verifyErr = verifyBacklogByID(views, createdID, state.spec)
	}
	if verifyErr != nil {
		if cleanupErr := deleteProjectView(ctx, runner, createdID); cleanupErr != nil {
			return fmt.Errorf("%s could not be verified (%v) and removing it also failed (%v); the Project now has two Backlog views: original %s and %s. Manually remove the replacement: %s; then rerun setup-backlog-view", createdLabel, verifyErr, cleanupErr, oldLabel, createdLabel, manualViewDeleteStep(createdID))
		}
		return fmt.Errorf("%s could not be verified, so it was deleted again and original %s was preserved: %w", createdLabel, oldLabel, verifyErr)
	}
	if err := deleteProjectView(ctx, runner, change.ViewID); err != nil {
		return fmt.Errorf("%s is verified but old %s could not be removed (%w); rerun setup-backlog-view --apply, which will plan deleting the non-standard duplicate, or %s", createdLabel, oldLabel, err, manualViewDeleteStep(change.ViewID))
	}
	return nil
}

func ApplyStandardBacklogView(ctx context.Context, runner Runner, project contract.Project) (StandardBacklogViewResult, error) {
	state, err := inspectStandardBacklogView(ctx, runner, project)
	if err != nil {
		return StandardBacklogViewResult{}, err
	}
	plan, err := planStandardBacklogViewFromState(project, state)
	if err != nil {
		return StandardBacklogViewResult{}, err
	}
	beforeUnrelated := unrelatedViewIDs(state.views)
	result := StandardBacklogViewResult{Project: plan.Project, OwnerType: plan.OwnerType, VisibleFields: plan.VisibleFields, Applied: []StandardBacklogViewChange{}}
	var appliedLabels []string
	fail := func(failedAt string, err error) (StandardBacklogViewResult, error) {
		return result, partialApplyError(appliedLabels, failedAt, err)
	}

	for _, change := range plan.Changes {
		var err error
		switch change.Action {
		case "create_view":
			_, err = createBacklogView(ctx, runner, project, state)
		case "update_view":
			err = updateBacklogView(ctx, runner, change.ViewID, state.spec)
		case "replace_view":
			err = replaceBacklogView(ctx, runner, project, state, change)
		case "delete_view":
			err = deleteProjectView(ctx, runner, change.ViewID)
		default:
			err = fmt.Errorf("unsupported Backlog view change %q", change.Action)
		}
		if err != nil {
			return fail(viewChangeLabel(change), err)
		}
		result.Applied = append(result.Applied, change)
		appliedLabels = append(appliedLabels, viewChangeLabel(change))
	}

	// The owner type is already known; do not rediscover it during readback.
	readProject := project
	readProject.OwnerType = state.ownerType
	verified, err := inspectStandardBacklogView(ctx, runner, readProject)
	if err != nil {
		return fail("readback", fmt.Errorf("post-write inspection: %w", err))
	}
	verifiedPlan, err := planStandardBacklogViewFromState(project, verified)
	if err != nil {
		return fail("readback", fmt.Errorf("post-write verification: %w", err))
	}
	if len(verifiedPlan.Changes) != 0 || len(findBacklogViews(verified.views)) != 1 {
		return fail("readback", fmt.Errorf("Backlog view did not read back as one standard view"))
	}
	if err := verifyUnrelatedViews(beforeUnrelated, verified.views); err != nil {
		return fail("readback", err)
	}
	result.Verified = true
	return result, nil
}
