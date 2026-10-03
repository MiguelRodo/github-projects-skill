package githubcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummariseArgsAbbreviatesLargeValues(t *testing.T) {
	query := "query($login: String!) {\n  user(login: $login) {\n    id\n  }\n}"
	body := strings.Repeat("line of prose\n", 40)
	long := strings.Repeat("x", 300)
	got := summariseArgs([]string{
		"api", "graphql", "-f", "query=" + query, "-f", "login=octo",
		"issue", "edit", "7", "--body", body, "--body-file", "-",
		"-f", "body=" + body, "--title", long,
	})
	for _, want := range []string{
		"api graphql -f query=<graphql 5 lines> -f login=octo",
		"--body <body 560 bytes>",
		"--body-file -",
		"-f body=<body 560 bytes>",
		"--title " + strings.Repeat("x", maxArgDisplay) + "...<300 bytes>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "user(login") || strings.Contains(got, "line of prose") {
		t.Fatalf("summary leaked large values: %q", got)
	}
}

func TestExecRunnerPutsGHStderrFirst(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'GraphQL: Could not resolve to a ProjectV2' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	_, err := ExecRunner{}.Run(context.Background(), "api", "graphql", "-f", "query=query {\n viewer { login }\n}")
	if err == nil {
		t.Fatal("expected an error")
	}
	want := "GraphQL: Could not resolve to a ProjectV2 (command: gh api graphql -f query=<graphql 3 lines>)"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
