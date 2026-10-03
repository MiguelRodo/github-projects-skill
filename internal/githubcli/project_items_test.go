package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

type request struct {
	query        string
	variables    map[string]any
	method, path string
	body         any
}

func canonicalRequest(r request) string {
	encoded, _ := json.Marshal(r.variables)
	body, _ := json.Marshal(r.body)
	return r.method + " " + r.path + " " + r.query + " " + string(encoded) + " " + string(body)
}

// issueFixture turns the concise issue fixtures into provider connection data.
func issueFixture(output []byte) []byte {
	var issue map[string]any
	if json.Unmarshal(output, &issue) != nil {
		return output
	}
	for _, name := range []string{"labels", "assignees"} {
		issue[name] = map[string]any{"nodes": issue[name]}
	}
	var items []any
	if projects, ok := issue["projectItems"].([]any); ok {
		for _, raw := range projects {
			item := raw.(map[string]any)
			items = append(items, map[string]any{"project": map[string]any{"title": item["title"]}, "status": item["status"]})
		}
	}
	issue["projectItems"] = map[string]any{"nodes": items}
	encoded, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": issue}}})
	return encoded
}
func projectIdentityFixture(output []byte) []byte {
	var project any
	if json.Unmarshal(output, &project) != nil {
		return output
	}
	encoded, _ := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"projectV2": project}}})
	return encoded
}
func projectItemsFixture(output []byte) []byte {
	var list struct {
		Items      []map[string]any `json:"items"`
		TotalCount int              `json:"totalCount"`
	}
	if json.Unmarshal(output, &list) != nil {
		return output
	}
	var nodes []any
	for _, item := range list.Items {
		content, _ := item["content"].(map[string]any)
		if content != nil {
			content["__typename"] = content["type"]
			delete(content, "type")
			content["repository"] = map[string]any{"nameWithOwner": content["repository"]}
		}
		fields := []any{}
		for k, v := range item {
			if k == "id" || k == "content" {
				continue
			}
			fields = append(fields, map[string]any{"__typename": "ProjectV2ItemFieldTextValue", "text": v, "field": map[string]any{"name": k}})
		}
		nodes = append(nodes, map[string]any{"id": item["id"], "content": content, "fieldValues": map[string]any{"nodes": fields}})
	}
	encoded, _ := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"projectV2": map[string]any{"items": map[string]any{"nodes": nodes, "totalCount": list.TotalCount}}}}})
	return encoded
}

type fakeResponse struct {
	request request
	header  http.Header
	input   []byte
	output  []byte
	err     error
}
type fakeClient struct {
	t         *testing.T
	responses []fakeResponse
	calls     int
}

func (f *fakeClient) next(actual request) ([]byte, error) {
	f.t.Helper()
	if f.calls >= len(f.responses) {
		f.t.Fatalf("unexpected request %d: %s", f.calls+1, canonicalRequest(actual))
	}
	response := f.responses[f.calls]
	f.calls++
	expected := response.request
	if response.input != nil {
		if expected.method == "" {
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.Unmarshal(response.input, &body); err != nil {
				f.t.Fatal(err)
			}
			expected.query, expected.variables = body.Query, body.Variables
		} else {
			var body any
			if err := json.Unmarshal(response.input, &body); err != nil {
				f.t.Fatal(err)
			}
			expected.body = body
		}
	}
	if canonicalRequest(actual) != canonicalRequest(expected) {
		f.t.Fatalf("request %d = %s, want %s", f.calls, canonicalRequest(actual), canonicalRequest(expected))
	}
	return response.output, response.err
}
func (f *fakeClient) GraphQL(_ context.Context, query string, variables map[string]any) (GraphQLResponse, error) {
	output, err := f.next(request{query: query, variables: variables})
	if err != nil {
		return GraphQLResponse{}, err
	}

	var response GraphQLResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return response, err
	}
	return response, nil
}
func (f *fakeClient) REST(_ context.Context, method, path string, body any) (RESTResponse, error) {
	output, err := f.next(request{method: method, path: path, body: body})
	return RESTResponse{Status: 200, Header: f.responses[f.calls-1].header, Body: output}, err
}

func testProject() contract.Project {
	return contract.Project{
		Owner:        "octo-user",
		Number:       40,
		Title:        "Planning",
		ContractPath: "/work/repo/.projects/project.md",
	}
}

func projectViewJSON(owner, title string, number int) []byte {
	return []byte(fmt.Sprintf(
		`{"number":%d,"owner":{"login":%q},"title":%q,"url":"https://example.invalid/project"}`,
		number, owner, title,
	))
}

func itemListJSON(t *testing.T, count, total int) []byte {
	t.Helper()
	items := make([]json.RawMessage, count)
	for index := range items {
		items[index] = json.RawMessage(fmt.Sprintf(`{"id":"item-%d","title":"Item %d"}`, index, index))
	}
	output, err := json.Marshal(itemList{Items: items, TotalCount: total})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestReadAllProjectItemsComplete(t *testing.T) {
	project := testProject()
	runner := &fakeClient{t: t, responses: []fakeResponse{
		{
			request: request{query: ProjectIdentityQuery, variables: map[string]any{"login": "octo-user", "number": 40}},
			output:  projectIdentityFixture(projectViewJSON("octo-user", "Planning", 40)),
		},
		{
			request: request{query: ProjectItemsQuery, variables: map[string]any{"login": "octo-user", "number": 40, "first": 100}},
			output:  projectItemsFixture(itemListJSON(t, 2, 2)),
		},
	}}

	snapshot, err := ReadAllProjectItems(context.Background(), runner, project)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalCount != 2 || len(snapshot.Items) != 2 {
		t.Fatalf("snapshot counts = %d/%d, want 2/2", len(snapshot.Items), snapshot.TotalCount)
	}
	if snapshot.Project.Owner != "octo-user" || snapshot.Project.Number != 40 {
		t.Fatalf("snapshot Project = %+v", snapshot.Project)
	}
	if runner.calls != len(runner.responses) {
		t.Fatalf("runner calls = %d, want %d", runner.calls, len(runner.responses))
	}
}

func TestReadAllProjectItemsRetriesLargeProjectWithObservedCount(t *testing.T) {
	project := testProject()
	total := firstItemLimit + 1
	steps := []fakeResponse{{request: request{query: ProjectIdentityQuery, variables: map[string]any{"login": "octo-user", "number": 40}}, output: projectIdentityFixture(projectViewJSON("octo-user", "Planning", 40))}}
	steps = append(steps, inventoryPages(t, project, total, firstItemLimit)...)
	steps = append(steps, inventoryPages(t, project, total, total+100)...)
	runner := &fakeClient{t: t, responses: steps}

	snapshot, err := ReadAllProjectItems(context.Background(), runner, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != total || snapshot.TotalCount != total {
		t.Fatalf("snapshot counts = %d/%d, want %d/%d", len(snapshot.Items), snapshot.TotalCount, total, total)
	}
	if runner.calls != 202 {
		t.Fatalf("calls=%d, want identity + 100 capped pages + 101 complete pages", runner.calls)
	}

}

func TestReadAllProjectItemsRejectsPartialResponse(t *testing.T) {
	project := testProject()
	runner := &fakeClient{t: t, responses: []fakeResponse{
		{
			request: request{query: ProjectIdentityQuery, variables: map[string]any{"login": "octo-user", "number": 40}},
			output:  projectIdentityFixture(projectViewJSON("octo-user", "Planning", 40)),
		},
		{
			request: request{query: ProjectItemsQuery, variables: map[string]any{"login": "octo-user", "number": 40, "first": 100}},
			output:  projectItemsFixture(itemListJSON(t, 1, 2)),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v, want incomplete response", err)
	}
}

func TestReadAllProjectItemsRejectsIdentityMismatch(t *testing.T) {
	project := testProject()
	runner := &fakeClient{t: t, responses: []fakeResponse{
		{
			request: request{query: ProjectIdentityQuery, variables: map[string]any{"login": "octo-user", "number": 40}},
			output:  projectIdentityFixture(projectViewJSON("another-user", "Planning", 40)),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "identity disagrees") {
		t.Fatalf("error = %v, want identity mismatch", err)
	}
}

func TestReadAllProjectItemsPreservesClientError(t *testing.T) {
	project := testProject()
	runner := &fakeClient{t: t, responses: []fakeResponse{
		{
			request: request{query: ProjectIdentityQuery, variables: map[string]any{"login": "octo-user", "number": 40}},
			err:     errors.New("authentication failed"),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("error = %v, want runner error", err)
	}
}

func objectFixture(key string, value []byte) []byte {
	encoded, _ := json.Marshal(map[string]string{key: strings.TrimSpace(string(value))})
	return encoded
}

func inventoryPages(t *testing.T, project contract.Project, total, limit int) []fakeResponse {
	t.Helper()
	var steps []fakeResponse
	for offset := 0; offset < min(total, limit); offset += 100 {
		first := min(100, limit-offset)
		count := min(first, total-offset)
		nodes := make([]projectListNode, count)
		for i := range nodes {
			nodes[i].ID = fmt.Sprintf("item-%d", offset+i)
		}
		variables := map[string]any{"login": project.Owner, "number": project.Number, "first": first}
		if offset > 0 {
			variables["cursor"] = fmt.Sprintf("cursor-%d", offset)
		}
		output, _ := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"projectV2": map[string]any{"items": map[string]any{"nodes": nodes, "totalCount": total, "pageInfo": map[string]any{"hasNextPage": offset+count < total, "endCursor": fmt.Sprintf("cursor-%d", offset+count)}}}}}})
		steps = append(steps, fakeResponse{request: request{query: ProjectItemsQuery, variables: variables}, output: output})
	}
	return steps
}
func TestProjectInventoryExportKeepsContentAndFieldShapes(t *testing.T) {
	raw := `{"id":"item","content":{"__typename":"Issue","title":"T","body":"B","number":1,"url":"https://github.com/owner/repo/issues/1","repository":{"nameWithOwner":"owner/repo"}},"fieldValues":{"nodes":[
  {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"Done","field":{"name":"Status"}},
  {"__typename":"ProjectV2ItemFieldIterationValue","title":"Sprint","startDate":"2026-10-01","duration":7,"iterationId":"iter","field":{"name":"Iteration"}},
  {"__typename":"ProjectV2ItemFieldMilestoneValue","milestone":{"title":"Release","description":null,"dueOn":null},"field":{"name":"Milestone"}},
  {"__typename":"ProjectV2ItemFieldLabelValue","labels":{"nodes":[{"name":"bug"}]},"field":{"name":"Labels"}},
  {"__typename":"ProjectV2ItemFieldUserValue","users":{"nodes":[{"login":"octo"}]},"field":{"name":"Assignees"}},
  {"__typename":"ProjectV2ItemFieldReviewerValue","reviewers":{"nodes":[{"__typename":"User","login":"octo"},{"__typename":"Team","name":"team"}]},"field":{"name":"Reviewers"}}
 ]}}`
	var node projectListNode
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(exportProjectListNode(node))
	want := json.RawMessage(`{"id":"item","content":{"type":"Issue","title":"T","body":"B","number":1,"url":"https://github.com/owner/repo/issues/1","repository":"owner/repo"},"status":"Done","iteration":{"title":"Sprint","startDate":"2026-10-01","duration":7,"iterationId":"iter"},"milestone":{"title":"Release","description":"","dueOn":""},"labels":["bug"],"assignees":["octo"],"reviewers":["octo","team"]}`)
	if canonicalRawSet([]json.RawMessage{got})[0] != canonicalRawSet([]json.RawMessage{want})[0] {
		t.Fatalf("export=%s", got)
	}
}
