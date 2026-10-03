package githubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

const firstItemLimit = 10000

// ProjectIdentity is provider readback for the exact Project selected by the
// repository contract.
type ProjectIdentity struct {
	Number int    `json:"number"`
	Owner  string `json:"owner"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// ProjectItems is a complete, count-checked Project item snapshot.
type ProjectItems struct {
	Project    ProjectIdentity   `json:"project"`
	TotalCount int               `json:"totalCount"`
	Items      []json.RawMessage `json:"items"`
}

type projectView struct {
	Number int `json:"number"`
	Owner  struct {
		Login string `json:"login"`
	} `json:"owner"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type itemList struct {
	Items      []json.RawMessage `json:"items"`
	TotalCount int               `json:"totalCount"`
}

// ReadAllProjectItems verifies Project identity, reads a deliberately
// generous snapshot, and refuses to call a partial response complete. If the
// Project is larger than the first limit, it retries once with the observed
// count plus room for concurrent additions.
func ReadAllProjectItems(ctx context.Context, client Client, expected contract.Project) (ProjectItems, error) {
	identity, err := readProjectIdentity(ctx, client, expected)
	if err != nil {
		return ProjectItems{}, err
	}

	list, err := readItemList(ctx, client, expected, firstItemLimit)
	if err != nil {
		return ProjectItems{}, err
	}
	if len(list.Items) != list.TotalCount && list.TotalCount > firstItemLimit {
		// The margin makes a small concurrent addition likely to be included. The
		// count check below still refuses a changing or incomplete snapshot.
		limit := list.TotalCount + 100
		if limit < list.TotalCount { // integer overflow guard
			limit = list.TotalCount
		}
		list, err = readItemList(ctx, client, expected, limit)
		if err != nil {
			return ProjectItems{}, err
		}
	}
	if len(list.Items) != list.TotalCount {
		return ProjectItems{}, fmt.Errorf(
			"Project item read is incomplete or changed during pagination: received %d of %d items; retry the command",
			len(list.Items), list.TotalCount,
		)
	}

	return ProjectItems{
		Project:    identity,
		TotalCount: list.TotalCount,
		Items:      list.Items,
	}, nil
}

func readProjectIdentity(ctx context.Context, client Client, expected contract.Project) (ProjectIdentity, error) {
	response, err := client.GraphQL(ctx, ProjectIdentityQuery, map[string]any{"login": expected.Owner, "number": expected.Number})
	if err != nil {
		return ProjectIdentity{}, fmt.Errorf("read Project identity: %w", err)
	}
	if len(response.Errors) > 0 {
		return ProjectIdentity{}, fmt.Errorf("read Project identity: %s", response.Errors[0].Message)
	}
	var data struct {
		Owner *struct {
			ProjectV2 *projectView `json:"projectV2"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(response.Data, &data); err != nil {
		return ProjectIdentity{}, fmt.Errorf("decode Project identity: %w", err)
	}
	if data.Owner == nil || data.Owner.ProjectV2 == nil {
		return ProjectIdentity{}, fmt.Errorf("read Project identity: Project not found")
	}
	observed := *data.Owner.ProjectV2

	if observed.Number != expected.Number || observed.Owner.Login != expected.Owner {
		return ProjectIdentity{}, fmt.Errorf(
			"Project identity disagrees with %s: got %s/%d, want %s/%d",
			expected.ContractPath,
			observed.Owner.Login,
			observed.Number,
			expected.Owner,
			expected.Number,
		)
	}
	if observed.Title != expected.Title {
		return ProjectIdentity{}, fmt.Errorf(
			"Project title disagrees with %s: got %q, want %q",
			expected.ContractPath,
			observed.Title,
			expected.Title,
		)
	}
	return ProjectIdentity{
		Number: observed.Number,
		Owner:  observed.Owner.Login,
		Title:  observed.Title,
		URL:    observed.URL,
	}, nil
}

// ProjectIdentityQuery resolves either owner kind without a separate lookup.
const ProjectIdentityQuery = `query($login: String!, $number: Int!) {
  owner: repositoryOwner(login: $login) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        number title url
        owner { ... on User { login } ... on Organization { login } }
      }
    }
  }
}`

// ProjectItemsQuery keeps gh's content and field-value export semantics.
const ProjectItemsQuery = `query($login: String!, $number: Int!, $first: Int!, $cursor: String) {
  owner: repositoryOwner(login: $login) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        items(first: $first, after: $cursor) {
          totalCount pageInfo { hasNextPage endCursor }
          nodes {
            id
            content {
              __typename
              ... on Issue { body title number url repository { nameWithOwner } }
              ... on PullRequest { body title number url repository { nameWithOwner } }
              ... on DraftIssue { id body title }
            }
            fieldValues(first: 100) {
              nodes {
                __typename
                ... on ProjectV2ItemFieldDateValue { date field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldIterationValue { title startDate duration iterationId field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldNumberValue { number field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldMilestoneValue { milestone { title description dueOn } field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldLabelValue { labels(first: 10) { nodes { name } } field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldPullRequestValue { pullRequests(first: 10) { nodes { url } } field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldRepositoryValue { repository { url } field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldUserValue { users(first: 10) { nodes { login } } field { ... on ProjectV2FieldCommon { name } } }
                ... on ProjectV2ItemFieldReviewerValue { reviewers(first: 10) { nodes { __typename ... on Team { name } ... on User { login } } } field { ... on ProjectV2FieldCommon { name } } }
              }
            }
          }
        }
      }
    }
  }
}`

type projectListNode struct {
	ID          string         `json:"id"`
	Content     map[string]any `json:"content"`
	FieldValues struct {
		Nodes []map[string]any `json:"nodes"`
	} `json:"fieldValues"`
}

func readItemList(ctx context.Context, client Client, project contract.Project, limit int) (itemList, error) {
	result := itemList{Items: []json.RawMessage{}}
	cursor := ""
	seen := map[string]bool{}
	for len(result.Items) < limit {
		first := min(100, limit-len(result.Items))
		variables := map[string]any{"login": project.Owner, "number": project.Number, "first": first}
		if cursor != "" {
			variables["cursor"] = cursor
		}
		response, err := client.GraphQL(ctx, ProjectItemsQuery, variables)
		if err != nil {
			return itemList{}, fmt.Errorf("read Project items: %w", err)
		}
		if len(response.Errors) > 0 {
			return itemList{}, fmt.Errorf("read Project items: %s", response.Errors[0].Message)
		}
		var data struct {
			Owner *struct {
				ProjectV2 *struct {
					Items struct {
						Nodes      []projectListNode `json:"nodes"`
						TotalCount int               `json:"totalCount"`
						PageInfo   struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"items"`
				} `json:"projectV2"`
			} `json:"owner"`
		}
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return itemList{}, fmt.Errorf("decode Project items: %w", err)
		}
		if data.Owner == nil || data.Owner.ProjectV2 == nil {
			return itemList{}, fmt.Errorf("read Project items: Project not found")
		}
		page := data.Owner.ProjectV2.Items
		if page.TotalCount < 0 {
			return itemList{}, fmt.Errorf("decode Project items: negative totalCount %d", page.TotalCount)
		}
		result.TotalCount = page.TotalCount
		for _, node := range page.Nodes {
			raw, err := json.Marshal(exportProjectListNode(node))
			if err != nil {
				return itemList{}, err
			}
			result.Items = append(result.Items, raw)
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		cursor = page.PageInfo.EndCursor
		if cursor == "" || seen[cursor] {
			return itemList{}, fmt.Errorf("read Project items: missing or repeated pagination cursor")
		}
		seen[cursor] = true
	}
	return result, nil
}
func exportProjectListNode(node projectListNode) map[string]any {
	result := map[string]any{"id": node.ID, "content": nil}
	if content := node.Content; content != nil {
		kind, _ := content["__typename"].(string)
		switch kind {
		case "Issue", "PullRequest":
			repo, _ := content["repository"].(map[string]any)
			result["content"] = map[string]any{"type": kind, "body": content["body"], "title": content["title"], "number": content["number"], "url": content["url"], "repository": repo["nameWithOwner"]}
		case "DraftIssue":
			draft := map[string]any{"type": kind, "body": content["body"], "title": content["title"]}
			if id, _ := content["id"].(string); id != "" {
				draft["id"] = id
			}
			result["content"] = draft
		}
	}
	for _, field := range node.FieldValues.Nodes {
		definition, _ := field["field"].(map[string]any)
		name, _ := definition["name"].(string)
		if name != "" {
			name = strings.ToLower(name[:1]) + name[1:]
		}
		kind, _ := field["__typename"].(string)
		var value any
		switch kind {
		case "ProjectV2ItemFieldDateValue":
			value = field["date"]
		case "ProjectV2ItemFieldNumberValue":
			value = field["number"]
		case "ProjectV2ItemFieldSingleSelectValue":
			value = field["name"]
		case "ProjectV2ItemFieldTextValue":
			value = field["text"]
		case "ProjectV2ItemFieldIterationValue":
			value = map[string]any{"title": field["title"], "startDate": field["startDate"], "duration": field["duration"], "iterationId": field["iterationId"]}
		case "ProjectV2ItemFieldMilestoneValue":
			milestone, _ := field["milestone"].(map[string]any)
			title, _ := milestone["title"].(string)
			description, _ := milestone["description"].(string)
			dueOn, _ := milestone["dueOn"].(string)
			value = map[string]string{"title": title, "description": description, "dueOn": dueOn}
		case "ProjectV2ItemFieldRepositoryValue":
			repo, _ := field["repository"].(map[string]any)
			value = repo["url"]
		default:
			connection, key := "", ""
			switch kind {
			case "ProjectV2ItemFieldLabelValue":
				connection, key = "labels", "name"
			case "ProjectV2ItemFieldPullRequestValue":
				connection, key = "pullRequests", "url"
			case "ProjectV2ItemFieldUserValue":
				connection, key = "users", "login"
			case "ProjectV2ItemFieldReviewerValue":
				connection, key = "reviewers", "login"
			}
			if connection != "" {
				names := []string{}
				conn, _ := field[connection].(map[string]any)
				nodes, _ := conn["nodes"].([]any)
				for _, n := range nodes {
					item, _ := n.(map[string]any)
					itemKey := key
					if item["__typename"] == "Team" {
						itemKey = "name"
					}
					name, _ := item[itemKey].(string)
					names = append(names, name)
				}
				value = names
			}
		}
		result[name] = value
	}
	return result
}
