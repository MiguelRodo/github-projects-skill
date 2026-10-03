package update

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
	"testing"
)

type fakeClient struct {
	t      *testing.T
	output []byte
	err    error
}

func (f fakeClient) REST(_ context.Context, method, path string, body any) (githubcli.RESTResponse, error) {
	f.t.Helper()
	if method != "GET" || path != "/repos/MiguelRodo/github-projects-skill/releases/latest" || body != nil {
		f.t.Fatalf("unexpected request: %s %s %v", method, path, body)
	}
	output, _ := json.Marshal(map[string]any{"tag_name": string(f.output)})
	return githubcli.RESTResponse{Status: 200, Body: output}, f.err
}
func (f fakeClient) GraphQL(context.Context, string, map[string]any) (githubcli.GraphQLResponse, error) {
	f.t.Fatal("unexpected GraphQL request")
	return githubcli.GraphQLResponse{}, nil
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		latest    string
		want      Result
	}{
		{
			name:      "update available",
			installed: "0.1.0",
			latest:    "v0.2.0\n",
			want:      Result{Installed: "0.1.0", Latest: "0.2.0", UpdateAvailable: true},
		},
		{
			name:      "current",
			installed: "v1.2.3",
			latest:    "v1.2.3\n",
			want:      Result{Installed: "1.2.3", Latest: "1.2.3"},
		},
		{
			name:      "development build",
			installed: "dev",
			latest:    "v1.0.0\n",
			want:      Result{Installed: "dev", Latest: "1.0.0", Development: true},
		},
		{
			name:      "go install pseudo-version",
			installed: "v0.0.0-20260102030405-abcdef123456",
			latest:    "v1.0.0\n",
			want:      Result{Installed: "0.0.0-20260102030405-abcdef123456", Latest: "1.0.0", Development: true},
		},
		{
			name:      "go install tagged module",
			installed: "v0.9.0",
			latest:    "v1.0.0\n",
			want:      Result{Installed: "0.9.0", Latest: "1.0.0", UpdateAvailable: true},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Check(context.Background(), fakeClient{t: t, output: []byte(test.latest)}, test.installed)
			if err != nil {
				t.Fatal(err)
			}
			if result != test.want {
				t.Fatalf("result = %+v, want %+v", result, test.want)
			}
		})
	}
}

func TestCheckClientError(t *testing.T) {
	_, err := Check(context.Background(), fakeClient{t: t, err: errors.New("no release")}, "1.0.0")
	if err == nil {
		t.Fatal("error = nil")
	}
}

func TestCheckBeforeFirstRelease(t *testing.T) {
	result, err := Check(context.Background(), fakeClient{t: t, err: &githubcli.HTTPError{Status: 404, Method: "GET", Path: "/repos/MiguelRodo/github-projects-skill/releases/latest", Message: "Not Found"}}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoPublishedRelease || !result.Development || result.Installed != "dev" {
		t.Fatalf("result = %+v", result)
	}
}

func TestParseVersionRejectsNonReleaseValues(t *testing.T) {
	for _, value := range []string{"1.2", "1.02.3", "1.2.3-beta", ""} {
		if _, err := parseVersion(value); err == nil {
			t.Fatalf("parseVersion(%q) error = nil", value)
		}
	}
}

func TestCheckDoesNotTreat404TextAsAnHTTPStatus(t *testing.T) {
	_, err := Check(context.Background(), fakeClient{t: t, err: errors.New("unrelated HTTP 404 text")}, "1.0.0")
	if err == nil {
		t.Fatal("untyped 404 text was treated as a missing release")
	}
}
