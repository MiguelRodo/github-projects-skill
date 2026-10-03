package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

type response struct {
	request request
	output  string
	err     error
	// group marks responses that are requested concurrently. Group 0 keeps the
	// strict positional ordering; a non-zero group lets the fake match any
	// not-yet-used response in its contiguous run.
	group int
	used  bool
}
type fakeClient struct {
	mu        sync.Mutex
	t         *testing.T
	responses []response
	index     int
}

func (r *fakeClient) next(actual request) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t.Helper()
	want := canonicalRequest(actual)
	for {
		if r.index >= len(r.responses) {
			r.t.Fatalf("unexpected GitHub request: %s", want)
		}
		current := r.responses[r.index]
		if current.group == 0 {
			r.index++
			if canonicalRequest(current.request) != want {
				r.t.Fatalf("request = %s, want %s", want, canonicalRequest(current.request))
			}
			return []byte(current.output), current.err
		}
		end := r.index
		for end < len(r.responses) && r.responses[end].group == current.group {
			end++
		}
		for i := r.index; i < end; i++ {
			if !r.responses[i].used && canonicalRequest(r.responses[i].request) == want {
				r.responses[i].used = true
				return []byte(r.responses[i].output), r.responses[i].err
			}
		}
		allUsed := true
		for i := r.index; i < end; i++ {
			if !r.responses[i].used {
				allUsed = false
				break
			}
		}
		if !allUsed {
			r.t.Fatalf("unexpected GitHub request in group %d: %s", current.group, want)
		}
		r.index = end
	}
}

// usedCount reports how many responses have been consumed. It counts strictly
// ordered responses already passed plus concurrently matched group responses
// that may still sit at the current index.
func (r *fakeClient) usedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for i, response := range r.responses {
		if i < r.index || response.used {
			count++
		}
	}
	return count
}
func (r *fakeClient) GraphQL(_ context.Context, query string, variables map[string]any) (githubcli.GraphQLResponse, error) {
	output, err := r.next(request{query: query, variables: variables})
	if err != nil {
		return githubcli.GraphQLResponse{}, err
	}

	var response githubcli.GraphQLResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return response, err
	}
	return response, nil
}
func (r *fakeClient) REST(_ context.Context, method, path string, body any) (githubcli.RESTResponse, error) {
	output, err := r.next(request{method: method, path: path, body: body})
	return githubcli.RESTResponse{Status: 200, Body: output}, err
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "skills", "github-projects", "tests", "fixtures", name)
}

func TestContractValidateJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(
		context.Background(),
		[]string{"contract", "validate", "--root", fixture(t, "dispatcher"), "--json"},
		&stdout,
		&stderr,
		&fakeClient{t: t},
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"mode": "dispatcher"`) ||
		!strings.Contains(stdout.String(), `"key": "alpha"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[1/1] Validating") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestProjectItemListJSONKeepsProgressOffStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{query: githubcli.ProjectIdentityQuery, variables: map[string]any{"login": "octo-org", "number": 12}},
			output:  string(projectIdentityFixture([]byte(`{"number":12,"owner":{"login":"octo-org"},"title":"Example planning","url":"https://example.invalid/project"}`))),
		},
		{
			request: request{query: githubcli.ProjectItemsQuery, variables: map[string]any{"login": "octo-org", "number": 12, "first": 100}},
			output:  string(projectItemsFixture([]byte(`{"items":[{"id":"item-1","status":"Todo","content":{"number":7,"repository":"octo-org/example","title":"Do work","type":"Issue"}}],"totalCount":1}`))),
		},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"project", "item-list", "--root", fixture(t, "single"), "--json"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "[1/3]") || !strings.Contains(stdout.String(), `"totalCount": 1`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	for step := 1; step <= 3; step++ {
		if !strings.Contains(stderr.String(), fmt.Sprintf("[%d/3]", step)) {
			t.Fatalf("stderr = %s", stderr.String())
		}
	}
}

func TestProjectItemListTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{query: githubcli.ProjectIdentityQuery, variables: map[string]any{"login": "octo-org", "number": 12}},
			output:  string(projectIdentityFixture([]byte(`{"number":12,"owner":{"login":"octo-org"},"title":"Example planning","url":"https://example.invalid/project"}`))),
		},
		{
			request: request{query: githubcli.ProjectItemsQuery, variables: map[string]any{"login": "octo-org", "number": 12, "first": 100}},
			output:  string(projectItemsFixture([]byte(`{"items":[{"class":"Task","priority":"P2","status":"Todo","content":{"number":7,"repository":"octo-org/example","title":"Do work","type":"Issue"}}],"totalCount":1}`))),
		},
	}}
	exitCode := Run(
		context.Background(),
		[]string{"project", "item-list", "--root", fixture(t, "single"), "--quiet"},
		&stdout,
		&stderr,
		fake,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
	for _, wanted := range []string{"TYPE", "octo-org/example", "P2", "Task", "Do work"} {
		if !strings.Contains(stdout.String(), wanted) {
			t.Fatalf("stdout = %s, missing %q", stdout.String(), wanted)
		}
	}
}

func TestUpdateCheckIsReadOnlyAndReportsDevelopmentBuild(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeClient{t: t, responses: []response{
		{
			request: request{method: "GET", path: "/" + "repos/MiguelRodo/github-projects-skill/releases/latest"},
			output:  string(objectFixture("tag_name", []byte("v0.1.0\n"))),
		},
	}}
	exitCode := Run(context.Background(), []string{"update", "check"}, &stdout, &stderr, fake)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "development build") || strings.Contains(stdout.String(), "sudo") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestUnknownCommandUsesUsageExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"missing"}, &stdout, &stderr, &fakeClient{t: t})
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestSubcommandHelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"project", "item-list", "--help"}, &stdout, &stderr, &fakeClient{t: t})
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: projects project item-list") || stderr.Len() != 0 {
		t.Fatalf("stdout = %s, stderr = %s", stdout.String(), stderr.String())
	}
}

func objectFixture(key string, value []byte) []byte {
	encoded, _ := json.Marshal(map[string]string{key: strings.TrimSpace(string(value))})
	return encoded
}
