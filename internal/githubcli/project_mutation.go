package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

// ProjectSchema holds the discovered GraphQL schema for a ProjectV2.
type ProjectSchema struct {
	ID     string
	Number int
	Title  string
	Fields map[string]ProjectField // lowercased field name -> field
}

// ProjectField holds one field's definition and its single-select options.
type ProjectField struct {
	ID       string
	Name     string
	DataType string
	Options  map[string]string // lowercased option name -> option ID
}

// FindField looks up a field by name (case-insensitive).
func (s ProjectSchema) FindField(name string) (ProjectField, bool) {
	field, ok := s.Fields[strings.ToLower(strings.TrimSpace(name))]
	return field, ok
}

// FindOptionID looks up a single-select option ID by name (case-insensitive).
func (f ProjectField) FindOptionID(name string) (string, bool) {
	if f.Options == nil {
		return "", false
	}
	id, ok := f.Options[strings.ToLower(strings.TrimSpace(name))]
	return id, ok
}

// ProjectItemState contains the inspected fields for an item in a Project.
type ProjectItemState struct {
	ItemID      string            `json:"itemId"`
	IssueNumber int               `json:"issueNumber,omitempty"`
	URL         string            `json:"url"`
	Fields      map[string]string `json:"fields"`
	raw         map[string]json.RawMessage
}

type graphQLFieldNode struct {
	Typename string `json:"__typename"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Options  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"options"`
}

type graphQLProjectData struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Fields struct {
		Nodes    []graphQLFieldNode `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"fields"`
}

type graphQLSchemaResponse struct {
	Data struct {
		User struct {
			ProjectV2 *graphQLProjectData `json:"projectV2"`
		} `json:"user"`
		Organization struct {
			ProjectV2 *graphQLProjectData `json:"projectV2"`
		} `json:"organization"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ProjectSchemaQuery returns the GraphQL query for fetching a ProjectV2's fields.
func ProjectSchemaQuery(ownerRoot string) string {
	return fmt.Sprintf(`query($login: String!, $number: Int!) {
  %s(login: $login) {
    projectV2(number: $number) {
      id
      number
      title
      fields(first: 100) {
        nodes {
          __typename
          ... on ProjectV2FieldCommon { id name dataType }
          ... on ProjectV2SingleSelectField { options { id name } }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, ownerRoot)
}

// QueryProjectSchema fetches the Project node ID, fields, and options via GraphQL.
func QueryProjectSchema(ctx context.Context, runner Runner, project contract.Project) (ProjectSchema, error) {
	ownerRoot := project.OwnerType
	if ownerRoot == "" {
		discovered, err := discoverOwnerType(ctx, runner, project.Owner)
		if err != nil {
			return ProjectSchema{}, fmt.Errorf("discover owner type for %s: %w", project.Owner, err)
		}
		ownerRoot = discovered
	}

	query := ProjectSchemaQuery(ownerRoot)

	output, err := runner.Run(
		ctx,
		"api", "graphql",
		"-f", "query="+query,
		"-f", "login="+project.Owner,
		"-F", "number="+strconv.Itoa(project.Number),
	)
	if err != nil {
		return ProjectSchema{}, fmt.Errorf("query Project schema for %s/%d: %w", project.Owner, project.Number, err)
	}

	var resp graphQLSchemaResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return ProjectSchema{}, fmt.Errorf("decode Project schema response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return ProjectSchema{}, fmt.Errorf("GraphQL error querying Project schema: %s", resp.Errors[0].Message)
	}

	var data *graphQLProjectData
	if ownerRoot == "organization" {
		data = resp.Data.Organization.ProjectV2
	} else {
		data = resp.Data.User.ProjectV2
	}
	if data == nil {
		return ProjectSchema{}, fmt.Errorf("Project %s/%d not found at %s root", project.Owner, project.Number, ownerRoot)
	}
	if data.Number != project.Number || data.Title != project.Title {
		return ProjectSchema{}, fmt.Errorf(
			"Project identity disagrees with %s: got number %d title %q, want number %d title %q",
			project.ContractPath,
			data.Number,
			data.Title,
			project.Number,
			project.Title,
		)
	}
	if data.Fields.PageInfo.HasNextPage {
		return ProjectSchema{}, fmt.Errorf("Project %s/%d has more than 100 fields; refusing an incomplete schema", project.Owner, project.Number)
	}

	schema := ProjectSchema{
		ID:     data.ID,
		Number: data.Number,
		Title:  data.Title,
		Fields: make(map[string]ProjectField),
	}
	for _, node := range data.Fields.Nodes {
		f := ProjectField{
			ID:       node.ID,
			Name:     node.Name,
			DataType: node.DataType,
			Options:  make(map[string]string),
		}
		for _, opt := range node.Options {
			f.Options[strings.ToLower(strings.TrimSpace(opt.Name))] = opt.ID
		}
		schema.Fields[strings.ToLower(strings.TrimSpace(node.Name))] = f
	}

	return schema, nil
}

func discoverOwnerType(ctx context.Context, runner Runner, owner string) (string, error) {
	output, err := runner.Run(ctx, "api", "users/"+owner, "--jq", ".type")
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(string(output))
	switch trimmed {
	case "User":
		return "user", nil
	case "Organization":
		return "organization", nil
	default:
		return "", fmt.Errorf("unsupported owner type %q for %s", trimmed, owner)
	}
}

// AddProjectItem adds an issue or pull request to a Project via gh project item-add.
func AddProjectItem(ctx context.Context, runner Runner, projectNumber int, projectOwner, url string) (string, error) {
	output, err := runner.Run(
		ctx,
		"project", "item-add", strconv.Itoa(projectNumber),
		"--owner", projectOwner,
		"--url", url,
		"--format", "json",
	)
	if err != nil {
		return "", fmt.Errorf("add item to Project %s/%d: %w", projectOwner, projectNumber, err)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &res); err != nil {
		return "", fmt.Errorf("decode item-add response: %w", err)
	}
	if res.ID == "" {
		return "", fmt.Errorf("item-add returned empty item ID for %s", url)
	}
	return res.ID, nil
}

// GitHubItemTarget is a canonical issue or pull-request target.
type GitHubItemTarget struct {
	Repository string `json:"repository"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Kind       string `json:"kind"`
	Number     int    `json:"number"`
	URL        string `json:"url"`
}

// ResolveGitHubItemTarget validates a contract repository and either an issue
// number or an exact github.com issue/pull URL. A URL must agree with the
// contract repository; it cannot silently redirect a contract-bound command.
func ResolveGitHubItemTarget(repository string, issueNumber int, rawURL string) (GitHubItemTarget, error) {
	owner, repo, err := splitGitHubRepository(repository)
	if err != nil {
		return GitHubItemTarget{}, err
	}
	if rawURL == "" {
		if issueNumber <= 0 {
			return GitHubItemTarget{}, errors.New("an issue number or URL is required")
		}
		return GitHubItemTarget{
			Repository: repository,
			Owner:      owner,
			Repo:       repo,
			Kind:       "issues",
			Number:     issueNumber,
			URL:        fmt.Sprintf("https://github.com/%s/issues/%d", repository, issueNumber),
		}, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return GitHubItemTarget{}, fmt.Errorf("parse item URL: %w", err)
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return GitHubItemTarget{}, fmt.Errorf("item URL must be an exact https://github.com issue or pull-request URL: %q", rawURL)
	}
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) != 4 || (pathParts[2] != "issues" && pathParts[2] != "pull") {
		return GitHubItemTarget{}, fmt.Errorf("item URL must identify one GitHub issue or pull request: %q", rawURL)
	}
	number, err := strconv.Atoi(pathParts[3])
	if err != nil || number <= 0 {
		return GitHubItemTarget{}, fmt.Errorf("item URL has an invalid issue or pull-request number: %q", rawURL)
	}
	urlRepo := pathParts[0] + "/" + pathParts[1]
	if !strings.EqualFold(urlRepo, repository) {
		return GitHubItemTarget{}, fmt.Errorf("item URL repository %s disagrees with contract repository %s", urlRepo, repository)
	}
	if issueNumber > 0 && (pathParts[2] != "issues" || issueNumber != number) {
		return GitHubItemTarget{}, fmt.Errorf("item URL disagrees with issue number %d", issueNumber)
	}
	return GitHubItemTarget{
		Repository: repository,
		Owner:      pathParts[0],
		Repo:       pathParts[1],
		Kind:       pathParts[2],
		Number:     number,
		URL:        fmt.Sprintf("https://github.com/%s/%s/%s/%d", pathParts[0], pathParts[1], pathParts[2], number),
	}, nil
}

func splitGitHubRepository(repository string) (string, string, error) {
	repoParts := strings.Split(repository, "/")
	if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
		return "", "", fmt.Errorf("invalid contract repository %q; expected owner/name", repository)
	}
	return repoParts[0], repoParts[1], nil
}

type graphQLProjectItemFieldValue struct {
	Typename    string   `json:"__typename"`
	Date        *string  `json:"date"`
	Title       *string  `json:"title"`
	IterationID *string  `json:"iterationId"`
	Value       *string  `json:"value"`
	Number      *float64 `json:"number"`
	Name        *string  `json:"name"`
	OptionID    *string  `json:"optionId"`
	Text        *string  `json:"text"`
	Field       struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		DataType string `json:"dataType"`
	} `json:"field"`
}

type graphQLProjectItemNode struct {
	ID         string `json:"id"`
	IsArchived bool   `json:"isArchived"`
	Project    struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
		Title  string `json:"title"`
		Owner  struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"project"`
	FieldValues struct {
		Nodes    []graphQLProjectItemFieldValue `json:"nodes"`
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
	} `json:"fieldValues"`
}

type graphQLProjectItemResponse struct {
	Data struct {
		Repository *struct {
			Target *struct {
				URL          string `json:"url"`
				ProjectItems struct {
					Nodes    []graphQLProjectItemNode `json:"nodes"`
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
				} `json:"projectItems"`
			} `json:"target"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ProjectItemQuery returns a target-centred GraphQL query. It reads only the
// Projects containing one issue or pull request, rather than serialising every
// item in the selected Project. Scalar Project fields are included so callers
// can verify requested values and preservation with one bounded readback.
func ProjectItemQuery(kind string) string {
	targetField := ""
	switch kind {
	case "issues":
		targetField = "issue"
	case "pull":
		targetField = "pullRequest"
	default:
		return ""
	}
	return fmt.Sprintf(`query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    target: %s(number: $number) {
      url
      projectItems(first: 100) {
        nodes {
          id
          isArchived
          project {
            id
            number
            title
            owner {
              ... on User { login }
              ... on Organization { login }
            }
          }
          fieldValues(first: 100) {
            nodes {
              __typename
              ... on ProjectV2ItemFieldDateValue {
                date
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
              ... on ProjectV2ItemFieldIterationValue {
                title
                iterationId
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
              ... on ProjectV2ItemFieldMultiSelectValue {
                value
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
              ... on ProjectV2ItemFieldNumberValue {
                number
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
              ... on ProjectV2ItemFieldSingleSelectValue {
                name
                optionId
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
              ... on ProjectV2ItemFieldTextValue {
                text
                field { ... on ProjectV2FieldCommon { id name dataType } }
              }
            }
            pageInfo { hasNextPage }
          }
        }
        pageInfo { hasNextPage }
      }
    }
  }
}`, targetField)
}

// QueryProjectItem resolves membership and current scalar Project fields from
// the target issue or pull request. The query is bounded to at most 100 Project
// memberships and 100 set field values per membership, and refuses to treat a
// truncated response as proof.
func QueryProjectItem(ctx context.Context, runner Runner, project contract.Project, target GitHubItemTarget) (*ProjectItemState, error) {
	query := ProjectItemQuery(target.Kind)
	if query == "" {
		return nil, fmt.Errorf("unsupported GitHub item kind %q", target.Kind)
	}
	output, err := runner.Run(
		ctx,
		"api", "graphql",
		"-f", "query="+query,
		"-f", "owner="+target.Owner,
		"-f", "repo="+target.Repo,
		"-F", "number="+strconv.Itoa(target.Number),
	)
	if err != nil {
		return nil, fmt.Errorf("query Project membership for %s: %w", target.URL, err)
	}

	var response graphQLProjectItemResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode Project membership for %s: %w", target.URL, err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error querying Project membership: %s", response.Errors[0].Message)
	}
	if response.Data.Repository == nil || response.Data.Repository.Target == nil {
		return nil, fmt.Errorf("GitHub target %s was not found or is not accessible", target.URL)
	}
	observedTarget := response.Data.Repository.Target
	if !strings.EqualFold(observedTarget.URL, target.URL) {
		return nil, fmt.Errorf("GitHub target identity disagrees: got %s, want %s", observedTarget.URL, target.URL)
	}
	if observedTarget.ProjectItems.PageInfo.HasNextPage {
		return nil, fmt.Errorf("%s belongs to more than 100 Projects; refusing an incomplete membership read", target.URL)
	}

	var match *graphQLProjectItemNode
	for index := range observedTarget.ProjectItems.Nodes {
		node := &observedTarget.ProjectItems.Nodes[index]
		if !strings.EqualFold(node.Project.Owner.Login, project.Owner) || node.Project.Number != project.Number {
			continue
		}
		if node.Project.Title != project.Title {
			return nil, fmt.Errorf(
				"Project identity disagrees with %s: got title %q, want %q",
				project.ContractPath,
				node.Project.Title,
				project.Title,
			)
		}
		if match != nil {
			return nil, fmt.Errorf("Project %s/%d contains more than one item for %s", project.Owner, project.Number, target.URL)
		}
		match = node
	}
	if match == nil {
		return nil, nil
	}
	if match.ID == "" || match.Project.ID == "" {
		return nil, fmt.Errorf("Project item for %s has incomplete node identity", target.URL)
	}
	if match.FieldValues.PageInfo.HasNextPage {
		return nil, fmt.Errorf("Project item %s has more than 100 set fields; refusing an incomplete field read", match.ID)
	}

	state, err := decodeGraphQLProjectItem(*match, target)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func decodeGraphQLProjectItem(node graphQLProjectItemNode, target GitHubItemTarget) (ProjectItemState, error) {
	fields := make(map[string]string)
	rawFields := make(map[string]json.RawMessage)
	rawFields["meta:id"] = mustMarshalProjectValue(node.ID)
	rawFields["meta:content"] = mustMarshalProjectValue(map[string]any{
		"number":     target.Number,
		"repository": target.Repository,
		"type":       target.Kind,
		"url":        target.URL,
	})
	rawFields["meta:project"] = mustMarshalProjectValue(map[string]any{
		"id":     node.Project.ID,
		"number": node.Project.Number,
		"owner":  node.Project.Owner.Login,
		"title":  node.Project.Title,
	})
	rawFields["meta:isarchived"] = mustMarshalProjectValue(node.IsArchived)
	seenFields := make(map[string]struct{})

	for _, value := range node.FieldValues.Nodes {
		if value.Field.Name == "" {
			// Built-in list fields (labels, assignees, reviewers and similar)
			// are not writable through this CLI's scalar field mutation path.
			continue
		}
		fieldKey := strings.ToLower(strings.TrimSpace(value.Field.Name))
		if _, exists := seenFields[fieldKey]; exists {
			return ProjectItemState{}, fmt.Errorf("Project item %s has duplicate set field name %q", node.ID, value.Field.Name)
		}
		seenFields[fieldKey] = struct{}{}

		scalar, present := graphQLProjectScalar(value)
		canonical := map[string]any{
			"dataType": value.Field.DataType,
			"fieldId":  value.Field.ID,
			"type":     value.Typename,
		}
		if present {
			canonical["value"] = scalar
			fields[value.Field.Name] = scalar
		} else {
			canonical["value"] = nil
		}
		if value.OptionID != nil {
			canonical["optionId"] = *value.OptionID
		}
		if value.IterationID != nil {
			canonical["iterationId"] = *value.IterationID
		}
		rawFields["field:"+fieldKey] = mustMarshalProjectValue(canonical)
	}

	return ProjectItemState{
		ItemID:      node.ID,
		IssueNumber: target.Number,
		URL:         target.URL,
		Fields:      fields,
		raw:         rawFields,
	}, nil
}

func graphQLProjectScalar(value graphQLProjectItemFieldValue) (string, bool) {
	var raw any
	switch value.Typename {
	case "ProjectV2ItemFieldDateValue":
		if value.Date == nil {
			return "", false
		}
		raw = *value.Date
	case "ProjectV2ItemFieldIterationValue":
		if value.Title == nil {
			return "", false
		}
		raw = *value.Title
	case "ProjectV2ItemFieldMultiSelectValue":
		if value.Value == nil {
			return "", false
		}
		raw = *value.Value
	case "ProjectV2ItemFieldNumberValue":
		if value.Number == nil {
			return "", false
		}
		raw = *value.Number
	case "ProjectV2ItemFieldSingleSelectValue":
		if value.Name == nil {
			return "", false
		}
		raw = *value.Name
	case "ProjectV2ItemFieldTextValue":
		if value.Text == nil {
			return "", false
		}
		raw = *value.Text
	default:
		return "", false
	}
	encoded, _ := json.Marshal(raw)
	if stringValue, ok := raw.(string); ok {
		return stringValue, true
	}
	return string(encoded), true
}

func mustMarshalProjectValue(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

// EnsureProjectItem adds a missing item and proves membership with a separate,
// target-centred read. Existing membership is an idempotent no-op.
func EnsureProjectItem(ctx context.Context, runner Runner, project contract.Project, target GitHubItemTarget) (ProjectItemState, bool, error) {
	before, err := QueryProjectItem(ctx, runner, project, target)
	if err != nil {
		return ProjectItemState{}, false, fmt.Errorf("inspect Project membership: %w", err)
	}
	if before != nil {
		return *before, false, nil
	}

	itemID, err := AddProjectItem(ctx, runner, project.Number, project.Owner, target.URL)
	if err != nil {
		return ProjectItemState{}, false, err
	}
	after, err := QueryProjectItem(ctx, runner, project, target)
	if err != nil {
		return ProjectItemState{}, true, fmt.Errorf("read back added Project item: %w", err)
	}
	if after == nil {
		return ProjectItemState{}, true, fmt.Errorf("Project item readback failed: %s is not in Project %s/%d", target.URL, project.Owner, project.Number)
	}
	if after.ItemID != itemID {
		return ProjectItemState{}, true, fmt.Errorf("Project item ID readback disagrees: add returned %s, target read found %s", itemID, after.ItemID)
	}
	return *after, true, nil
}

// EditItemFieldInput specifies parameters for editing a Project item field.
type EditItemFieldInput struct {
	ItemID               string
	ProjectNodeID        string
	FieldID              string
	SingleSelectOptionID string
	DateValue            string
	TextValue            string
	Clear                bool
}

// EditProjectItemField calls gh project item-edit to change a single field.
func EditProjectItemField(ctx context.Context, runner Runner, input EditItemFieldInput) error {
	args := []string{
		"project", "item-edit",
		"--id", input.ItemID,
		"--project-id", input.ProjectNodeID,
		"--field-id", input.FieldID,
	}
	if input.Clear {
		args = append(args, "--clear")
	} else if input.SingleSelectOptionID != "" {
		args = append(args, "--single-select-option-id", input.SingleSelectOptionID)
	} else if input.DateValue != "" {
		args = append(args, "--date", input.DateValue)
	} else if input.TextValue != "" {
		args = append(args, "--text", input.TextValue)
	} else {
		return errors.New("edit item field requires a value or --clear")
	}

	_, err := runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("edit Project item field: %w", err)
	}
	return nil
}

const githubAPIVersion = "2026-03-10"

type organizationIssueField struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Options  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"options"`
}

type repositoryIssueType struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type issueFieldValue struct {
	IssueFieldID   int             `json:"issue_field_id"`
	IssueFieldName string          `json:"issue_field_name"`
	DataType       string          `json:"data_type"`
	Value          json.RawMessage `json:"value"`
	SingleSelect   *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"single_select_option"`
}

func apiHeaders() []string {
	return []string{
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: " + githubAPIVersion,
	}
}

func queryOrganizationIssueFields(ctx context.Context, runner Runner, owner string) ([]organizationIssueField, error) {
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, "orgs/"+owner+"/issue-fields?per_page=100")
	output, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list organization issue fields for %s: %w", owner, err)
	}
	var pages [][]organizationIssueField
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode organization issue fields: %w", err)
	}
	var result []organizationIssueField
	for _, page := range pages {
		result = append(result, page...)
	}
	return result, nil
}

func resolveOrganizationIssueField(fields []organizationIssueField, owner, name, dataType, desired string) (organizationIssueField, error) {
	var matches []organizationIssueField
	for _, field := range fields {
		if field.Name == name {
			matches = append(matches, field)
		}
	}
	if len(matches) != 1 {
		return organizationIssueField{}, fmt.Errorf("organization issue field %q resolved to %d exact matches for %s", name, len(matches), owner)
	}
	field := matches[0]
	if field.DataType != dataType {
		return organizationIssueField{}, fmt.Errorf("organization issue field %q has type %s, want %s", name, field.DataType, dataType)
	}
	if dataType == "single_select" {
		optionMatches := 0
		for _, option := range field.Options {
			if option.Name == desired {
				optionMatches++
			}
		}
		if optionMatches != 1 {
			return organizationIssueField{}, fmt.Errorf("option %q resolved to %d exact matches in organization issue field %q", desired, optionMatches, name)
		}
	}
	return field, nil
}

func validateRepositoryIssueType(ctx context.Context, runner Runner, repository, desired string) error {
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("repos/%s/issue-types?per_page=100", repository))
	output, err := runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("list issue types available to %s: %w", repository, err)
	}
	var pages [][]repositoryIssueType
	if err := json.Unmarshal(output, &pages); err != nil {
		return fmt.Errorf("decode repository issue types: %w", err)
	}
	matches := 0
	for _, page := range pages {
		for _, issueType := range page {
			if issueType.Name == desired {
				matches++
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("issue type %q resolved to %d exact matches for repository %s", desired, matches, repository)
	}
	return nil
}

func queryIssueFieldValues(ctx context.Context, runner Runner, target GitHubItemTarget) ([]issueFieldValue, error) {
	args := []string{"api", "--paginate", "--slurp"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("repos/%s/issues/%d/issue-field-values?per_page=100", target.Repository, target.Number))
	output, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list issue field values for %s#%d: %w", target.Repository, target.Number, err)
	}
	var pages [][]issueFieldValue
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode issue field values: %w", err)
	}
	var result []issueFieldValue
	for _, page := range pages {
		result = append(result, page...)
	}
	return result, nil
}

func setOrganizationIssueFields(ctx context.Context, runner Runner, target GitHubItemTarget, changes []organizationFieldChange) (bool, error) {
	if len(changes) == 0 {
		return false, nil
	}
	before, err := queryIssueFieldValues(ctx, runner, target)
	if err != nil {
		return false, err
	}
	pending := make([]organizationFieldChange, 0, len(changes))
	for _, change := range changes {
		if currentIssueFieldValue(before, change.Field.ID) != change.Desired {
			pending = append(pending, change)
		}
	}
	if len(pending) == 0 {
		return false, nil
	}
	values := make([]map[string]any, 0, len(pending))
	changedIDs := make(map[int]struct{}, len(pending))
	for _, change := range pending {
		values = append(values, map[string]any{"field_id": change.Field.ID, "value": change.Desired})
		changedIDs[change.Field.ID] = struct{}{}
	}
	body, err := json.Marshal(map[string]any{"issue_field_values": values})
	if err != nil {
		return false, fmt.Errorf("encode issue field update: %w", err)
	}
	args := []string{"api", "--method", "POST"}
	args = append(args, apiHeaders()...)
	args = append(args, fmt.Sprintf("repos/%s/issues/%d/issue-field-values", target.Repository, target.Number), "--input", "-")
	stdinRunner, ok := runner.(inputRunner)
	if !ok {
		return false, errors.New("GitHub runner does not support JSON request bodies")
	}
	if _, err := stdinRunner.RunInput(ctx, body, args...); err != nil {
		return false, fmt.Errorf("set organization issue fields: %w", err)
	}
	after, err := queryIssueFieldValues(ctx, runner, target)
	if err != nil {
		return true, fmt.Errorf("read back organization issue fields: %w", err)
	}
	for _, change := range pending {
		if got := currentIssueFieldValue(after, change.Field.ID); got != change.Desired {
			return true, fmt.Errorf("organization issue field %q readback disagrees: got %q, want %q", change.Field.Name, got, change.Desired)
		}
	}
	if !issueFieldValuesPreserved(before, after, changedIDs) {
		return true, errors.New("an unrelated organization issue field changed while setting requested fields")
	}
	return true, nil
}

func currentIssueFieldValue(values []issueFieldValue, fieldID int) string {
	for _, value := range values {
		if value.IssueFieldID != fieldID {
			continue
		}
		if value.SingleSelect != nil {
			return value.SingleSelect.Name
		}
		var text string
		if err := json.Unmarshal(value.Value, &text); err == nil {
			return text
		}
	}
	return ""
}

func issueFieldValuesPreserved(before, after []issueFieldValue, changedIDs map[int]struct{}) bool {
	canonical := func(values []issueFieldValue) map[int]string {
		result := make(map[int]string)
		for _, value := range values {
			if _, changed := changedIDs[value.IssueFieldID]; changed {
				continue
			}
			encoded, _ := json.Marshal(value)
			result[value.IssueFieldID] = string(encoded)
		}
		return result
	}
	return reflect.DeepEqual(canonical(before), canonical(after))
}

type projectFieldChange struct {
	Name     string
	Field    ProjectField
	Desired  string
	OptionID string
	Clear    bool
}

type organizationFieldChange struct {
	Name    string
	Field   organizationIssueField
	Desired string
}

// MutateProjectItemInput defines the full high-level mutation request.
type MutateProjectItemInput struct {
	Project      contract.Project
	Repo         string // "owner/name"
	IssueNumber  int
	URL          string
	Priority     string // common value: P0, P1, P2, P3
	Class        string // common or declared class
	Status       string // status name
	TargetDate   string // YYYY-MM-DD
	ClearFields  []string
	AddIfMissing bool // membership is a separate mutation unless explicitly authorised
}

// MutateProjectItemResult is the verified result after editing Project item fields.
type MutateProjectItemResult struct {
	Project ProjectIdentity   `json:"project"`
	ItemID  string            `json:"itemId"`
	URL     string            `json:"url"`
	Added   bool              `json:"added"`
	Fields  map[string]string `json:"fields"`
}

// PreparedProjectItemMutation holds live definitions for one command invocation.
// Its fields are private so the requested changes cannot diverge from preflight.
// Do not persist it or reuse it across separate commands.
type PreparedProjectItemMutation struct {
	input               MutateProjectItemInput
	schema              ProjectSchema
	projectChanges      []projectFieldChange
	organizationChanges []organizationFieldChange
	issueType           string
}

// PrepareProjectItemMutation resolves configuration before an issue is created,
// or before an existing item is changed. The same invocation can then use Apply
// without fetching those definitions again.
func PrepareProjectItemMutation(ctx context.Context, runner Runner, input MutateProjectItemInput) (*PreparedProjectItemMutation, error) {
	if err := validateProjectMutationValues(input); err != nil {
		return nil, err
	}
	owner, repo, err := splitGitHubRepository(input.Repo)
	if err != nil {
		return nil, err
	}
	target := GitHubItemTarget{
		Repository: input.Repo,
		Owner:      owner,
		Repo:       repo,
		Kind:       "issues",
	}
	if input.IssueNumber != 0 || input.URL != "" {
		target, err = validateProjectMutationInput(input)
		if err != nil {
			return nil, err
		}
	}
	schema := ProjectSchema{Fields: make(map[string]ProjectField)}
	if projectMutationNeedsSchema(input) {
		schema, err = QueryProjectSchema(ctx, runner, input.Project)
		if err != nil {
			return nil, err
		}
	}
	projectChanges, organizationChanges, issueType, err := prepareProjectChanges(ctx, runner, input, target, schema)
	if err != nil {
		return nil, err
	}
	return &PreparedProjectItemMutation{
		input: input, schema: schema, projectChanges: projectChanges,
		organizationChanges: organizationChanges, issueType: issueType,
	}, nil
}

// ValidateProjectItemMutationConfiguration checks live configuration without
// reading or mutating a particular issue.
func ValidateProjectItemMutationConfiguration(ctx context.Context, runner Runner, input MutateProjectItemInput) error {
	_, err := PrepareProjectItemMutation(ctx, runner, input)
	return err
}

// InspectProjectItemMutation validates the requested fields against fresh live
// definitions and returns the current target-centred item state for plan output.
func InspectProjectItemMutation(ctx context.Context, runner Runner, input MutateProjectItemInput) (*ProjectItemState, error) {
	target, err := validateProjectMutationInput(input)
	if err != nil {
		return nil, err
	}
	schema := ProjectSchema{Fields: make(map[string]ProjectField)}
	if projectMutationNeedsSchema(input) {
		schema, err = QueryProjectSchema(ctx, runner, input.Project)
		if err != nil {
			return nil, err
		}
	}
	if _, _, _, err := prepareProjectChanges(ctx, runner, input, target, schema); err != nil {
		return nil, err
	}
	item, err := QueryProjectItem(ctx, runner, input.Project, target)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("%s is not a member of Project %s/%d; run project item-add explicitly", target.URL, input.Project.Owner, input.Project.Number)
	}
	return item, nil
}

// MutateProjectItem performs contract-bound field updates and independently
// verifies the resulting values plus preservation of unrelated scalar Project
// fields in one bounded final readback.
func MutateProjectItem(ctx context.Context, runner Runner, input MutateProjectItemInput) (MutateProjectItemResult, error) {
	if _, err := validateProjectMutationInput(input); err != nil {
		return MutateProjectItemResult{}, err
	}
	prepared, err := PrepareProjectItemMutation(ctx, runner, input)
	if err != nil {
		return MutateProjectItemResult{}, err
	}
	return prepared.Apply(ctx, runner, input.IssueNumber, input.URL)
}

// Apply inspects the target afresh, applies the prepared changes and verifies
// them independently. Only the target identity may be supplied after preflight.
func (prepared *PreparedProjectItemMutation) Apply(ctx context.Context, runner Runner, issueNumber int, url string) (MutateProjectItemResult, error) {
	if prepared == nil {
		return MutateProjectItemResult{}, errors.New("Project mutation has not been prepared")
	}
	input := prepared.input
	input.IssueNumber, input.URL = issueNumber, url
	target, err := validateProjectMutationInput(input)
	if err != nil {
		return MutateProjectItemResult{}, err
	}
	if target.Kind != "issues" && (len(prepared.organizationChanges) > 0 || prepared.issueType != "") {
		return MutateProjectItemResult{}, errors.New("organization issue fields and types cannot be set on a pull request")
	}
	if prepared.input.IssueNumber != 0 || prepared.input.URL != "" {
		original, err := validateProjectMutationInput(prepared.input)
		if err != nil || original.URL != target.URL {
			return MutateProjectItemResult{}, errors.New("Project mutation target changed after preflight")
		}
	}
	schema := prepared.schema
	projectChanges, organizationChanges, issueType := prepared.projectChanges, prepared.organizationChanges, prepared.issueType

	current, err := QueryProjectItem(ctx, runner, input.Project, target)
	if err != nil {
		return MutateProjectItemResult{}, fmt.Errorf("inspect current Project item: %w", err)
	}
	added := false
	if current == nil {
		if !input.AddIfMissing {
			return MutateProjectItemResult{}, fmt.Errorf("%s is not a member of Project %s/%d; run project item-add explicitly", target.URL, input.Project.Owner, input.Project.Number)
		}
		itemID, err := AddProjectItem(ctx, runner, input.Project.Number, input.Project.Owner, target.URL)
		if err != nil {
			return MutateProjectItemResult{}, err
		}
		state, err := QueryProjectItem(ctx, runner, input.Project, target)
		if err != nil {
			return MutateProjectItemResult{}, fmt.Errorf("read back added Project item: %w", err)
		}
		if state == nil {
			return MutateProjectItemResult{}, fmt.Errorf("Project item readback failed: %s is not in Project %s/%d", target.URL, input.Project.Owner, input.Project.Number)
		}
		if state.ItemID != itemID {
			return MutateProjectItemResult{}, fmt.Errorf("Project item ID readback disagrees: add returned %s, target read found %s", itemID, state.ItemID)
		}
		current = state
		added = true
	}

	baseline := *current
	projectMutationApplied := false
	for _, change := range projectChanges {
		if projectItemFieldMatches(*current, change) {
			continue
		}
		edit := EditItemFieldInput{
			ItemID:        current.ItemID,
			ProjectNodeID: schema.ID,
			FieldID:       change.Field.ID,
			Clear:         change.Clear,
		}
		if !change.Clear {
			if change.OptionID != "" {
				edit.SingleSelectOptionID = change.OptionID
			} else {
				edit.DateValue = change.Desired
			}
		}
		if err := EditProjectItemField(ctx, runner, edit); err != nil {
			return MutateProjectItemResult{}, partialProjectMutationError(change.Name, err)
		}
		projectMutationApplied = true
	}

	if _, err := setOrganizationIssueFields(ctx, runner, target, organizationChanges); err != nil {
		return MutateProjectItemResult{}, partialProjectMutationError("organization issue fields", err)
	}

	if issueType != "" {
		if target.Kind != "issues" {
			return MutateProjectItemResult{}, errors.New("organization issue types can only be set on issues, not pull requests")
		}
		if _, err := EditIssue(ctx, runner, EditIssueInput{
			Repo:      target.Repository,
			Number:    target.Number,
			IssueType: &issueType,
		}); err != nil {
			return MutateProjectItemResult{}, partialProjectMutationError("Class", err)
		}
	}

	finalItem := current
	if projectMutationApplied {
		finalItem, err = QueryProjectItem(ctx, runner, input.Project, target)
		if err != nil {
			return MutateProjectItemResult{}, partialProjectMutationError("final Project readback", err)
		}
		if finalItem == nil || finalItem.ItemID != current.ItemID {
			return MutateProjectItemResult{}, partialProjectMutationError("final Project readback", errors.New("item identity or membership changed"))
		}
		for _, change := range projectChanges {
			if projectItemFieldMatches(*finalItem, change) {
				continue
			}
			got, _ := projectItemFieldValue(*finalItem, change.Field.Name)
			return MutateProjectItemResult{}, partialProjectMutationError(change.Name, fmt.Errorf("readback disagrees: got %q, want %q", got, change.Desired))
		}
		changedFields := make([]string, 0, len(projectChanges))
		for _, change := range projectChanges {
			changedFields = append(changedFields, change.Field.Name)
		}
		if !projectItemPreserved(baseline, *finalItem, changedFields...) {
			return MutateProjectItemResult{}, partialProjectMutationError("final Project readback", errors.New("an unrelated scalar Project item value changed"))
		}
	}

	resultFields := projectFieldsForResult(*finalItem, schema)
	for _, change := range organizationChanges {
		resultFields[change.Field.Name] = change.Desired
	}
	if issueType != "" {
		resultFields["Class"] = issueType
	}
	return MutateProjectItemResult{
		Project: ProjectIdentity{Number: input.Project.Number, Owner: input.Project.Owner, Title: input.Project.Title},
		ItemID:  finalItem.ItemID,
		URL:     target.URL,
		Added:   added,
		Fields:  resultFields,
	}, nil
}

func validateProjectMutationInput(input MutateProjectItemInput) (GitHubItemTarget, error) {
	target, err := ResolveGitHubItemTarget(input.Repo, input.IssueNumber, input.URL)
	if err != nil {
		return GitHubItemTarget{}, err
	}
	if err := validateProjectMutationValues(input); err != nil {
		return GitHubItemTarget{}, err
	}
	return target, nil
}

func validateProjectMutationValues(input MutateProjectItemInput) error {
	if input.Priority != "" {
		if _, err := input.Project.ResolvePriority(input.Priority); err != nil {
			return err
		}
	}
	if input.Class != "" {
		if _, err := input.Project.ValidateClass(input.Class); err != nil {
			return err
		}
	}
	if input.Status != "" {
		if _, err := input.Project.ResolveStatus(input.Status); err != nil {
			return err
		}
	}
	if input.TargetDate != "" {
		if err := ValidateISODate(input.TargetDate); err != nil {
			return err
		}
	}
	return nil
}

func projectMutationNeedsSchema(input MutateProjectItemInput) bool {
	requestedDimensions := map[string]bool{
		"Priority": input.Priority != "",
		"Class":    input.Class != "",
		"Status":   input.Status != "",
	}
	for dimension, requested := range requestedDimensions {
		if !requested {
			continue
		}
		if location, ok := input.Project.FieldLocations[dimension]; ok && location.Location == "project field" {
			return true
		}
	}
	return input.TargetDate != "" || len(input.ClearFields) > 0
}

func prepareProjectChanges(ctx context.Context, runner Runner, input MutateProjectItemInput, target GitHubItemTarget, schema ProjectSchema) ([]projectFieldChange, []organizationFieldChange, string, error) {
	var projectChanges []projectFieldChange
	var organizationChanges []organizationFieldChange
	var organizationFields []organizationIssueField
	organizationFieldsLoaded := false
	issueType := ""
	setFields := make(map[string]bool)
	setDestinations := make(map[string]bool)

	addSingleSelect := func(dimension, desired string) error {
		location, ok := input.Project.FieldLocations[dimension]
		if !ok || location.Field == "" {
			return fmt.Errorf("%s does not declare a provider field for %s", input.Project.ContractPath, dimension)
		}
		switch location.Location {
		case "project field":
			field, ok := schema.FindField(location.Field)
			if !ok {
				return fmt.Errorf("declared %s field %q is absent from Project %s/%d", dimension, location.Field, input.Project.Owner, input.Project.Number)
			}
			if field.DataType != "SINGLE_SELECT" {
				return fmt.Errorf("declared %s field %q has type %s, want SINGLE_SELECT", dimension, location.Field, field.DataType)
			}
			optionID, ok := field.FindOptionID(desired)
			if !ok {
				return fmt.Errorf("option %q for %s is absent from Project field %q", desired, dimension, field.Name)
			}
			destination := "project:" + field.ID
			if setDestinations[destination] {
				return fmt.Errorf("Project field %q is requested by more than one dimension", field.Name)
			}
			setDestinations[destination] = true
			projectChanges = append(projectChanges, projectFieldChange{Name: dimension, Field: field, Desired: desired, OptionID: optionID})
			setFields[strings.ToLower(field.Name)] = true
			return nil
		case "organization issue field":
			if target.Kind != "issues" {
				return fmt.Errorf("%s uses an organization issue field and cannot be set on a pull request", dimension)
			}
			if !organizationFieldsLoaded {
				var err error
				organizationFields, err = queryOrganizationIssueFields(ctx, runner, target.Owner)
				if err != nil {
					return err
				}
				organizationFieldsLoaded = true
			}
			field, err := resolveOrganizationIssueField(organizationFields, target.Owner, location.Field, "single_select", desired)
			if err != nil {
				return err
			}
			destination := "organization:" + strconv.Itoa(field.ID)
			if setDestinations[destination] {
				return fmt.Errorf("organization issue field %q is requested by more than one dimension", field.Name)
			}
			setDestinations[destination] = true
			organizationChanges = append(organizationChanges, organizationFieldChange{Name: dimension, Field: field, Desired: desired})
			setFields[strings.ToLower(field.Name)] = true
			return nil
		default:
			return fmt.Errorf("%s location %q is not supported for %s updates", dimension, location.Location, dimension)
		}
	}

	if input.Priority != "" {
		value, err := input.Project.ResolvePriority(input.Priority)
		if err != nil {
			return nil, nil, "", err
		}
		if err := addSingleSelect("Priority", value); err != nil {
			return nil, nil, "", err
		}
	}
	if input.Class != "" {
		value, err := input.Project.ValidateClass(input.Class)
		if err != nil {
			return nil, nil, "", err
		}
		location, ok := input.Project.FieldLocations["Class"]
		if !ok {
			return nil, nil, "", fmt.Errorf("%s does not declare a Class location", input.Project.ContractPath)
		}
		if location.Location == "organization issue type" {
			if target.Kind != "issues" {
				return nil, nil, "", errors.New("Class uses an organization issue type and cannot be set on a pull request")
			}
			if err := validateRepositoryIssueType(ctx, runner, target.Repository, value); err != nil {
				return nil, nil, "", err
			}
			issueType = value
			setFields[strings.ToLower(location.Field)] = true
		} else if err := addSingleSelect("Class", value); err != nil {
			return nil, nil, "", err
		}
	}
	if input.Status != "" {
		value, err := input.Project.ResolveStatus(input.Status)
		if err != nil {
			return nil, nil, "", err
		}
		if err := addSingleSelect("Status", value); err != nil {
			return nil, nil, "", err
		}
	}
	if input.TargetDate != "" {
		if err := ValidateISODate(input.TargetDate); err != nil {
			return nil, nil, "", err
		}
		location, ok := input.Project.FieldLocations["Due date"]
		if !ok || location.Field == "" || location.Location != "project field" {
			return nil, nil, "", fmt.Errorf("%s must declare Due date as a project field for --target-date", input.Project.ContractPath)
		}
		field, ok := schema.FindField(location.Field)
		if !ok {
			return nil, nil, "", fmt.Errorf("declared Due date field %q is absent from Project %s/%d", location.Field, input.Project.Owner, input.Project.Number)
		}
		if field.DataType != "DATE" {
			return nil, nil, "", fmt.Errorf("declared Due date field %q has type %s, want DATE", location.Field, field.DataType)
		}
		projectChanges = append(projectChanges, projectFieldChange{Name: "Due date", Field: field, Desired: input.TargetDate})
		setFields[strings.ToLower(field.Name)] = true
	}

	clearSeen := make(map[string]bool)
	for _, supplied := range input.ClearFields {
		name := strings.TrimSpace(supplied)
		if name == "" {
			continue
		}
		var matchingLocations []contract.FieldLocation
		for _, location := range input.Project.FieldLocations {
			if strings.EqualFold(location.Field, name) {
				matchingLocations = append(matchingLocations, location)
			}
		}
		if len(matchingLocations) == 0 {
			return nil, nil, "", fmt.Errorf("cannot clear undeclared field %q", name)
		}
		if len(matchingLocations) > 1 {
			return nil, nil, "", fmt.Errorf("cannot clear ambiguous declared field %q", name)
		}
		location := matchingLocations[0]
		if location.Location != "project field" {
			return nil, nil, "", fmt.Errorf("--clear currently supports declared Project fields only; %q is at %s", name, location.Location)
		}
		canonical := location.Field
		key := strings.ToLower(canonical)
		if setFields[key] {
			return nil, nil, "", fmt.Errorf("field %q cannot be set and cleared in the same operation", canonical)
		}
		if clearSeen[key] {
			continue
		}
		clearSeen[key] = true
		field, ok := schema.FindField(canonical)
		if !ok {
			return nil, nil, "", fmt.Errorf("declared field %q is absent from Project %s/%d", canonical, input.Project.Owner, input.Project.Number)
		}
		projectChanges = append(projectChanges, projectFieldChange{Name: "clear " + canonical, Field: field, Clear: true})
	}
	return projectChanges, organizationChanges, issueType, nil
}

// ValidateISODate rejects values that are not real calendar dates in the
// exact YYYY-MM-DD form accepted by GitHub Project date fields.
func ValidateISODate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return fmt.Errorf("invalid target date %q; expected a real date in YYYY-MM-DD form", value)
	}
	return nil
}

func projectItemFieldValue(item ProjectItemState, fieldName string) (string, bool) {
	for key, value := range item.Fields {
		if !strings.EqualFold(key, fieldName) {
			continue
		}
		return value, true
	}
	return "", false
}

func projectItemFieldMatches(item ProjectItemState, change projectFieldChange) bool {
	if change.Clear {
		for key, value := range item.Fields {
			if !strings.EqualFold(key, change.Field.Name) {
				continue
			}
			return value == ""
		}
		return true
	}
	value, present := projectItemFieldValue(item, change.Field.Name)
	return present && value == change.Desired
}

func projectItemPreserved(before, after ProjectItemState, changedFields ...string) bool {
	excluded := make(map[string]struct{}, len(changedFields))
	for _, field := range changedFields {
		if field != "" {
			excluded[strings.ToLower(field)] = struct{}{}
		}
	}
	filter := func(values map[string]json.RawMessage) map[string]string {
		result := make(map[string]string)
		for key, raw := range values {
			normalizedKey := strings.ToLower(key)
			if strings.HasPrefix(normalizedKey, "field:") {
				fieldName := strings.TrimPrefix(normalizedKey, "field:")
				if _, skip := excluded[fieldName]; skip {
					continue
				}
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				result[normalizedKey] = string(raw)
				continue
			}
			encoded, _ := json.Marshal(decoded)
			result[normalizedKey] = string(encoded)
		}
		return result
	}
	return reflect.DeepEqual(filter(before.raw), filter(after.raw))
}

func projectFieldsForResult(item ProjectItemState, schema ProjectSchema) map[string]string {
	fields := make(map[string]string)
	names := make([]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		if value, ok := projectItemFieldValue(item, name); ok {
			fields[name] = value
		}
	}
	return fields
}

func partialProjectMutationError(step string, err error) error {
	return fmt.Errorf("%s mutation did not verify (%v); an earlier narrow change may already have applied, so inspect before retrying", step, err)
}
