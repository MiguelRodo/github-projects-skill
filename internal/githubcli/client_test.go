package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MiguelRodo/github-projects-skill/internal/buildinfo"
)

func testHTTPClient(t *testing.T, handler http.HandlerFunc) *HTTPClient {
	t.Helper()
	t.Setenv("GH_TOKEN", "synthetic-token")
	t.Setenv("GITHUB_TOKEN", "")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &HTTPClient{baseURL: server.URL, httpClient: server.Client()}
}

func TestHTTPClientHeadersAndTypedGraphQLVariables(t *testing.T) {
	calls := 0
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		for header, want := range map[string]string{"Authorization": "Bearer synthetic-token", "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": githubAPIVersion, "User-Agent": "projects/" + buildinfo.Current().Version, "Content-Type": "application/json"} {
			if got := r.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
		if r.Method != "POST" || r.URL.Path != "/graphql" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Query != "query($n: Int!) { node(number: $n) { id } }" || string(body.Variables["n"]) != "7" {
			t.Errorf("body = %+v", body)
		}
		fmt.Fprint(w, `{"data":{"node":{"id":"ID"}},"errors":[{"message":"partial failure","path":["other"],"extensions":{"type":"FORBIDDEN"}}]}`)
	})
	response, err := client.GraphQL(context.Background(), "query($n: Int!) { node(number: $n) { id } }", map[string]any{"n": 7})
	if err != nil || string(response.Data) != `{"node":{"id":"ID"}}` || len(response.Errors) != 1 || response.Errors[0].Message != "partial failure" || calls != 1 {
		t.Fatalf("response=%+v error=%v calls=%d", response, err, calls)
	}
}

func TestHTTPClientRESTBodyAndHTTPError(t *testing.T) {
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.EscapedPath() != "/repos/owner/repo/issues/7" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(body, map[string]any{"body": "private issue prose", "milestone": nil, "type": "Task"}) {
			t.Errorf("body=%v", body)
		}
		w.Header().Set("X-Request-ID", "trace")
		w.WriteHeader(422)
		fmt.Fprint(w, `{"message":"Validation Failed"}`)
	})
	response, err := client.REST(context.Background(), "PATCH", "/repos/owner/repo/issues/7", map[string]any{"body": "private issue prose", "milestone": nil, "type": "Task"})
	var failure *HTTPError
	if !errors.As(err, &failure) || failure.Status != 422 || failure.Method != "PATCH" || failure.Path != "/repos/owner/repo/issues/7" || err.Error() != "Validation Failed (PATCH /repos/owner/repo/issues/7)" || response.Status != 422 || response.Header.Get("X-Request-ID") != "trace" {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

func TestHTTPClientTokenPrecedence(t *testing.T) {
	for _, test := range []struct{ gh, github, want string }{{"first", "second", "first"}, {"", "second", "second"}} {
		t.Run(test.want, func(t *testing.T) {
			client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer "+test.want {
					t.Errorf("Authorization=%q", got)
				}
				fmt.Fprint(w, `{}`)
			})
			t.Setenv("GH_TOKEN", test.gh)
			t.Setenv("GITHUB_TOKEN", test.github)
			t.Setenv("PATH", t.TempDir())
			if _, err := client.REST(context.Background(), "GET", "/user", nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPClientGHFallbackOnceAndLazy(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "calls")
	// The fake checks every argument and deliberately writes a secret on stderr.
	script := "#!/bin/sh\n[ \"$*\" = 'auth token --hostname github.com' ] || exit 1\nprintf x >> '" + marker + "'\nprintf fallback-token\necho fallback-token >&2\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fallback-token" {
			t.Error("wrong fallback authentication")
		}
		fmt.Fprint(w, `{}`)
	})
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", dir)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("token resolved eagerly")
	}
	for i := 0; i < 2; i++ {
		if _, err := client.REST(context.Background(), "GET", "/user", nil); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "x" {
		t.Fatalf("fallback calls=%q error=%v", data, err)
	}
}

func TestHTTPClientMissingAuthentication(t *testing.T) {
	client := testHTTPClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unauthenticated request sent") })
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	_, err := client.REST(context.Background(), "GET", "/user", nil)
	if err == nil || err.Error() != `GitHub authentication is missing: run "gh auth login" or set GH_TOKEN` {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPClientRESTPagination(t *testing.T) {
	calls := 0
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			if r.URL.RequestURI() != "/items?per_page=2" {
				t.Errorf("first path=%s", r.URL)
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/items?per_page=2&page=1>; rel="prev", <http://%s/items?per_page=2&page=2>; rel="next"`, r.Host, r.Host))
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		case 2:
			if r.URL.RequestURI() != "/items?per_page=2&page=2" {
				t.Errorf("next path=%s", r.URL)
			}
			fmt.Fprint(w, `[{"id":3}]`)
		default:
			t.Error("unexpected extra page")
		}
	})
	pages, err := RESTPages(context.Background(), client, "/items?per_page=2")
	if err != nil || calls != 2 || len(pages) != 2 || string(pages[1]) != `[{"id":3}]` {
		t.Fatalf("pages=%s error=%v calls=%d", pages, err, calls)
	}
}

func TestHTTPClientRetryPolicy(t *testing.T) {
	tests := []struct {
		name, method, query string
		status              int
		retryAfter          string
		want                int
	}{
		{name: "query gateway", query: "query { viewer { login } }", status: 503, want: 3},
		{name: "anonymous query", query: "{ viewer { login } }", status: 502, want: 3},
		{name: "commented mutation", query: "# comment\nmutation { addProjectV2ItemById { item { id } } }", status: 503, want: 1},
		{name: "mixed operation document", query: "query Q { viewer { login } } mutation M { updateIssue { issue { id } } }", status: 503, want: 1},
		{name: "mutation", query: "mutation { updateIssue { issue { id } } }", status: 504, want: 1},
		{name: "POST", method: "POST", status: 503, want: 1},
		{name: "PATCH", method: "PATCH", status: 502, want: 1},
		{name: "DELETE", method: "DELETE", status: 504, want: 1},
		{name: "query throttled", query: "query { viewer { login } }", status: 429, retryAfter: "0", want: 3},
		{name: "GET throttled", method: "GET", status: 403, retryAfter: "0", want: 3},
		{name: "POST throttled", method: "POST", status: 429, retryAfter: "0", want: 1},
		{name: "long throttle", method: "GET", status: 429, retryAfter: "11", want: 1},
		{name: "unmarked throttle", method: "GET", status: 403, want: 1},
		{name: "ordinary error", method: "GET", status: 500, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, `{"message":"temporary failure"}`)
			})
			var err error
			if test.query != "" {
				_, err = client.GraphQL(context.Background(), test.query, nil)
			} else {
				_, err = client.REST(context.Background(), test.method, "/test", map[string]any{"title": "x"})
			}
			if err == nil || calls != test.want {
				t.Fatalf("calls=%d want=%d error=%v", calls, test.want, err)
			}
		})
	}
}

func TestHTTPClientQuerySucceedsAfter503(t *testing.T) {
	calls := 0
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"message":"busy"}`)
			return
		}
		fmt.Fprint(w, `{"data":{"viewer":{"login":"octo"}}}`)
	})
	response, err := client.GraphQL(context.Background(), "query { viewer { login } }", nil)
	if err != nil || calls != 2 || string(response.Data) != `{"viewer":{"login":"octo"}}` {
		t.Fatalf("response=%+v calls=%d error=%v", response, calls, err)
	}
}

func TestHTTPClientErrorsDoNotExposeSecretsOrLargeInputs(t *testing.T) {
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"message":"denied synthetic-token"}`)
	})
	_, err := client.REST(context.Background(), "POST", "/issues?private=synthetic-token", map[string]any{"body": strings.Repeat("private prose", 100)})
	if err == nil || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "private prose") || !strings.HasPrefix(err.Error(), "denied <redacted>") {
		t.Fatalf("error=%v", err)
	}
	query := "query($body: String!) {\n  viewer { login }\n}"
	_, err = client.GraphQL(context.Background(), query, map[string]any{"body": "private prose"})
	if err == nil || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "viewer") || !strings.Contains(err.Error(), "(graphql: query($body: String!) {") {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPClientContextCancellationAndRedirects(t *testing.T) {
	client := testHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "https://example.invalid/secret")
			w.WriteHeader(307)
			return
		}
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(429)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.REST(ctx, "GET", "/slow", nil)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("cancellation error=%v", err)
	}
	_, err = client.REST(context.Background(), "POST", "/redirect", map[string]any{})
	var failure *HTTPError
	if !errors.As(err, &failure) || failure.Status != 307 {
		t.Fatalf("redirect error=%v", err)
	}
	_, err = client.REST(context.Background(), "GET", "https://example.invalid/next", nil)
	if err == nil {
		t.Fatal("accepted cross-origin pagination")
	}
}

func TestOperationLineBoundsDiagnosticWithoutBodyOrFullQuery(t *testing.T) {
	query := "query($login: String!) {\n  user(login: $login) {\n    id\n  }\n}"
	if got := operationLine(query); got != "query($login: String!) {" {
		t.Fatalf("operation=%q", got)
	}
	if got := operationLine(strings.Repeat("x", 300)); len(got) > 123 {
		t.Fatalf("unbounded operation: %d bytes", len(got))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPClientDoesNotRetryAmbiguousFailuresAndBoundsTimeout(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			client := testHTTPClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected server request") })
			client.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 30*time.Second {
					t.Error("request has no 30-second timeout")
				}
				return nil, errors.New("synthetic-token transport failed")
			})}
			_, err := client.REST(context.Background(), method, "/test", map[string]any{"body": "private prose"})
			if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), "private prose") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestHTTPClientSuppressesFailedGHTokenOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf 'failed-secret-token' >&2\nprintf 'failed-secret-token'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", dir)
	_, err := client.REST(context.Background(), "GET", "/user", nil)
	if err == nil || strings.Contains(err.Error(), "failed-secret-token") || !strings.HasPrefix(err.Error(), "GitHub authentication is missing") {
		t.Fatalf("error=%v", err)
	}
}
