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
	// OptionNames maps a lowercased option name to its exact live spelling.
	OptionNames map[string]string
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

// FindOptionName returns the exact live spelling of a single-select option
// matched case-insensitively, so readback compares against the provider value.
func (f ProjectField) FindOptionName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if exact, ok := f.OptionNames[key]; ok {
		return exact
	}
	return name
}

// ProjectItemState contains the inspected fields for an item in a Project.
type ProjectItemState struct {
	ItemID      string            `json:"itemId"`
	IssueNumber int               `json:"issueNumber,omitempty"`
	URL         string            `json:"url"`
	Fields      map[string]string `json:"fields"`
	// Archived reports that the item is archived in the Project: it remains a
	// member but is hidden from Project views.
	Archived bool `json:"archived"`
	// ContentState is the issue or pull request state (for example OPEN or
	// CLOSED) reported alongside the Project item. It is content, not a Project
	// field, so it is deliberately excluded from preservation comparisons.
	ContentState string `json:"contentState,omitempty"`
	raw          map[string]json.RawMessage
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
		Owner *graphQLProjectOwner[graphQLProjectData] `json:"owner"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// graphQLProjectOwner is the repositoryOwner root narrowed to ProjectV2Owner.
// It resolves both users and organizations in one request, so the contract's
// optional owner type becomes an assertion rather than a prerequisite lookup.
type graphQLProjectOwner[P any] struct {
	Typename  string `json:"__typename"`
	Login     string `json:"login"`
	ProjectV2 *P     `json:"projectV2"`
}

// ProjectSchemaQuery returns the GraphQL query for fetching a ProjectV2's
// fields. It works for user and organization owners alike.
func ProjectSchemaQuery() string {
	return `query($login: String!, $number: Int!) {
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
            ... on ProjectV2FieldCommon { id name dataType }
            ... on ProjectV2SingleSelectField { options { id name } }
          }
          pageInfo { hasNextPage }
        }
      }
    }
  }
}`
}

// verifyProjectOwner checks the owner identity returned by repositoryOwner
// against the contract, including the declared owner type when present.
func verifyProjectOwner(project contract.Project, typename, login string) error {
	if !strings.EqualFold(login, project.Owner) {
		return fmt.Errorf("Project owner identity disagrees with %s: got %q, want %q", project.ContractPath, login, project.Owner)
	}
	observedType := ""
	switch typename {
	case "User":
		observedType = "user"
	case "Organization":
		observedType = "organization"
	default:
		return fmt.Errorf("unsupported Project owner type %q for %s", typename, project.Owner)
	}
	if project.OwnerType != "" && project.OwnerType != observedType {
		return fmt.Errorf("Project owner type disagrees with %s: %s is a %s, contract declares %s", project.ContractPath, project.Owner, observedType, project.OwnerType)
	}
	return nil
}

// QueryProjectSchema fetches the Project node ID, fields, and options via one
// GraphQL request, without a separate owner-type lookup.
func QueryProjectSchema(ctx context.Context, client Client, project contract.Project) (ProjectSchema, error) {
	output, err := graphQLBytes(ctx, client, ProjectSchemaQuery(), map[string]any{"login": project.Owner, "number": project.Number})
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
	if resp.Data.Owner == nil {
		return ProjectSchema{}, fmt.Errorf("Project owner %s was not found or is not accessible", project.Owner)
	}
	if err := verifyProjectOwner(project, resp.Data.Owner.Typename, resp.Data.Owner.Login); err != nil {
		return ProjectSchema{}, err
	}
	data := resp.Data.Owner.ProjectV2
	if data == nil {
		return ProjectSchema{}, fmt.Errorf("Project %s/%d not found", project.Owner, project.Number)
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
			ID:          node.ID,
			Name:        node.Name,
			DataType:    node.DataType,
			Options:     make(map[string]string),
			OptionNames: make(map[string]string),
		}
		for _, opt := range node.Options {
			key := strings.ToLower(strings.TrimSpace(opt.Name))
			f.Options[key] = opt.ID
			f.OptionNames[key] = opt.Name
		}
		schema.Fields[strings.ToLower(strings.TrimSpace(node.Name))] = f
	}

	return schema, nil
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

type graphQLProjectIdentity struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type graphQLProjectItemResponse struct {
	Data struct {
		ProjectOwner *graphQLProjectOwner[graphQLProjectIdentity] `json:"projectOwner"`
		Repository   *struct {
			Target *struct {
				ID           string `json:"id"`
				URL          string `json:"url"`
				State        string `json:"state"`
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
// can verify requested values and preservation with one bounded readback. The
// same request resolves the contract Project's node identity, so membership can
// be added by node ID without further lookups.
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
	return fmt.Sprintf(`query($owner: String!, $repo: String!, $number: Int!, $projectOwner: String!, $projectNumber: Int!) {
  projectOwner: repositoryOwner(login: $projectOwner) {
    __typename
    login
    ... on ProjectV2Owner {
      projectV2(number: $projectNumber) { id number title }
    }
  }
  repository(owner: $owner, name: $repo) {
    target: %s(number: $number) {
      id
      url
      state
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

// projectItemLookup is one target-centred read: the verified Project and
// target node identities, plus the target's item in that Project (nil when the
// target is not a member).
type projectItemLookup struct {
	ProjectID string
	ContentID string
	Item      *ProjectItemState
}

// QueryProjectItem resolves membership and current scalar Project fields from
// the target issue or pull request. The query is bounded to at most 100 Project
// memberships and 100 set field values per membership, and refuses to treat a
// truncated response as proof.
func QueryProjectItem(ctx context.Context, client Client, project contract.Project, target GitHubItemTarget) (*ProjectItemState, error) {
	lookup, err := lookupProjectItem(ctx, client, project, target)
	if err != nil {
		return nil, err
	}
	return lookup.Item, nil
}

func lookupProjectItem(ctx context.Context, client Client, project contract.Project, target GitHubItemTarget) (projectItemLookup, error) {
	query := ProjectItemQuery(target.Kind)
	if query == "" {
		return projectItemLookup{}, fmt.Errorf("unsupported GitHub item kind %q", target.Kind)
	}
	output, err := graphQLBytes(ctx, client, query, map[string]any{"owner": target.Owner, "repo": target.Repo, "number": target.Number, "projectOwner": project.Owner, "projectNumber": project.Number})
	if err != nil {
		return projectItemLookup{}, fmt.Errorf("query Project membership for %s: %w", target.URL, err)
	}

	var response graphQLProjectItemResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return projectItemLookup{}, fmt.Errorf("decode Project membership for %s: %w", target.URL, err)
	}
	if len(response.Errors) > 0 {
		return projectItemLookup{}, fmt.Errorf("GraphQL error querying Project membership: %s", response.Errors[0].Message)
	}
	owner := response.Data.ProjectOwner
	if owner == nil {
		return projectItemLookup{}, fmt.Errorf("Project owner %s was not found or is not accessible", project.Owner)
	}
	if err := verifyProjectOwner(project, owner.Typename, owner.Login); err != nil {
		return projectItemLookup{}, err
	}
	if owner.ProjectV2 == nil || owner.ProjectV2.ID == "" {
		return projectItemLookup{}, fmt.Errorf("Project %s/%d was not found or has no node identity", project.Owner, project.Number)
	}
	if owner.ProjectV2.Number != project.Number || owner.ProjectV2.Title != project.Title {
		return projectItemLookup{}, fmt.Errorf(
			"Project identity disagrees with %s: got number %d title %q, want number %d title %q",
			project.ContractPath, owner.ProjectV2.Number, owner.ProjectV2.Title, project.Number, project.Title,
		)
	}
	projectID := owner.ProjectV2.ID

	if response.Data.Repository == nil || response.Data.Repository.Target == nil {
		return projectItemLookup{}, fmt.Errorf("GitHub target %s was not found or is not accessible", target.URL)
	}
	observedTarget := response.Data.Repository.Target
	if !strings.EqualFold(observedTarget.URL, target.URL) {
		return projectItemLookup{}, fmt.Errorf("GitHub target identity disagrees: got %s, want %s", observedTarget.URL, target.URL)
	}
	if observedTarget.ID == "" {
		return projectItemLookup{}, fmt.Errorf("GitHub target %s has no node identity", target.URL)
	}
	if observedTarget.ProjectItems.PageInfo.HasNextPage {
		return projectItemLookup{}, fmt.Errorf("%s belongs to more than 100 Projects; refusing an incomplete membership read", target.URL)
	}
	lookup := projectItemLookup{ProjectID: projectID, ContentID: observedTarget.ID}

	var match *graphQLProjectItemNode
	for index := range observedTarget.ProjectItems.Nodes {
		node := &observedTarget.ProjectItems.Nodes[index]
		if !strings.EqualFold(node.Project.Owner.Login, project.Owner) || node.Project.Number != project.Number {
			continue
		}
		if node.Project.Title != project.Title {
			return projectItemLookup{}, fmt.Errorf(
				"Project identity disagrees with %s: got title %q, want %q",
				project.ContractPath,
				node.Project.Title,
				project.Title,
			)
		}
		if match != nil {
			return projectItemLookup{}, fmt.Errorf("Project %s/%d contains more than one item for %s", project.Owner, project.Number, target.URL)
		}
		match = node
	}
	if match == nil {
		return lookup, nil
	}
	if match.ID == "" || match.Project.ID == "" {
		return projectItemLookup{}, fmt.Errorf("Project item for %s has incomplete node identity", target.URL)
	}
	if match.Project.ID != projectID {
		return projectItemLookup{}, fmt.Errorf("Project node identity disagrees: membership reports %s, Project read reports %s", match.Project.ID, projectID)
	}
	if match.FieldValues.PageInfo.HasNextPage {
		return projectItemLookup{}, fmt.Errorf("Project item %s has more than 100 set fields; refusing an incomplete field read", match.ID)
	}

	state, err := decodeGraphQLProjectItem(*match, target, observedTarget.State)
	if err != nil {
		return projectItemLookup{}, err
	}
	lookup.Item = &state
	return lookup, nil
}

func decodeGraphQLProjectItem(node graphQLProjectItemNode, target GitHubItemTarget, contentState string) (ProjectItemState, error) {
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
		ItemID:       node.ID,
		IssueNumber:  target.Number,
		URL:          target.URL,
		Fields:       fields,
		Archived:     node.IsArchived,
		ContentState: contentState,
		raw:          rawFields,
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

// ProjectItemMembership is the verified outcome of ensuring membership.
type ProjectItemMembership struct {
	Item ProjectItemState
	// Added reports that this call added the target to the Project.
	Added bool
	// Unarchived reports that the target was an archived member, hidden from
	// Project views, and this call restored it.
	Unarchived bool
}

// EnsureProjectItem adds a missing item, or restores an archived one, and
// proves membership with a separate, target-centred read. Existing unarchived
// membership is an idempotent no-op.
func EnsureProjectItem(ctx context.Context, client Client, project contract.Project, target GitHubItemTarget) (ProjectItemMembership, error) {
	lookup, err := lookupProjectItem(ctx, client, project, target)
	if err != nil {
		return ProjectItemMembership{}, fmt.Errorf("inspect Project membership: %w", err)
	}
	return ensureProjectMembership(ctx, client, project, target, lookup)
}

func ensureProjectMembership(ctx context.Context, client Client, project contract.Project, target GitHubItemTarget, lookup projectItemLookup) (ProjectItemMembership, error) {
	if lookup.Item != nil && !lookup.Item.Archived {
		return ProjectItemMembership{Item: *lookup.Item}, nil
	}
	if lookup.Item != nil {
		before := *lookup.Item
		if err := unarchiveProjectItem(ctx, client, lookup.ProjectID, before.ItemID); err != nil {
			return ProjectItemMembership{}, err
		}
		after, err := QueryProjectItem(ctx, client, project, target)
		if err != nil {
			return ProjectItemMembership{Unarchived: true}, fmt.Errorf("read back unarchived Project item: %w", err)
		}
		if after == nil || after.ItemID != before.ItemID {
			return ProjectItemMembership{Unarchived: true}, fmt.Errorf("Project item readback failed after unarchiving: item %s is no longer the member for %s", before.ItemID, target.URL)
		}
		if after.Archived {
			return ProjectItemMembership{Unarchived: true}, fmt.Errorf("Project item %s is still archived after unarchiving", before.ItemID)
		}
		if !projectItemPreservedAcrossUnarchive(before, *after) {
			return ProjectItemMembership{Unarchived: true}, errors.New("a scalar Project item value changed while unarchiving the item")
		}
		return ProjectItemMembership{Item: *after, Unarchived: true}, nil
	}

	itemID, err := addProjectItemByID(ctx, client, lookup.ProjectID, lookup.ContentID)
	if err != nil {
		return ProjectItemMembership{}, fmt.Errorf("add %s to Project %s/%d: %w", target.URL, project.Owner, project.Number, err)
	}
	after, err := QueryProjectItem(ctx, client, project, target)
	if err != nil {
		return ProjectItemMembership{Added: true}, fmt.Errorf("read back added Project item: %w", err)
	}
	if after == nil {
		return ProjectItemMembership{Added: true}, fmt.Errorf("Project item readback failed: %s is not in Project %s/%d", target.URL, project.Owner, project.Number)
	}
	if after.ItemID != itemID {
		return ProjectItemMembership{Added: true}, fmt.Errorf("Project item ID readback disagrees: add returned %s, target read found %s", itemID, after.ItemID)
	}
	return ProjectItemMembership{Item: *after, Added: true}, nil
}

// projectItemPreservedAcrossUnarchive compares every scalar value and identity
// except the archive flag, which unarchiving is expected to change.
func projectItemPreservedAcrossUnarchive(before, after ProjectItemState) bool {
	normalized := ProjectItemState{raw: make(map[string]json.RawMessage, len(before.raw))}
	for key, value := range before.raw {
		normalized.raw[key] = value
	}
	normalized.raw["meta:isarchived"] = mustMarshalProjectValue(after.Archived)
	return projectItemPreserved(normalized, after)
}

const addProjectItemMutation = `mutation($projectId: ID!, $contentId: ID!) {
  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) { item { id } }
}`

const unarchiveProjectItemMutation = `mutation($projectId: ID!, $itemId: ID!) {
  unarchiveProjectV2Item(input: {projectId: $projectId, itemId: $itemId}) { item { id } }
}`

type graphQLMutationResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// mutationItemID decodes one mutation payload field and returns the item node
// ID it reports under payloadField (item or projectV2Item).
func mutationItemID(raw json.RawMessage, payloadField string) string {
	var payload map[string]*struct {
		ID string `json:"id"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if item := payload[payloadField]; item != nil {
		return item.ID
	}
	return ""
}

// addProjectItemByID adds content to a Project by node IDs already verified by
// the target-centred read, avoiding gh project item-add's internal lookups.
func addProjectItemByID(ctx context.Context, client Client, projectID, contentID string) (string, error) {
	if projectID == "" || contentID == "" {
		return "", errors.New("Project and target node IDs are required to add an item")
	}
	output, err := graphQLBytes(ctx, client, addProjectItemMutation, map[string]any{"projectId": projectID, "contentId": contentID})
	if err != nil {
		return "", err
	}
	var response graphQLMutationResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("decode addProjectV2ItemById response: %w", err)
	}
	if len(response.Errors) > 0 {
		return "", fmt.Errorf("GraphQL error adding Project item: %s", response.Errors[0].Message)
	}
	itemID := mutationItemID(response.Data["addProjectV2ItemById"], "item")
	if itemID == "" {
		return "", errors.New("addProjectV2ItemById returned an empty item ID")
	}
	return itemID, nil
}

func unarchiveProjectItem(ctx context.Context, client Client, projectID, itemID string) error {
	output, err := graphQLBytes(ctx, client, unarchiveProjectItemMutation, map[string]any{"projectId": projectID, "itemId": itemID})
	if err != nil {
		return fmt.Errorf("unarchive Project item %s: %w", itemID, err)
	}
	var response graphQLMutationResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("decode unarchiveProjectV2Item response: %w", err)
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("GraphQL error unarchiving Project item %s: %s", itemID, response.Errors[0].Message)
	}
	if got := mutationItemID(response.Data["unarchiveProjectV2Item"], "item"); got != itemID {
		return fmt.Errorf("unarchiveProjectV2Item returned item %q, want %s", got, itemID)
	}
	return nil
}

// projectFieldWriteRequest builds one GraphQL request containing an aliased
// update or clear mutation per change, so all Project field writes for an item
// cost a single API call. Variables keep values out of the query text.
func projectFieldWriteRequest(projectID, itemID string, changes []projectFieldChange) ([]byte, []string, error) {
	declarations := []string{"$projectId: ID!", "$itemId: ID!"}
	selections := make([]string, 0, len(changes))
	aliases := make([]string, 0, len(changes))
	variables := map[string]any{"projectId": projectID, "itemId": itemID}
	for index, change := range changes {
		alias := fmt.Sprintf("f%d", index)
		aliases = append(aliases, alias)
		declarations = append(declarations, fmt.Sprintf("$%sField: ID!", alias))
		variables[alias+"Field"] = change.Field.ID
		if change.Clear {
			selections = append(selections, fmt.Sprintf(
				"  %s: clearProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $%sField}) { projectV2Item { id } }",
				alias, alias,
			))
			continue
		}
		value := map[string]string{}
		switch {
		case change.OptionID != "":
			value["singleSelectOptionId"] = change.OptionID
		case change.Desired != "":
			value["date"] = change.Desired
		default:
			return nil, nil, fmt.Errorf("Project field change %s has no value", change.Name)
		}
		declarations = append(declarations, fmt.Sprintf("$%sValue: ProjectV2FieldValue!", alias))
		variables[alias+"Value"] = value
		selections = append(selections, fmt.Sprintf(
			"  %s: updateProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $%sField, value: $%sValue}) { projectV2Item { id } }",
			alias, alias, alias,
		))
	}
	query := "mutation(" + strings.Join(declarations, ", ") + ") {\n" + strings.Join(selections, "\n") + "\n}"
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, nil, fmt.Errorf("encode Project field write: %w", err)
	}
	return body, aliases, nil
}

// writeProjectItemFields applies every change in one GraphQL request. GitHub
// executes each aliased mutation independently, so an error does not mean
// nothing applied; callers must read back before reporting what changed.
func writeProjectItemFields(ctx context.Context, client Client, projectID, itemID string, changes []projectFieldChange) error {
	if len(changes) == 0 {
		return nil
	}
	body, aliases, err := projectFieldWriteRequest(projectID, itemID, changes)
	if err != nil {
		return err
	}
	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return err
	}
	output, err := graphQLBytes(ctx, client, request.Query, request.Variables)
	if err != nil {
		return err
	}
	var response graphQLMutationResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("decode Project field write response: %w", err)
	}
	var failed []string
	for index, alias := range aliases {
		if mutationItemID(response.Data[alias], "projectV2Item") != itemID {
			failed = append(failed, changes[index].Name)
		}
	}
	if len(response.Errors) > 0 || len(failed) > 0 {
		message := "unconfirmed: " + strings.Join(failed, ", ")
		if len(response.Errors) > 0 {
			message = "GraphQL error: " + response.Errors[0].Message + "; " + message
		}
		return errors.New(message)
	}
	return nil
}

// projectFieldWriteError reports a failed batched write honestly. GraphQL may
// have applied some aliased mutations, so it reads the item back and names
// which requested values are now present.
func projectFieldWriteError(ctx context.Context, client Client, project contract.Project, target GitHubItemTarget, changes []projectFieldChange, writeErr error) error {
	names := make([]string, 0, len(changes))
	for _, change := range changes {
		names = append(names, change.Name)
	}
	item, readErr := QueryProjectItem(ctx, client, project, target)
	if readErr == nil && item == nil {
		readErr = errors.New("item is no longer a member")
	}
	if readErr != nil {
		return fmt.Errorf(
			"Project field write did not verify (%v); GitHub applies each field mutation in the request independently, so some of [%s] may already have applied, and the readback to confirm which also failed (%v); inspect before retrying",
			writeErr, strings.Join(names, ", "), readErr,
		)
	}
	var applied, missing []string
	for _, change := range changes {
		if projectItemFieldMatches(*item, change) {
			applied = append(applied, change.Name)
		} else {
			missing = append(missing, change.Name)
		}
	}
	return fmt.Errorf(
		"Project field write did not verify (%v); GitHub applies each field mutation in the request independently: readback shows applied [%s], not applied [%s]; inspect before retrying",
		writeErr, strings.Join(applied, ", "), strings.Join(missing, ", "),
	)
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

func queryOrganizationIssueFields(ctx context.Context, client Client, owner string) ([]organizationIssueField, error) {
	output, err := restPageBytes(ctx, client, "/"+"orgs/"+owner+"/issue-fields?per_page=100")
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

func validateRepositoryIssueType(ctx context.Context, client Client, repository, desired string) error {
	output, err := restPageBytes(ctx, client, "/"+fmt.Sprintf("repos/%s/issue-types?per_page=100", repository))
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

func queryIssueFieldValues(ctx context.Context, client Client, target GitHubItemTarget) ([]issueFieldValue, error) {
	output, err := restPageBytes(ctx, client, "/"+fmt.Sprintf("repos/%s/issues/%d/issue-field-values?per_page=100", target.Repository, target.Number))
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

func setOrganizationIssueFields(ctx context.Context, client Client, target GitHubItemTarget, changes []organizationFieldChange) (bool, error) {
	if len(changes) == 0 {
		return false, nil
	}
	before, err := queryIssueFieldValues(ctx, client, target)
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
	if _, err := client.REST(ctx, "POST", fmt.Sprintf("/repos/%s/issues/%d/issue-field-values", target.Repository, target.Number), map[string]any{"issue_field_values": values}); err != nil {
		return false, fmt.Errorf("set organization issue fields: %w", err)
	}
	after, err := queryIssueFieldValues(ctx, client, target)
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
	Project ProjectIdentity `json:"project"`
	ItemID  string          `json:"itemId"`
	URL     string          `json:"url"`
	Added   bool            `json:"added"`
	// Unarchived reports that an archived member was restored because the
	// caller asked for membership (AddIfMissing).
	Unarchived bool `json:"unarchived,omitempty"`
	// Archived reports that the item remains archived (hidden from Project
	// views) after the edit; field edits do not change archive state.
	Archived bool              `json:"archived"`
	Fields   map[string]string `json:"fields"`
	// AutomationSideEffects lists changes made by the Project's own built-in
	// workflows during the command, reported rather than treated as failures.
	AutomationSideEffects []string `json:"automationSideEffects,omitempty"`
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
func PrepareProjectItemMutation(ctx context.Context, client Client, input MutateProjectItemInput) (*PreparedProjectItemMutation, error) {
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
		schema, err = QueryProjectSchema(ctx, client, input.Project)
		if err != nil {
			return nil, err
		}
	}
	projectChanges, organizationChanges, issueType, err := prepareProjectChanges(ctx, client, input, target, schema)
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
func ValidateProjectItemMutationConfiguration(ctx context.Context, client Client, input MutateProjectItemInput) error {
	_, err := PrepareProjectItemMutation(ctx, client, input)
	return err
}

// InspectProjectItemMutation validates the requested fields against fresh live
// definitions and returns the current target-centred item state for plan output.
func InspectProjectItemMutation(ctx context.Context, client Client, input MutateProjectItemInput) (*ProjectItemState, error) {
	target, err := validateProjectMutationInput(input)
	if err != nil {
		return nil, err
	}
	schema := ProjectSchema{Fields: make(map[string]ProjectField)}
	if projectMutationNeedsSchema(input) {
		schema, err = QueryProjectSchema(ctx, client, input.Project)
		if err != nil {
			return nil, err
		}
	}
	if _, _, _, err := prepareProjectChanges(ctx, client, input, target, schema); err != nil {
		return nil, err
	}
	item, err := QueryProjectItem(ctx, client, input.Project, target)
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
func MutateProjectItem(ctx context.Context, client Client, input MutateProjectItemInput) (MutateProjectItemResult, error) {
	if _, err := validateProjectMutationInput(input); err != nil {
		return MutateProjectItemResult{}, err
	}
	prepared, err := PrepareProjectItemMutation(ctx, client, input)
	if err != nil {
		return MutateProjectItemResult{}, err
	}
	return prepared.Apply(ctx, client, input.IssueNumber, input.URL)
}

// Apply inspects the target afresh, applies the prepared changes and verifies
// them independently. Only the target identity may be supplied after preflight.
func (prepared *PreparedProjectItemMutation) Apply(ctx context.Context, client Client, issueNumber int, url string) (MutateProjectItemResult, error) {
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

	lookup, err := lookupProjectItem(ctx, client, input.Project, target)
	if err != nil {
		return MutateProjectItemResult{}, fmt.Errorf("inspect current Project item: %w", err)
	}
	if schema.ID != "" && schema.ID != lookup.ProjectID {
		return MutateProjectItemResult{}, fmt.Errorf("Project node identity changed after preflight: schema %s, target read %s", schema.ID, lookup.ProjectID)
	}
	if lookup.Item == nil && !input.AddIfMissing {
		return MutateProjectItemResult{}, fmt.Errorf("%s is not a member of Project %s/%d; run project item-add explicitly", target.URL, input.Project.Owner, input.Project.Number)
	}
	current := lookup.Item
	added, unarchived := false, false
	if input.AddIfMissing && (current == nil || current.Archived) {
		membership, err := ensureProjectMembership(ctx, client, input.Project, target, lookup)
		if err != nil {
			return MutateProjectItemResult{}, err
		}
		current = &membership.Item
		added, unarchived = membership.Added, membership.Unarchived
	}

	baseline := *current
	var pending []projectFieldChange
	for _, change := range projectChanges {
		if !projectItemFieldMatches(*current, change) {
			pending = append(pending, change)
		}
	}
	projectMutationApplied := len(pending) > 0
	if projectMutationApplied {
		if err := writeProjectItemFields(ctx, client, lookup.ProjectID, current.ItemID, pending); err != nil {
			return MutateProjectItemResult{}, projectFieldWriteError(ctx, client, input.Project, target, pending, err)
		}
	}

	if _, err := setOrganizationIssueFields(ctx, client, target, organizationChanges); err != nil {
		return MutateProjectItemResult{}, partialProjectMutationError("organization issue fields", err)
	}

	if issueType != "" {
		if target.Kind != "issues" {
			return MutateProjectItemResult{}, errors.New("organization issue types can only be set on issues, not pull requests")
		}
		if _, err := EditIssue(ctx, client, EditIssueInput{
			Repo:      target.Repository,
			Number:    target.Number,
			IssueType: &issueType,
		}); err != nil {
			return MutateProjectItemResult{}, partialProjectMutationError("Class", err)
		}
	}

	finalItem := current
	var sideEffects []string
	contentKind := "pull request"
	if target.Kind == "issues" {
		contentKind = "issue"
	}
	changedFields := make([]string, 0, len(projectChanges))
	for _, change := range projectChanges {
		changedFields = append(changedFields, change.Field.Name)
	}
	if projectMutationApplied {
		readBack := func() (*ProjectItemState, error) {
			item, err := QueryProjectItem(ctx, client, input.Project, target)
			if err != nil {
				return nil, partialProjectMutationError("final Project readback", err)
			}
			if item == nil || item.ItemID != current.ItemID {
				return nil, partialProjectMutationError("final Project readback", errors.New("item identity or membership changed"))
			}
			return item, nil
		}
		finalItem, err = readBack()
		if err != nil {
			return MutateProjectItemResult{}, err
		}
		mismatches := projectFieldMismatches(*finalItem, projectChanges)
		if added && len(mismatches) == 1 && mismatches[0].Name == "Status" {
			// The Project's "Item added to project" workflow can run after our
			// write and overwrite the requested Status. Let it settle, re-apply
			// Status once and verify everything again.
			status := mismatches[0]
			overwritten, _ := projectItemFieldValue(*finalItem, status.Field.Name)
			if err := sleepContext(ctx, addedItemAutomationSettleDelay); err != nil {
				return MutateProjectItemResult{}, partialProjectMutationError(status.Name, err)
			}
			if err := writeProjectItemFields(ctx, client, lookup.ProjectID, current.ItemID, []projectFieldChange{status}); err != nil {
				return MutateProjectItemResult{}, projectFieldWriteError(ctx, client, input.Project, target, []projectFieldChange{status}, err)
			}
			finalItem, err = readBack()
			if err != nil {
				return MutateProjectItemResult{}, err
			}
			mismatches = projectFieldMismatches(*finalItem, projectChanges)
			if len(mismatches) > 0 {
				got, _ := projectItemFieldValue(*finalItem, mismatches[0].Field.Name)
				return MutateProjectItemResult{}, partialProjectMutationError(mismatches[0].Name, fmt.Errorf(
					"readback disagrees after re-applying %s once: got %q, want %q; the Project's item-added workflow may keep overriding it, so check its workflow settings",
					status.Field.Name, got, mismatches[0].Desired,
				))
			}
			sideEffects = append(sideEffects, fmt.Sprintf(
				"Project automation changed %s to %q after the item was added; %q was re-applied and verified",
				status.Field.Name, overwritten, status.Desired,
			))
		}
		if len(mismatches) > 0 {
			got, _ := projectItemFieldValue(*finalItem, mismatches[0].Field.Name)
			return MutateProjectItemResult{}, partialProjectMutationError(mismatches[0].Name, fmt.Errorf("readback disagrees: got %q, want %q", got, mismatches[0].Desired))
		}
		if baseline.ContentState != "" && finalItem.ContentState != "" && baseline.ContentState != finalItem.ContentState {
			sideEffects = append(sideEffects, fmt.Sprintf(
				"Project automation changed the %s state from %s to %s (for example the built-in Auto-close issue workflow when Status is Done)",
				contentKind, baseline.ContentState, finalItem.ContentState,
			))
		}
	}
	if added {
		if effect, ok := addedItemStatusAutomation(input.Project, *finalItem, changedFields); ok {
			sideEffects = append(sideEffects, effect)
			changedFields = append(changedFields, input.Project.FieldLocations["Status"].Field)
		}
	}
	if projectMutationApplied && !projectItemPreserved(baseline, *finalItem, changedFields...) {
		return MutateProjectItemResult{}, partialProjectMutationError("final Project readback", errors.New("an unrelated scalar Project item value changed"))
	}

	resultFields := projectFieldsForResult(*finalItem, schema)
	for _, change := range organizationChanges {
		resultFields[change.Field.Name] = change.Desired
	}
	if issueType != "" {
		resultFields["Class"] = issueType
	}
	return MutateProjectItemResult{
		Project:    ProjectIdentity{Number: input.Project.Number, Owner: input.Project.Owner, Title: input.Project.Title},
		ItemID:     finalItem.ItemID,
		URL:        target.URL,
		Added:      added,
		Unarchived: unarchived,
		Archived:   finalItem.Archived,
		Fields:     resultFields,

		AutomationSideEffects: sideEffects,
	}, nil
}

// addedItemAutomationSettleDelay is how long Apply waits for the Project's
// item-added workflow before re-applying an overwritten Status once.
var addedItemAutomationSettleDelay = 2 * time.Second

// sleepContext waits for d or until ctx ends. Tests replace it.
var sleepContext = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func projectFieldMismatches(item ProjectItemState, changes []projectFieldChange) []projectFieldChange {
	var mismatches []projectFieldChange
	for _, change := range changes {
		if !projectItemFieldMatches(item, change) {
			mismatches = append(mismatches, change)
		}
	}
	return mismatches
}

// addedItemStatusAutomation recognises the built-in "Item added to project"
// workflow: on an item this command has just added, any unrequested Status is
// the Project's automation, not collateral damage. The workflow can set Status
// before or after the post-add read, so the final value is reported whenever it
// is non-empty rather than only when it appeared to be unset at baseline. Any
// other unrequested change still fails preservation.
func addedItemStatusAutomation(project contract.Project, after ProjectItemState, changedFields []string) (string, bool) {
	location, ok := project.FieldLocations["Status"]
	if !ok || location.Location != "project field" || location.Field == "" {
		return "", false
	}
	for _, name := range changedFields {
		if strings.EqualFold(name, location.Field) {
			return "", false
		}
	}
	current, present := projectItemFieldValue(after, location.Field)
	if !present || current == "" {
		return "", false
	}
	return fmt.Sprintf("Project automation set %s to %q after the item was added", location.Field, current), true
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

func prepareProjectChanges(ctx context.Context, client Client, input MutateProjectItemInput, target GitHubItemTarget, schema ProjectSchema) ([]projectFieldChange, []organizationFieldChange, string, error) {
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
			desired = field.FindOptionName(desired)
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
				organizationFields, err = queryOrganizationIssueFields(ctx, client, target.Owner)
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
			if err := validateRepositoryIssueType(ctx, client, target.Repository, value); err != nil {
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
