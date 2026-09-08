package update

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	t      *testing.T
	output []byte
	err    error
}

func (f fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.t.Helper()
	want := []string{"api", "repos/MiguelRodo/github-projects-skill/releases/latest", "--jq", ".tag_name"}
	if !reflect.DeepEqual(args, want) {
		f.t.Fatalf("args = %v, want %v", args, want)
	}
	return f.output, f.err
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
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Check(context.Background(), fakeRunner{t: t, output: []byte(test.latest)}, test.installed)
			if err != nil {
				t.Fatal(err)
			}
			if result != test.want {
				t.Fatalf("result = %+v, want %+v", result, test.want)
			}
		})
	}
}

func TestCheckRunnerError(t *testing.T) {
	_, err := Check(context.Background(), fakeRunner{t: t, err: errors.New("no release")}, "1.0.0")
	if err == nil {
		t.Fatal("error = nil")
	}
}

func TestCheckBeforeFirstRelease(t *testing.T) {
	result, err := Check(context.Background(), fakeRunner{t: t, err: errors.New("gh: Not Found (HTTP 404)")}, "dev")
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
