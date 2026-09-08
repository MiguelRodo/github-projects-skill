package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

type fakeResponse struct {
	args   []string
	input  []byte
	output []byte
	err    error
}

type fakeRunner struct {
	t         *testing.T
	responses []fakeResponse
	calls     int
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.t.Helper()
	return f.next(nil, args...)
}

func (f *fakeRunner) RunInput(_ context.Context, input []byte, args ...string) ([]byte, error) {
	f.t.Helper()
	return f.next(input, args...)
}

func (f *fakeRunner) next(input []byte, args ...string) ([]byte, error) {
	f.t.Helper()
	if f.calls >= len(f.responses) {
		f.t.Fatalf("unexpected call: %v", args)
	}
	response := f.responses[f.calls]
	f.calls++
	if !reflect.DeepEqual(args, response.args) {
		f.t.Fatalf("call %d args = %v, want %v", f.calls, args, response.args)
	}
	if !reflect.DeepEqual(input, response.input) {
		f.t.Fatalf("call %d input = %s, want %s", f.calls, input, response.input)
	}
	return response.output, response.err
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
	runner := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"project", "view", "40", "--owner", "octo-user", "--format", "json"},
			output: projectViewJSON("octo-user", "Planning", 40),
		},
		{
			args:   []string{"project", "item-list", "40", "--owner", "octo-user", "--limit", strconv.Itoa(firstItemLimit), "--format", "json"},
			output: itemListJSON(t, 2, 2),
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
	runner := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"project", "view", "40", "--owner", "octo-user", "--format", "json"},
			output: projectViewJSON("octo-user", "Planning", 40),
		},
		{
			args:   []string{"project", "item-list", "40", "--owner", "octo-user", "--limit", strconv.Itoa(firstItemLimit), "--format", "json"},
			output: itemListJSON(t, firstItemLimit, total),
		},
		{
			args:   []string{"project", "item-list", "40", "--owner", "octo-user", "--limit", strconv.Itoa(total + 100), "--format", "json"},
			output: itemListJSON(t, total, total),
		},
	}}

	snapshot, err := ReadAllProjectItems(context.Background(), runner, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != total || snapshot.TotalCount != total {
		t.Fatalf("snapshot counts = %d/%d, want %d/%d", len(snapshot.Items), snapshot.TotalCount, total, total)
	}
}

func TestReadAllProjectItemsRejectsPartialResponse(t *testing.T) {
	project := testProject()
	runner := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"project", "view", "40", "--owner", "octo-user", "--format", "json"},
			output: projectViewJSON("octo-user", "Planning", 40),
		},
		{
			args:   []string{"project", "item-list", "40", "--owner", "octo-user", "--limit", strconv.Itoa(firstItemLimit), "--format", "json"},
			output: itemListJSON(t, 1, 2),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v, want incomplete response", err)
	}
}

func TestReadAllProjectItemsRejectsIdentityMismatch(t *testing.T) {
	project := testProject()
	runner := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args:   []string{"project", "view", "40", "--owner", "octo-user", "--format", "json"},
			output: projectViewJSON("another-user", "Planning", 40),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "identity disagrees") {
		t.Fatalf("error = %v, want identity mismatch", err)
	}
}

func TestReadAllProjectItemsPreservesRunnerError(t *testing.T) {
	project := testProject()
	runner := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args: []string{"project", "view", "40", "--owner", "octo-user", "--format", "json"},
			err:  errors.New("authentication failed"),
		},
	}}

	_, err := ReadAllProjectItems(context.Background(), runner, project)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("error = %v, want runner error", err)
	}
}
