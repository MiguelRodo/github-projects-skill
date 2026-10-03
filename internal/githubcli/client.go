package githubcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MiguelRodo/github-projects-skill/internal/buildinfo"
)

// Client is the authenticated GitHub request boundary.
type Client interface {
	GraphQL(context.Context, string, map[string]any) (GraphQLResponse, error)
	REST(context.Context, string, string, any) (RESTResponse, error)
}

type GraphQLError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}
type RESTResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type HTTPError struct {
	Status                int
	Method, Path, Message string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s (%s %s)", e.Message, e.Method, e.Path) }

// HTTPClient's zero value is ready to use. Authentication is resolved once,
// lazily, so local commands such as help do not require credentials or gh.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	tokenOnce  sync.Once
	token      string
	tokenErr   error
}

func (c *HTTPClient) authenticate(ctx context.Context) error {
	c.tokenOnce.Do(func() {
		c.token = strings.TrimSpace(os.Getenv("GH_TOKEN"))
		if c.token == "" {
			c.token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
		}
		if c.token == "" {
			out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
			if err == nil {
				c.token = strings.TrimSpace(string(out))
			}
		}
		if c.token == "" {
			c.tokenErr = errors.New(`GitHub authentication is missing: run "gh auth login" or set GH_TOKEN`)
		}
	})
	return c.tokenErr
}

func (c *HTTPClient) redact(s string) string {
	if c.token != "" {
		s = strings.ReplaceAll(s, c.token, "<redacted>")
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = strings.ToValidUTF8(s[:300], "") + "…"
	}
	return s
}

func operationLine(query string) string {
	// Leading comments are legal in GraphQL documents.
	for _, line := range strings.Split(strings.TrimSpace(query), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if len(line) > 120 {
				line = strings.ToValidUTF8(line[:120], "") + "…"
			}
			return line
		}
	}
	return "query"
}

func isGraphQLQuery(query string) bool {
	line := operationLine(query)
	if !(strings.HasPrefix(line, "{") || strings.HasPrefix(line, "query") && (len(line) == 5 || strings.ContainsAny(line[5:6], " ({\t"))) {
		return false
	}
	// A document can contain several operations. Never retry one that also
	// declares a mutation, even if the first operation is a query. The scanner
	// ignores comments and string literals and only reads top-level keywords.
	depth := 0
	for i := 0; i < len(query); {
		switch query[i] {
		case '#':
			for i < len(query) && query[i] != '\n' {
				i++
			}
		case '"':
			block := strings.HasPrefix(query[i:], `"""`)
			i++
			if block {
				i += 2
			}
			for i < len(query) {
				if query[i] == '\\' {
					i = min(i+2, len(query))
					continue
				}
				if block && strings.HasPrefix(query[i:], `"""`) {
					i += 3
					break
				}
				if !block && query[i] == '"' {
					i++
					break
				}
				i++
			}
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
		default:
			if query[i] >= 'A' && query[i] <= 'Z' || query[i] >= 'a' && query[i] <= 'z' || query[i] == '_' {
				start := i
				for i < len(query) && (query[i] >= 'A' && query[i] <= 'Z' || query[i] >= 'a' && query[i] <= 'z' || query[i] >= '0' && query[i] <= '9' || query[i] == '_') {
					i++
				}
				if depth == 0 && query[start:i] == "mutation" {
					return false
				}
			} else {
				i++
			}
		}
	}
	return true
}

func (c *HTTPClient) GraphQL(ctx context.Context, query string, variables map[string]any) (GraphQLResponse, error) {
	response, err := c.request(ctx, "POST", "/graphql", map[string]any{"query": query, "variables": variables}, isGraphQLQuery(query))
	if err != nil {
		return GraphQLResponse{}, fmt.Errorf("%s (graphql: %s)", c.errorMessage(err), c.redact(operationLine(query)))
	}
	var result GraphQLResponse
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return result, fmt.Errorf("decode GitHub response (graphql: %s)", c.redact(operationLine(query)))
	}
	for i := range result.Errors {
		result.Errors[i].Message = c.redact(result.Errors[i].Message)
	}
	return result, nil
}

func (c *HTTPClient) errorMessage(err error) string {
	var e *HTTPError
	if errors.As(err, &e) {
		return e.Message
	}
	return c.redact(err.Error())
}

func (c *HTTPClient) REST(ctx context.Context, method, path string, body any) (RESTResponse, error) {
	method = strings.ToUpper(method)
	return c.request(ctx, method, path, body, method == "GET" || method == "HEAD" || method == "OPTIONS")
}

func (c *HTTPClient) request(ctx context.Context, method, path string, body any, retry bool) (RESTResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.authenticate(ctx); err != nil {
		return RESTResponse{}, err
	}
	base := c.baseURL
	if base == "" {
		base = "https://api.github.com"
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return RESTResponse{}, errors.New("invalid GitHub API base URL")
	}
	target, err := url.Parse(path)
	if err != nil {
		return RESTResponse{}, errors.New("invalid GitHub API path")
	}
	if !target.IsAbs() {
		target, err = url.Parse(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/"))
	}
	if err != nil || target.Scheme != baseParsed.Scheme || target.Host != baseParsed.Host || target.User != nil {
		return RESTResponse{}, errors.New("GitHub API pagination URL must use the same origin")
	}
	displayPath := c.redact(target.EscapedPath()) // no query values or request bodies in diagnostics
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return RESTResponse{}, fmt.Errorf("encode GitHub request (%s %s)", method, displayPath)
		}
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	// Reject redirects: never forward credentials or replay a mutation.
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(encoded))
		if err != nil {
			return RESTResponse{}, fmt.Errorf("construct GitHub request (%s %s)", method, displayPath)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		req.Header.Set("User-Agent", "projects/"+buildinfo.Current().Version)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := copyClient.Do(req)
		if err != nil {
			return RESTResponse{}, fmt.Errorf("%s (%s %s)", c.redact(err.Error()), method, displayPath)
		}
		data, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		result := RESTResponse{Status: res.StatusCode, Header: res.Header, Body: data}
		if readErr != nil {
			return result, fmt.Errorf("read GitHub response (%s %s)", method, displayPath)
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return result, nil
		}
		if delay, ok := retryDelay(res, attempt); retry && attempt < 2 && ok {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, fmt.Errorf("%s (%s %s)", ctx.Err(), method, displayPath)
			case <-timer.C:
				continue
			}
		}
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		message := c.redact(failure.Message)
		if message == "" {
			message = fmt.Sprintf("GitHub HTTP %d: %s", res.StatusCode, http.StatusText(res.StatusCode))
		}
		return result, &HTTPError{Status: res.StatusCode, Method: method, Path: displayPath, Message: message}
	}
}

func retryDelay(res *http.Response, attempt int) (time.Duration, bool) {
	switch res.StatusCode {
	case 502, 503, 504:
		return time.Duration(attempt+1) * 100 * time.Millisecond, true
	case 403, 429:
		value := res.Header.Get("Retry-After")
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 && seconds <= 10 {
			return time.Duration(seconds) * time.Second, true
		}
		if when, err := http.ParseTime(value); err == nil {
			delay := time.Until(when)
			if delay >= 0 && delay <= 10*time.Second {
				return delay, true
			}
		}
	}
	return 0, false
}

// RESTPages follows Link rel=next, preserving separate JSON page bodies.
// Clients validate absolute next URLs before attaching credentials.
func RESTPages(ctx context.Context, client Client, path string) ([]json.RawMessage, error) {
	var pages []json.RawMessage
	seen := map[string]bool{}
	for path != "" {
		if seen[path] {
			return nil, errors.New("GitHub pagination repeated a page")
		}
		seen[path] = true
		res, err := client.REST(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		pages = append(pages, json.RawMessage(res.Body))
		path = nextLink(res.Header)
	}
	return pages, nil
}
func nextLink(header http.Header) string {
	for _, value := range header.Values("Link") {
		for _, link := range strings.Split(value, ",") {
			parts := strings.Split(strings.TrimSpace(link), ";")
			if len(parts) < 2 {
				continue
			}
			for _, param := range parts[1:] {
				key, val, ok := strings.Cut(strings.TrimSpace(param), "=")
				if ok && key == "rel" && containsRelation(strings.Trim(val, `"`), "next") {
					return strings.Trim(strings.TrimSpace(parts[0]), "<>")
				}
			}
		}
	}
	return ""
}
func containsRelation(value, wanted string) bool {
	for _, rel := range strings.Fields(value) {
		if rel == wanted {
			return true
		}
	}
	return false
}

// graphQLBytes keeps existing decoders' data+errors handling together.
func graphQLBytes(ctx context.Context, client Client, query string, variables map[string]any) ([]byte, error) {
	response, err := client.GraphQL(ctx, query, variables)
	if err != nil {
		return nil, err
	}
	return json.Marshal(response)
}
func graphQLInput(ctx context.Context, client Client, body map[string]any) ([]byte, error) {
	return graphQLBytes(ctx, client, body["query"].(string), body["variables"].(map[string]any))
}
func graphQLWrite(ctx context.Context, client Client, body map[string]any) error {
	response, err := client.GraphQL(ctx, body["query"].(string), body["variables"].(map[string]any))
	if err != nil {
		return err
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("%s (graphql: %s)", response.Errors[0].Message, operationLine(body["query"].(string)))
	}
	return nil
}
func restBytes(ctx context.Context, client Client, method, path string, body any) ([]byte, error) {
	res, err := client.REST(ctx, method, path, body)
	return res.Body, err
}
func restPageBytes(ctx context.Context, client Client, path string) ([]byte, error) {
	pages, err := RESTPages(ctx, client, path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pages)
}
