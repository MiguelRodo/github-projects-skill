package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type response struct {
	args   []string
	output string
	err    error
}

type runner struct {
	t         *testing.T
	responses []response
	index     int
}

func (r *runner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.t.Helper()
	if r.index >= len(r.responses) {
		r.t.Fatalf("unexpected gh call: %v", args)
	}
	response := r.responses[r.index]
	r.index++
	if !reflect.DeepEqual(args, response.args) {
		r.t.Fatalf("gh args = %v, want %v", args, response.args)
	}
	return []byte(response.output), response.err
}

func (r *runner) RunInput(_ context.Context, input []byte, args ...string) ([]byte, error) {
	r.t.Helper()
	r.t.Fatalf("unexpected gh input call: %v (input %s)", args, input)
	return nil, nil
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
		&runner{t: t},
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
	fake := &runner{t: t, responses: []response{
		{
			args:   []string{"project", "view", "12", "--owner", "octo-org", "--format", "json"},
			output: `{"number":12,"owner":{"login":"octo-org"},"title":"Example planning","url":"https://example.invalid/project"}`,
		},
		{
			args:   []string{"project", "item-list", "12", "--owner", "octo-org", "--limit", "10000", "--format", "json"},
			output: `{"items":[{"id":"item-1","status":"Todo","content":{"number":7,"repository":"octo-org/example","title":"Do work","type":"Issue"}}],"totalCount":1}`,
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
	fake := &runner{t: t, responses: []response{
		{
			args:   []string{"project", "view", "12", "--owner", "octo-org", "--format", "json"},
			output: `{"number":12,"owner":{"login":"octo-org"},"title":"Example planning","url":"https://example.invalid/project"}`,
		},
		{
			args:   []string{"project", "item-list", "12", "--owner", "octo-org", "--limit", "10000", "--format", "json"},
			output: `{"items":[{"class":"Task","priority":"P2","status":"Todo","content":{"number":7,"repository":"octo-org/example","title":"Do work","type":"Issue"}}],"totalCount":1}`,
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
	fake := &runner{t: t, responses: []response{
		{
			args:   []string{"api", "repos/MiguelRodo/github-projects-skill/releases/latest", "--jq", ".tag_name"},
			output: "v0.1.0\n",
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
	exitCode := Run(context.Background(), []string{"missing"}, &stdout, &stderr, &runner{t: t})
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestSubcommandHelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"project", "item-list", "--help"}, &stdout, &stderr, &runner{t: t})
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: projects project item-list") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
