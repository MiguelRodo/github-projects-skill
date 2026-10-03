package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
)

// IssueView represents the inspected state of an issue.
type IssueView struct {
	Number       int               `json:"number"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	State        string            `json:"state"`
	StateReason  string            `json:"stateReason,omitempty"`
	URL          string            `json:"url"`
	Labels       []IssueLabel      `json:"labels"`
	Assignees    []IssueAssignee   `json:"assignees"`
	Milestone    *IssueMilestone   `json:"milestone,omitempty"`
	IssueType    *IssueType        `json:"issueType,omitempty"`
	ProjectItems []json.RawMessage `json:"projectItems,omitempty"`
	// AutomationSideEffects is never read from GitHub. EditIssue fills it with
	// Project Status changes made by built-in workflows on close or reopen.
	AutomationSideEffects []string `json:"automationSideEffects,omitempty"`
	// Changed is never read from GitHub. EditIssue reports whether it actually
	// modified the issue; a verified no-op leaves it false.
	Changed bool `json:"-"`
}

// IssueLabel is an issue label name.
type IssueLabel struct {
	Name string `json:"name"`
}

// IssueAssignee is an assigned user's login.
type IssueAssignee struct {
	Login string `json:"login"`
}

// IssueMilestone is an assigned milestone title.
type IssueMilestone struct {
	Title string `json:"title"`
}

// IssueType is an organisation-native GitHub issue type.
type IssueType struct {
	Name string `json:"name"`
}

// IssueSummary is the stable subset used for exact-title duplicate checks.
type IssueSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// CreateIssueInput holds parameters for creating an issue.
type CreateIssueInput struct {
	Repo      string
	Title     string
	Body      string
	Labels    []string
	Assignees []string
	Milestone string
}

// EditIssueInput holds parameters for editing an existing issue.
type EditIssueInput struct {
	Repo            string
	Number          int
	Title           *string
	Body            *string
	AddLabels       []string
	RemoveLabels    []string
	AddAssignees    []string
	RemoveAssignees []string
	Milestone       *string
	IssueType       *string // empty removes the current issue type
	State           *string // "open" or "closed"
	CloseReason     string  // "completed" or "not_planned"; empty defaults to completed when closing an open issue
}

// closeReasons maps REST close reasons to GraphQL stateReason values.
var closeReasons = map[string]struct{ state string }{
	"completed":   {state: "COMPLETED"},
	"not_planned": {state: "NOT_PLANNED"},
}

func normalizeCloseReason(reason string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(reason)), " ", "_")
}

// IssueViewQuery reads an issue and its Project Status summaries in one request.
const IssueViewQuery = `query($owner: String!, $name: String!, $number: Int!, $labelsCursor: String, $assigneesCursor: String, $projectsCursor: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      number title body state stateReason url
      labels(first: 100, after: $labelsCursor) { nodes { name } pageInfo { hasNextPage endCursor } }
      assignees(first: 100, after: $assigneesCursor) { nodes { login } pageInfo { hasNextPage endCursor } }
      milestone { title }
      issueType { name }
      projectItems(first: 100, after: $projectsCursor) {
        nodes {
          project { title }
          status: fieldValueByName(name: "Status") {
            ... on ProjectV2ItemFieldSingleSelectValue { name optionId }
          }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

func ViewIssue(ctx context.Context, client Client, repo string, number int) (IssueView, error) {
	owner, name, err := splitGitHubRepository(repo)
	if err != nil {
		return IssueView{}, err
	}
	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	var data struct {
		Repository *struct {
			Issue *struct {
				Number      int    `json:"number"`
				Title       string `json:"title"`
				Body        string `json:"body"`
				State       string `json:"state"`
				StateReason string `json:"stateReason"`
				URL         string `json:"url"`
				Labels      struct {
					Nodes    []IssueLabel `json:"nodes"`
					PageInfo pageInfo     `json:"pageInfo"`
				} `json:"labels"`
				Assignees struct {
					Nodes    []IssueAssignee `json:"nodes"`
					PageInfo pageInfo        `json:"pageInfo"`
				} `json:"assignees"`
				Milestone    *IssueMilestone `json:"milestone"`
				IssueType    *IssueType      `json:"issueType"`
				ProjectItems struct {
					Nodes []*struct {
						Project struct {
							Title string `json:"title"`
						} `json:"project"`
						Status struct {
							Name     string `json:"name"`
							OptionID string `json:"optionId"`
						} `json:"status"`
					} `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"projectItems"`
			} `json:"issue"`
		} `json:"repository"`
	}

	variables := map[string]any{"owner": owner, "name": name, "number": number}
	pending := map[string]bool{"labelsCursor": true, "assigneesCursor": true, "projectsCursor": true}
	seen := map[string]map[string]bool{"labelsCursor": {}, "assigneesCursor": {}, "projectsCursor": {}}
	var view IssueView
	for page := 0; ; page++ {
		response, err := client.GraphQL(ctx, IssueViewQuery, variables)
		if err != nil {
			return IssueView{}, fmt.Errorf("view issue %s#%d: %w", repo, number, err)
		}
		if len(response.Errors) > 0 {
			return IssueView{}, fmt.Errorf("view issue %s#%d: %s", repo, number, response.Errors[0].Message)
		}
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return IssueView{}, fmt.Errorf("decode issue view: %w", err)
		}
		if data.Repository == nil || data.Repository.Issue == nil {
			return IssueView{}, fmt.Errorf("view issue %s#%d: issue not found", repo, number)
		}
		issue := data.Repository.Issue
		if page == 0 {
			view = IssueView{Number: issue.Number, Title: issue.Title, Body: issue.Body, State: issue.State, StateReason: issue.StateReason, URL: issue.URL, Labels: []IssueLabel{}, Assignees: []IssueAssignee{}, Milestone: issue.Milestone, IssueType: issue.IssueType}
		}
		if pending["labelsCursor"] {
			view.Labels = append(view.Labels, issue.Labels.Nodes...)
		}
		if pending["assigneesCursor"] {
			view.Assignees = append(view.Assignees, issue.Assignees.Nodes...)
		}
		if pending["projectsCursor"] {
			for _, item := range issue.ProjectItems.Nodes {
				if item == nil {
					continue
				}
				raw, _ := json.Marshal(struct {
					Title  string `json:"title"`
					Status any    `json:"status"`
				}{item.Project.Title, item.Status})
				view.ProjectItems = append(view.ProjectItems, raw)
			}
		}
		next := false
		for key, info := range map[string]pageInfo{"labelsCursor": issue.Labels.PageInfo, "assigneesCursor": issue.Assignees.PageInfo, "projectsCursor": issue.ProjectItems.PageInfo} {
			if !pending[key] {
				continue
			}
			pending[key] = info.HasNextPage
			if !info.HasNextPage {
				continue
			}
			if info.EndCursor == "" || seen[key][info.EndCursor] {
				return IssueView{}, fmt.Errorf("view issue %s#%d: missing or repeated %s pagination cursor", repo, number, key)
			}
			seen[key][info.EndCursor] = true
			variables[key] = info.EndCursor
			next = true
		}
		if !next {
			return view, nil
		}
		// Overflow connections reuse the same document. Ordinary reads need one
		// request; completed connections are ignored on subsequent pages.
		data.Repository = nil
	}
}

// Exact-title duplicate check methods reported in ExactTitleCheck.Method.
const (
	// ExactTitleMethodSearchRecent is a complete search over open and closed
	// issues plus the newest page of recent open issues for search-index lag.
	ExactTitleMethodSearchRecent = "search+recent"
	// ExactTitleMethodRecentOpen is the titles-only scan of open issues created
	// within ExactTitleRecentWindow, used whenever search cannot be trusted.
	ExactTitleMethodRecentOpen = "recent-open"
)

// ExactTitleCheck is the result of an exact-title duplicate check.
type ExactTitleCheck struct {
	// Matches are the exact-title issues, sorted by ascending number.
	Matches []IssueSummary `json:"-"`
	// Method is ExactTitleMethodSearchRecent or ExactTitleMethodRecentOpen.
	Method string `json:"method"`
	// RecentWindow is the creation-time window of the recent-open scan.
	RecentWindow string `json:"recentWindow"`
	// Complete is false only when the recent-open scan stopped at
	// ExactTitleRecentScanLimit while still inside RecentWindow.
	Complete bool `json:"complete"`
	// Unchecked names the issues the check did not cover, if any.
	Unchecked string `json:"unchecked,omitempty"`
}

// FindIssuesByExactTitle checks for issues whose title exactly matches title
// after trimming its outer whitespace. Pull requests are excluded.
//
// It first asks the Search API for title-phrase candidates across open and
// closed issues and reads one titles-only GraphQL page of recently created
// open issues to cover search-index lag, then keeps only exact matches.
// Whenever search cannot be trusted to be complete, it instead reads the
// titles of open issues created within ExactTitleRecentWindow, at most
// ExactTitleRecentScanLimit of them, and reports in Unchecked that older and
// closed issues were not checked.
func FindIssuesByExactTitle(ctx context.Context, client Client, repo, title string) (ExactTitleCheck, error) {
	normalizedTitle := strings.TrimSpace(title)
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return ExactTitleCheck{}, fmt.Errorf("invalid repository %q: want OWNER/REPO", repo)
	}
	byNumber, searchOK := findIssuesByExactTitleViaSearch(ctx, client, repo, normalizedTitle)
	if err := ctx.Err(); err != nil {
		return ExactTitleCheck{}, fmt.Errorf("check %s for an exact-title match: %w", repo, err)
	}
	check := ExactTitleCheck{Method: ExactTitleMethodSearchRecent, RecentWindow: exactTitleRecentWindowLabel}
	maxPages := 1
	if !searchOK {
		byNumber = map[int]IssueSummary{}
		check.Method = ExactTitleMethodRecentOpen
		maxPages = ExactTitleRecentScanLimit / exactTitlePageSize
	}
	cutoff := exactTitleNow().Add(-ExactTitleRecentWindow)
	recent, capped, err := recentOpenIssuesByExactTitle(ctx, client, owner, name, normalizedTitle, cutoff, maxPages)
	if err != nil {
		return ExactTitleCheck{}, err
	}
	for _, issue := range recent {
		byNumber[issue.Number] = issue
	}
	check.Complete = true
	if !searchOK {
		check.Complete = !capped
		check.Unchecked = "closed issues and issues created more than " + exactTitleRecentWindowLabel + " ago"
		if capped {
			check.Unchecked = fmt.Sprintf("closed issues and open issues beyond the newest %d created in the last %s", ExactTitleRecentScanLimit, exactTitleRecentWindowLabel)
		}
	}
	check.Matches = make([]IssueSummary, 0, len(byNumber))
	for _, issue := range byNumber {
		check.Matches = append(check.Matches, issue)
	}
	sort.Slice(check.Matches, func(i, j int) bool { return check.Matches[i].Number < check.Matches[j].Number })
	return check, nil
}

// exactTitleNow is the clock for the recent-open window; tests replace it.
var exactTitleNow = time.Now

const (
	// ExactTitleRecentWindow is how far back, by creation time, the titles-only
	// scan of open issues reaches.
	ExactTitleRecentWindow      = 7 * 24 * time.Hour
	exactTitleRecentWindowLabel = "7d"
	// ExactTitleRecentScanLimit is a backstop: the most open issues, newest
	// first, whose titles the scan reads even inside ExactTitleRecentWindow.
	ExactTitleRecentScanLimit = 500
	// exactTitleSearchQueryLimit is GitHub's documented search query limit.
	exactTitleSearchQueryLimit = 256
	// exactTitleSearchResultCap is the most results the Search API will page through.
	exactTitleSearchResultCap = 1000
	exactTitlePageSize        = 100
)

type exactTitleSearchPage struct {
	TotalCount        int                     `json:"total_count"`
	IncompleteResults bool                    `json:"incomplete_results"`
	Items             []exactTitleSearchEntry `json:"items"`
}

type exactTitleSearchEntry struct {
	IssueSummary
	PullRequest json.RawMessage `json:"pull_request"`
	HTMLURL     string          `json:"html_url"`
}

// exactTitleSearchQuery returns the Search API query for title, or false when
// the title cannot be searched as a single trustworthy quoted phrase.
func exactTitleSearchQuery(repo, title string) (string, bool) {
	hasWord := false
	for _, r := range title {
		switch {
		case r == '"' || r == '\\':
			return "", false
		case !unicode.IsGraphic(r):
			return "", false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			hasWord = true
		}
	}
	// A title made only of punctuation or symbols has no searchable terms.
	if !hasWord {
		return "", false
	}
	query := fmt.Sprintf(`repo:%s is:issue in:title "%s"`, repo, title)
	if len(query) > exactTitleSearchQueryLimit {
		return "", false
	}
	return query, true
}

// findIssuesByExactTitleViaSearch returns the exact matches by number and true
// only when every search page was complete; otherwise the caller must fall
// back.
func findIssuesByExactTitleViaSearch(ctx context.Context, client Client, repo, title string) (map[int]IssueSummary, bool) {
	query, ok := exactTitleSearchQuery(repo, title)
	if !ok {
		return nil, false
	}
	byNumber := map[int]IssueSummary{}
	fetched := 0
	for page := 1; ; page++ {
		params := url.Values{"q": {query}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		output, err := restBytes(ctx, client, "GET", "/search/issues?"+params.Encode(), nil)
		if err != nil {
			return nil, false
		}
		var result exactTitleSearchPage
		if err := json.Unmarshal(output, &result); err != nil {
			return nil, false
		}
		if result.IncompleteResults || result.TotalCount > exactTitleSearchResultCap {
			return nil, false
		}
		for _, item := range result.Items {
			if (len(item.PullRequest) == 0 || string(item.PullRequest) == "null") && item.Title == title {
				item.URL = item.HTMLURL
				byNumber[item.Number] = item.IssueSummary
			}
		}
		fetched += len(result.Items)
		if fetched >= result.TotalCount {
			return byNumber, true
		}
		if len(result.Items) == 0 || page*exactTitlePageSize >= exactTitleSearchResultCap {
			// total_count promised more than search will return.
			return nil, false
		}
	}
}

// exactTitleRecentQuery reads only the number, title, state, URL and creation
// time of the newest open issues. The issues connection excludes pull requests.
const exactTitleRecentQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, after: $cursor, orderBy: {field: CREATED_AT, direction: DESC}, states: [OPEN]) {
      nodes { number title state url createdAt }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

type exactTitleRecentNode struct {
	IssueSummary
	CreatedAt time.Time `json:"createdAt"`
}

type exactTitleRecentResponse struct {
	Errors []GraphQLError `json:"errors"`
	Data   struct {
		Repository *struct {
			Issues struct {
				Nodes    []exactTitleRecentNode `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		} `json:"repository"`
	} `json:"data"`
}

// recentOpenIssuesByExactTitle reads the titles of open issues, newest first,
// until one was created before cutoff, there are no more, or maxPages pages
// were read. It returns the exact matches and whether it stopped at maxPages
// while still inside the window.
func recentOpenIssuesByExactTitle(ctx context.Context, client Client, owner, name, title string, cutoff time.Time, maxPages int) ([]IssueSummary, bool, error) {
	repo := owner + "/" + name
	matches := make([]IssueSummary, 0)
	cursor := ""
	for page := 0; page < maxPages; page++ {
		variables := map[string]any{"owner": owner, "name": name}
		if cursor != "" {
			variables["cursor"] = cursor
		}
		output, err := graphQLBytes(ctx, client, exactTitleRecentQuery, variables)
		if err != nil {
			return nil, false, fmt.Errorf("read recent open issue titles in %s for an exact-title match: %w", repo, err)
		}
		var response exactTitleRecentResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return nil, false, fmt.Errorf("decode recent open issue titles in %s: %w", repo, err)
		}
		if len(response.Errors) > 0 {
			return nil, false, fmt.Errorf("read recent open issue titles in %s: %s", repo, response.Errors[0].Message)
		}
		if response.Data.Repository == nil {
			return nil, false, fmt.Errorf("read recent open issue titles in %s: repository not found", repo)
		}
		issues := response.Data.Repository.Issues
		for _, node := range issues.Nodes {
			if node.CreatedAt.Before(cutoff) {
				return matches, false, nil
			}
			if node.Title == title {
				issue := node.IssueSummary
				issue.State = strings.ToLower(issue.State)
				matches = append(matches, issue)
			}
		}
		if !issues.PageInfo.HasNextPage {
			return matches, false, nil
		}
		if issues.PageInfo.EndCursor == "" {
			return nil, false, fmt.Errorf("read recent open issue titles in %s: next page has no cursor", repo)
		}
		cursor = issues.PageInfo.EndCursor
	}
	return matches, true, nil
}

// CreateIssue creates an issue on GitHub and independently verifies it via readback.
func CreateIssue(ctx context.Context, client Client, input CreateIssueInput) (IssueView, error) {
	if input.Repo == "" {
		return IssueView{}, errors.New("issue creation requires a repository")
	}
	if strings.TrimSpace(input.Title) == "" {
		return IssueView{}, errors.New("issue creation requires a non-empty title")
	}
	input.Title = strings.TrimSpace(input.Title)
	if err := ResolveSelfAssignees(ctx, client, &input.Assignees); err != nil {
		return IssueView{}, err
	}

	labels := cleanNames(input.Labels)
	if err := verifyLabelsExist(ctx, client, input.Repo, labels); err != nil {
		return IssueView{}, err
	}
	body := map[string]any{"title": input.Title, "body": input.Body}
	if len(labels) > 0 {
		body["labels"] = labels
	}
	if assignees := cleanNames(input.Assignees); len(assignees) > 0 {
		body["assignees"] = assignees
	}
	if input.Milestone != "" {
		number, err := resolveMilestone(ctx, client, input.Repo, input.Milestone)
		if err != nil {
			return IssueView{}, err
		}
		body["milestone"] = number
	}
	output, err := restBytes(ctx, client, "POST", "/repos/"+input.Repo+"/issues", body)
	if err != nil {
		return IssueView{}, fmt.Errorf("create issue in %s: %w", input.Repo, err)
	}
	var created struct {
		URL string `json:"html_url"`
	}
	if err := json.Unmarshal(output, &created); err != nil {
		return IssueView{}, fmt.Errorf("issue was created but its identity could not be decoded; do not retry creation: %w", err)
	}
	target, err := ResolveGitHubItemTarget(input.Repo, 0, created.URL)
	if err != nil {
		return IssueView{}, fmt.Errorf("issue was created but its identity could not be resolved; do not retry creation: %w", err)
	}
	if target.Kind != "issues" {
		return IssueView{}, fmt.Errorf("issue was created but GitHub returned a non-issue URL %s; do not retry creation", target.URL)
	}

	view, err := ViewIssue(ctx, client, input.Repo, target.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("issue was created at %s; do not retry creation: read back created issue %s#%d: %w", target.URL, input.Repo, target.Number, err)
	}

	if err := verifyCreatedIssue(view, input); err != nil {
		return IssueView{}, fmt.Errorf("issue was created at %s; do not retry creation: %w", target.URL, err)
	}

	return view, nil
}

// EditIssue modifies an issue and independently verifies applied changes and preservation.
func EditIssue(ctx context.Context, client Client, input EditIssueInput) (IssueView, error) {
	if input.Repo == "" {
		return IssueView{}, errors.New("issue edit requires a repository")
	}
	if input.Number <= 0 {
		return IssueView{}, errors.New("issue edit requires a positive issue number")
	}
	// Resolve @me first so an add/remove overlap through the alias is caught.
	if err := ResolveSelfAssignees(ctx, client, &input.AddAssignees, &input.RemoveAssignees); err != nil {
		return IssueView{}, err
	}
	if err := validateIssueEditInput(input); err != nil {
		return IssueView{}, err
	}

	before, err := ViewIssue(ctx, client, input.Repo, input.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("inspect issue before edit: %w", err)
	}

	path := fmt.Sprintf("/repos/%s/issues/%d", input.Repo, input.Number)
	patch := map[string]any{}
	if input.Title != nil && *input.Title != before.Title {
		patch["title"] = *input.Title
	}
	if input.Body != nil && *input.Body != before.Body {
		patch["body"] = *input.Body
	}
	var addLabels, removeLabels, addAssignees, removeAssignees []string
	for _, name := range cleanNames(input.AddLabels) {
		if !containsName(issueLabelNames(before.Labels), name) {
			addLabels = append(addLabels, name)
		}
	}
	for _, name := range cleanNames(input.RemoveLabels) {
		if containsName(issueLabelNames(before.Labels), name) {
			removeLabels = append(removeLabels, name)
		}
	}
	for _, name := range cleanNames(input.AddAssignees) {
		if !containsName(issueAssigneeNames(before.Assignees), name) {
			addAssignees = append(addAssignees, name)
		}
	}
	for _, name := range cleanNames(input.RemoveAssignees) {
		if containsName(issueAssigneeNames(before.Assignees), name) {
			removeAssignees = append(removeAssignees, name)
		}
	}
	// Validate every added label before any mutation: REST otherwise creates it.
	if err := verifyLabelsExist(ctx, client, input.Repo, addLabels); err != nil {
		return IssueView{}, err
	}
	if input.Milestone != nil {
		if *input.Milestone == "" {
			if before.Milestone != nil {
				patch["milestone"] = nil
			}
		} else if before.Milestone == nil || before.Milestone.Title != *input.Milestone {
			number, err := resolveMilestone(ctx, client, input.Repo, *input.Milestone)
			if err != nil {
				return IssueView{}, err
			}
			patch["milestone"] = number
		}
	}
	if input.IssueType != nil {
		requested := strings.TrimSpace(*input.IssueType)
		if requested == "" {
			if before.IssueType != nil {
				patch["type"] = nil
			}
		} else if before.IssueType == nil || !strings.EqualFold(before.IssueType.Name, requested) {
			patch["type"] = requested
		}
	}
	hasEdits := len(patch) > 0 || len(addLabels)+len(removeLabels)+len(addAssignees)+len(removeAssignees) > 0
	if len(patch) > 0 {
		if _, err := client.REST(ctx, "PATCH", path, patch); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}
	if len(addLabels) > 0 {
		if _, err := client.REST(ctx, "POST", path+"/labels", map[string]any{"labels": addLabels}); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}
	for _, name := range removeLabels {
		if _, err := client.REST(ctx, "DELETE", path+"/labels/"+url.PathEscape(name), nil); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}
	if len(addAssignees) > 0 {
		if _, err := client.REST(ctx, "POST", path+"/assignees", map[string]any{"assignees": addAssignees}); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}
	if len(removeAssignees) > 0 {
		if _, err := client.REST(ctx, "DELETE", path+"/assignees", map[string]any{"assignees": removeAssignees}); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}

	stateChanged := false
	if input.State != nil {
		state := strings.ToLower(strings.TrimSpace(*input.State))
		statePatch := map[string]any{"state": state}
		stage := "reopen issue"
		if state == "closed" {
			stage = "close issue"
			reason := normalizeCloseReason(input.CloseReason)
			if reason == "" && strings.EqualFold(before.State, "open") {
				reason = "completed"
			}
			if reason != "" {
				statePatch["state_reason"] = reason
			}
			stateChanged = strings.EqualFold(before.State, "open") || reason != "" && !strings.EqualFold(before.StateReason, closeReasons[reason].state)
			if strings.EqualFold(before.State, "closed") {
				stage = "change close reason of"
			}
		} else {
			stateChanged = strings.EqualFold(before.State, "closed")
		}
		if stateChanged {
			if _, err := client.REST(ctx, "PATCH", path, statePatch); err != nil {
				return IssueView{}, fmt.Errorf("%s %s#%d: %w", stage, input.Repo, input.Number, err)
			}
		}
	}

	if !hasEdits && !stateChanged {
		if err := verifyEditedIssue(before, before, input); err != nil {
			return IssueView{}, err
		}
		before.Changed = false
		return before, nil
	}

	after, err := ViewIssue(ctx, client, input.Repo, input.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("read back edited issue: %w", err)
	}

	if err := verifyEditedIssue(before, after, input); err != nil {
		return IssueView{}, err
	}
	after.AutomationSideEffects = projectStatusAutomation(before.ProjectItems, after.ProjectItems)
	after.Changed = true
	return after, nil
}

// SelfAssignee is gh's alias for the authenticated user.
const SelfAssignee = "@me"

// ResolveSelfAssignees replaces gh's @me alias in each list with the
// authenticated user's login, so plans and readback compare real logins. It
// reads /user at most once, and only when the alias is present. Each list that
// contains the alias is replaced with a new slice.
func ResolveSelfAssignees(ctx context.Context, client Client, lists ...*[]string) error {
	login := ""
	for _, list := range lists {
		if list == nil || !containsName(*list, SelfAssignee) {
			continue
		}
		if login == "" {
			output, err := restBytes(ctx, client, "GET", "/user", nil)
			if err != nil {
				return fmt.Errorf("resolve %s to the authenticated login: %w", SelfAssignee, err)
			}
			var user struct {
				Login string `json:"login"`
			}
			if err := json.Unmarshal(output, &user); err != nil {
				return fmt.Errorf("resolve %s: decode authenticated user: %w", SelfAssignee, err)
			}
			login = strings.TrimSpace(user.Login)
			if login == "" || strings.ContainsAny(login, " \t\n") {
				return fmt.Errorf("resolve %s to the authenticated login: unexpected GitHub login", SelfAssignee)
			}
		}
		resolved := make([]string, 0, len(*list))
		for _, value := range *list {
			if strings.EqualFold(strings.TrimSpace(value), SelfAssignee) {
				value = login
			}
			resolved = append(resolved, value)
		}
		*list = resolved
	}
	return nil
}

func verifyCreatedIssue(view IssueView, input CreateIssueInput) error {
	if view.Number <= 0 || strings.TrimSpace(view.URL) == "" {
		return errors.New("created issue readback is missing its stable number or URL")
	}
	if view.Title != input.Title {
		return fmt.Errorf("created issue title readback disagrees: got %q, want %q", view.Title, input.Title)
	}
	if view.Body != input.Body {
		return errors.New("created issue body readback disagrees with the requested body")
	}
	if !strings.EqualFold(view.State, "OPEN") {
		return fmt.Errorf("created issue state is %s, want OPEN", view.State)
	}
	if err := compareNameSets("created issue labels", issueLabelNames(view.Labels), input.Labels); err != nil {
		return err
	}
	if err := compareNameSets("created issue assignees", issueAssigneeNames(view.Assignees), input.Assignees); err != nil {
		return err
	}
	if input.Milestone == "" {
		if view.Milestone != nil {
			return fmt.Errorf("created issue unexpectedly has milestone %q", view.Milestone.Title)
		}
	} else if view.Milestone == nil || view.Milestone.Title != input.Milestone {
		observed := ""
		if view.Milestone != nil {
			observed = view.Milestone.Title
		}
		return fmt.Errorf("created issue milestone readback disagrees: got %q, want %q", observed, input.Milestone)
	}
	return nil
}

func validateIssueEditInput(input EditIssueInput) error {
	if overlap := overlappingNames(input.AddLabels, input.RemoveLabels); len(overlap) > 0 {
		return fmt.Errorf("labels cannot be both added and removed: %s", strings.Join(overlap, ", "))
	}
	if overlap := overlappingNames(input.AddAssignees, input.RemoveAssignees); len(overlap) > 0 {
		return fmt.Errorf("assignees cannot be both added and removed: %s", strings.Join(overlap, ", "))
	}
	if input.State != nil {
		switch strings.ToLower(strings.TrimSpace(*input.State)) {
		case "open", "closed":
		default:
			return fmt.Errorf("invalid issue state %q; expected open or closed", *input.State)
		}
	}
	if input.CloseReason != "" {
		if _, ok := closeReasons[normalizeCloseReason(input.CloseReason)]; !ok {
			return fmt.Errorf("invalid close reason %q; expected completed or not_planned", input.CloseReason)
		}
		if input.State == nil || !strings.EqualFold(strings.TrimSpace(*input.State), "closed") {
			return errors.New("a close reason requires the closed state")
		}
	}
	return nil
}

func verifyEditedIssue(before, after IssueView, input EditIssueInput) error {
	if after.Number != before.Number || after.URL != before.URL {
		return fmt.Errorf("issue identity changed during edit: got #%d %s, want #%d %s", after.Number, after.URL, before.Number, before.URL)
	}

	wantTitle := before.Title
	if input.Title != nil {
		wantTitle = *input.Title
	}
	if after.Title != wantTitle {
		return fmt.Errorf("title readback disagrees: got %q, want %q", after.Title, wantTitle)
	}
	wantBody := before.Body
	if input.Body != nil {
		wantBody = *input.Body
	}
	if after.Body != wantBody {
		return errors.New("body readback disagrees with the requested or preserved body")
	}

	wantLabels := applyNameDelta(issueLabelNames(before.Labels), input.AddLabels, input.RemoveLabels)
	if err := compareNameSets("labels", issueLabelNames(after.Labels), wantLabels); err != nil {
		return err
	}
	wantAssignees := applyNameDelta(issueAssigneeNames(before.Assignees), input.AddAssignees, input.RemoveAssignees)
	if err := compareNameSets("assignees", issueAssigneeNames(after.Assignees), wantAssignees); err != nil {
		return err
	}

	wantMilestone := ""
	if before.Milestone != nil {
		wantMilestone = before.Milestone.Title
	}
	if input.Milestone != nil {
		wantMilestone = *input.Milestone
	}
	gotMilestone := ""
	if after.Milestone != nil {
		gotMilestone = after.Milestone.Title
	}
	if gotMilestone != wantMilestone {
		return fmt.Errorf("milestone readback disagrees: got %q, want %q", gotMilestone, wantMilestone)
	}

	wantType := ""
	if before.IssueType != nil {
		wantType = before.IssueType.Name
	}
	if input.IssueType != nil {
		wantType = strings.TrimSpace(*input.IssueType)
	}
	gotType := ""
	if after.IssueType != nil {
		gotType = after.IssueType.Name
	}
	if !strings.EqualFold(gotType, wantType) {
		return fmt.Errorf("issue type readback disagrees: got %q, want %q", gotType, wantType)
	}

	wantState := before.State
	if input.State != nil {
		wantState = *input.State
	}
	if !strings.EqualFold(after.State, wantState) {
		return fmt.Errorf("state readback disagrees: got %q, want %q", after.State, wantState)
	}
	if input.State == nil && after.StateReason != before.StateReason {
		return fmt.Errorf("unrequested state reason changed: got %q, want preserved %q", after.StateReason, before.StateReason)
	}
	if input.State != nil && strings.EqualFold(strings.TrimSpace(*input.State), "closed") {
		wantReason := before.StateReason
		if input.CloseReason != "" {
			wantReason = closeReasons[normalizeCloseReason(input.CloseReason)].state
		} else if strings.EqualFold(before.State, "open") {
			wantReason = "COMPLETED"
		}
		if !strings.EqualFold(after.StateReason, wantReason) {
			return fmt.Errorf("close reason readback disagrees: got %q, want %q", after.StateReason, wantReason)
		}
	}

	// Built-in Project workflows (Item closed, Item reopened) may set Status
	// when the issue state changes; any other Project change is collateral.
	ignoreStatus := !strings.EqualFold(before.State, after.State)
	if !reflect.DeepEqual(canonicalProjectItems(before.ProjectItems, ignoreStatus), canonicalProjectItems(after.ProjectItems, ignoreStatus)) {
		return errors.New("unrequested Project membership or Project item summary changed during issue edit")
	}
	return nil
}

// canonicalProjectItems canonicalises gh's projectItems summaries, optionally
// without their Status so workflow-driven Status changes can be tolerated.
func canonicalProjectItems(values []json.RawMessage, ignoreStatus bool) []string {
	if !ignoreStatus {
		return canonicalRawSet(values)
	}
	stripped := make([]json.RawMessage, 0, len(values))
	for _, raw := range values {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			stripped = append(stripped, raw)
			continue
		}
		delete(decoded, "status")
		encoded, _ := json.Marshal(decoded)
		stripped = append(stripped, encoded)
	}
	return canonicalRawSet(stripped)
}

// projectStatusAutomation describes Project Status changes between two
// verified issue reads, keyed by the Project title gh reports.
func projectStatusAutomation(before, after []json.RawMessage) []string {
	type summary struct {
		Title  string `json:"title"`
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
	}
	previous := make(map[string]string)
	for _, raw := range before {
		var item summary
		if json.Unmarshal(raw, &item) == nil {
			previous[item.Title] = item.Status.Name
		}
	}
	var effects []string
	for _, raw := range after {
		var item summary
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if old, ok := previous[item.Title]; ok && old != item.Status.Name {
			effects = append(effects, fmt.Sprintf("Project %q automation changed Status from %q to %q", item.Title, old, item.Status.Name))
		}
	}
	sort.Strings(effects)
	return effects
}

func issueLabelNames(labels []IssueLabel) []string {
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		result = append(result, label.Name)
	}
	return result
}

func issueAssigneeNames(assignees []IssueAssignee) []string {
	result := make([]string, 0, len(assignees))
	for _, assignee := range assignees {
		result = append(result, assignee.Login)
	}
	return result
}

func cleanNames(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if trimmed == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	return result
}

func containsName(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func overlappingNames(left, right []string) []string {
	var overlap []string
	for _, value := range cleanNames(left) {
		if containsName(right, value) {
			overlap = append(overlap, value)
		}
	}
	sort.Strings(overlap)
	return overlap
}

func applyNameDelta(before, additions, removals []string) []string {
	result := cleanNames(before)
	for _, removal := range cleanNames(removals) {
		filtered := result[:0]
		for _, value := range result {
			if !strings.EqualFold(value, removal) {
				filtered = append(filtered, value)
			}
		}
		result = filtered
	}
	for _, addition := range cleanNames(additions) {
		if !containsName(result, addition) {
			result = append(result, addition)
		}
	}
	return result
}

func compareNameSets(subject string, got, want []string) error {
	canonical := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, value := range cleanNames(values) {
			result = append(result, strings.ToLower(value))
		}
		sort.Strings(result)
		return result
	}
	gotCanonical := canonical(got)
	wantCanonical := canonical(want)
	if !reflect.DeepEqual(gotCanonical, wantCanonical) {
		return fmt.Errorf("%s readback disagrees: got %v, want %v", subject, got, want)
	}
	return nil
}

func canonicalRawSet(values []json.RawMessage) []string {
	result := make([]string, 0, len(values))
	for _, raw := range values {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			result = append(result, string(raw))
			continue
		}
		encoded, err := json.Marshal(decoded)
		if err != nil {
			result = append(result, string(raw))
			continue
		}
		result = append(result, string(encoded))
	}
	sort.Strings(result)
	return result
}

// verifyLabelsExist preserves gh's refusal to implicitly create labels.
func verifyLabelsExist(ctx context.Context, client Client, repo string, names []string) error {
	for _, name := range names {
		_, err := client.REST(ctx, "GET", "/repos/"+repo+"/labels/"+url.PathEscape(name), nil)
		if err != nil {
			var failure *HTTPError
			if errors.As(err, &failure) && failure.Status == 404 {
				return fmt.Errorf("label %s does not exist in %s", name, repo)
			}
			return fmt.Errorf("verify label %s in %s: %w", name, repo, err)
		}
	}
	return nil
}
func resolveMilestone(ctx context.Context, client Client, repo, title string) (int, error) {
	pages, err := RESTPages(ctx, client, "/repos/"+repo+"/milestones?state=all&per_page=100")
	if err != nil {
		return 0, fmt.Errorf("resolve milestone %q: %w", title, err)
	}
	for _, page := range pages {
		var milestones []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(page, &milestones); err != nil {
			return 0, fmt.Errorf("decode milestones: %w", err)
		}
		for _, milestone := range milestones {
			if milestone.Title == title {
				return milestone.Number, nil
			}
		}
	}
	return 0, fmt.Errorf("milestone %q does not exist in %s", title, repo)
}
