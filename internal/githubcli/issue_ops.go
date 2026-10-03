package githubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
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
	// AutomationSideEffects is never read from gh. EditIssue fills it with
	// Project Status changes made by built-in workflows on close or reopen.
	AutomationSideEffects []string `json:"automationSideEffects,omitempty"`
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

// closeReasons maps the documented close reason to the gh issue close spelling
// and the GraphQL/REST stateReason value.
var closeReasons = map[string]struct{ gh, state string }{
	"completed":   {gh: "completed", state: "COMPLETED"},
	"not_planned": {gh: "not planned", state: "NOT_PLANNED"},
}

func normalizeCloseReason(reason string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(reason)), " ", "_")
}

// ViewIssue inspects an issue using gh issue view --json.
func ViewIssue(ctx context.Context, runner Runner, repo string, number int) (IssueView, error) {
	output, err := runner.Run(
		ctx,
		"issue", "view", strconv.Itoa(number),
		"--repo", repo,
		"--json", "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url",
	)
	if err != nil {
		return IssueView{}, fmt.Errorf("view issue %s#%d: %w", repo, number, err)
	}
	var view IssueView
	if err := json.Unmarshal(output, &view); err != nil {
		return IssueView{}, fmt.Errorf("decode issue view: %w", err)
	}
	return view, nil
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
func FindIssuesByExactTitle(ctx context.Context, runner Runner, repo, title string) (ExactTitleCheck, error) {
	normalizedTitle := strings.TrimSpace(title)
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return ExactTitleCheck{}, fmt.Errorf("invalid repository %q: want OWNER/REPO", repo)
	}
	byNumber, searchOK := findIssuesByExactTitleViaSearch(ctx, runner, repo, normalizedTitle)
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
	recent, capped, err := recentOpenIssuesByExactTitle(ctx, runner, owner, name, normalizedTitle, cutoff, maxPages)
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
	PullRequest bool `json:"pull_request"`
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
func findIssuesByExactTitleViaSearch(ctx context.Context, runner Runner, repo, title string) (map[int]IssueSummary, bool) {
	query, ok := exactTitleSearchQuery(repo, title)
	if !ok {
		return nil, false
	}
	byNumber := map[int]IssueSummary{}
	fetched := 0
	for page := 1; ; page++ {
		args := []string{"api", "-X", "GET"}
		args = append(args, apiHeaders()...)
		args = append(args,
			"search/issues",
			"-f", "q="+query,
			"-f", fmt.Sprintf("per_page=%d", exactTitlePageSize),
			"-f", fmt.Sprintf("page=%d", page),
			"--jq", `{total_count, incomplete_results, items: [.items[] | {number, title, state, url: .html_url, pull_request: (.pull_request != null)}]}`,
		)
		output, err := runner.Run(ctx, args...)
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
			if !item.PullRequest && item.Title == title {
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
	Data struct {
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
func recentOpenIssuesByExactTitle(ctx context.Context, runner Runner, owner, name, title string, cutoff time.Time, maxPages int) ([]IssueSummary, bool, error) {
	repo := owner + "/" + name
	matches := make([]IssueSummary, 0)
	cursor := ""
	for page := 0; page < maxPages; page++ {
		args := []string{"api", "graphql", "-f", "query=" + exactTitleRecentQuery, "-f", "owner=" + owner, "-f", "name=" + name}
		if cursor != "" {
			args = append(args, "-f", "cursor="+cursor)
		}
		output, err := runner.Run(ctx, args...)
		if err != nil {
			return nil, false, fmt.Errorf("read recent open issue titles in %s for an exact-title match: %w", repo, err)
		}
		var response exactTitleRecentResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return nil, false, fmt.Errorf("decode recent open issue titles in %s: %w", repo, err)
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
func CreateIssue(ctx context.Context, runner Runner, input CreateIssueInput) (IssueView, error) {
	if input.Repo == "" {
		return IssueView{}, errors.New("issue creation requires a repository")
	}
	if strings.TrimSpace(input.Title) == "" {
		return IssueView{}, errors.New("issue creation requires a non-empty title")
	}
	input.Title = strings.TrimSpace(input.Title)
	if err := ResolveSelfAssignees(ctx, runner, &input.Assignees); err != nil {
		return IssueView{}, err
	}

	// gh refuses a non-interactive create without --body, so always pass it.
	args := []string{"issue", "create", "--repo", input.Repo, "--title", input.Title, "--body", input.Body}
	for _, l := range input.Labels {
		if strings.TrimSpace(l) != "" {
			args = append(args, "--label", strings.TrimSpace(l))
		}
	}
	for _, a := range input.Assignees {
		if strings.TrimSpace(a) != "" {
			args = append(args, "--assignee", strings.TrimSpace(a))
		}
	}
	if input.Milestone != "" {
		args = append(args, "--milestone", input.Milestone)
	}

	output, err := runner.Run(ctx, args...)
	if err != nil {
		return IssueView{}, fmt.Errorf("create issue in %s: %w", input.Repo, err)
	}

	// gh issue create succeeded, so every later failure must tell the caller
	// not to retry and risk a duplicate issue.
	target, err := ResolveGitHubItemTarget(input.Repo, 0, strings.TrimSpace(string(output)))
	if err != nil {
		return IssueView{}, fmt.Errorf("issue was created but its identity could not be resolved from gh output %q; do not retry creation: %w", string(output), err)
	}
	if target.Kind != "issues" {
		return IssueView{}, fmt.Errorf("issue was created but gh returned a non-issue URL %s; do not retry creation", target.URL)
	}

	view, err := ViewIssue(ctx, runner, input.Repo, target.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("issue was created at %s; do not retry creation: read back created issue %s#%d: %w", target.URL, input.Repo, target.Number, err)
	}

	if err := verifyCreatedIssue(view, input); err != nil {
		return IssueView{}, fmt.Errorf("issue was created at %s; do not retry creation: %w", target.URL, err)
	}

	return view, nil
}

// EditIssue modifies an issue and independently verifies applied changes and preservation.
func EditIssue(ctx context.Context, runner Runner, input EditIssueInput) (IssueView, error) {
	if input.Repo == "" {
		return IssueView{}, errors.New("issue edit requires a repository")
	}
	if input.Number <= 0 {
		return IssueView{}, errors.New("issue edit requires a positive issue number")
	}
	// Resolve @me first so an add/remove overlap through the alias is caught.
	if err := ResolveSelfAssignees(ctx, runner, &input.AddAssignees, &input.RemoveAssignees); err != nil {
		return IssueView{}, err
	}
	if err := validateIssueEditInput(input); err != nil {
		return IssueView{}, err
	}

	before, err := ViewIssue(ctx, runner, input.Repo, input.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("inspect issue before edit: %w", err)
	}

	editArgs := []string{"issue", "edit", strconv.Itoa(input.Number), "--repo", input.Repo}
	hasEdits := false

	if input.Title != nil && *input.Title != before.Title {
		editArgs = append(editArgs, "--title", *input.Title)
		hasEdits = true
	}
	if input.Body != nil && *input.Body != before.Body {
		editArgs = append(editArgs, "--body", *input.Body)
		hasEdits = true
	}
	beforeLabels := issueLabelNames(before.Labels)
	for _, l := range cleanNames(input.AddLabels) {
		if !containsName(beforeLabels, l) {
			editArgs = append(editArgs, "--add-label", l)
			hasEdits = true
		}
	}
	for _, l := range cleanNames(input.RemoveLabels) {
		if containsName(beforeLabels, l) {
			editArgs = append(editArgs, "--remove-label", l)
			hasEdits = true
		}
	}
	beforeAssignees := issueAssigneeNames(before.Assignees)
	for _, a := range cleanNames(input.AddAssignees) {
		if !containsName(beforeAssignees, a) {
			editArgs = append(editArgs, "--add-assignee", a)
			hasEdits = true
		}
	}
	for _, a := range cleanNames(input.RemoveAssignees) {
		if containsName(beforeAssignees, a) {
			editArgs = append(editArgs, "--remove-assignee", a)
			hasEdits = true
		}
	}
	if input.Milestone != nil {
		if *input.Milestone == "" {
			if before.Milestone != nil {
				editArgs = append(editArgs, "--remove-milestone")
				hasEdits = true
			}
		} else if before.Milestone == nil || before.Milestone.Title != *input.Milestone {
			editArgs = append(editArgs, "--milestone", *input.Milestone)
			hasEdits = true
		}
	}
	if input.IssueType != nil {
		requestedType := strings.TrimSpace(*input.IssueType)
		if requestedType == "" {
			if before.IssueType != nil {
				editArgs = append(editArgs, "--remove-type")
				hasEdits = true
			}
		} else if before.IssueType == nil || !strings.EqualFold(before.IssueType.Name, requestedType) {
			editArgs = append(editArgs, "--type", requestedType)
			hasEdits = true
		}
	}

	if hasEdits {
		if _, err := runner.Run(ctx, editArgs...); err != nil {
			return IssueView{}, fmt.Errorf("apply issue edits: %w", err)
		}
	}

	stateChanged := false
	if input.State != nil {
		targetState := strings.ToUpper(strings.TrimSpace(*input.State))
		if targetState == "CLOSED" && strings.EqualFold(before.State, "OPEN") {
			closeArgs := []string{"issue", "close", strconv.Itoa(input.Number), "--repo", input.Repo}
			if input.CloseReason != "" {
				closeArgs = append(closeArgs, "--reason", closeReasons[normalizeCloseReason(input.CloseReason)].gh)
			}
			if _, err := runner.Run(ctx, closeArgs...); err != nil {
				return IssueView{}, fmt.Errorf("close issue %s#%d: %w", input.Repo, input.Number, err)
			}
			stateChanged = true
		} else if targetState == "CLOSED" && input.CloseReason != "" &&
			!strings.EqualFold(before.StateReason, closeReasons[normalizeCloseReason(input.CloseReason)].state) {
			// gh issue close is a no-op on a closed issue, so change only the reason.
			reasonArgs := []string{"api", "--method", "PATCH"}
			reasonArgs = append(reasonArgs, apiHeaders()...)
			reasonArgs = append(reasonArgs,
				fmt.Sprintf("repos/%s/issues/%d", input.Repo, input.Number),
				"-f", "state=closed",
				"-f", "state_reason="+normalizeCloseReason(input.CloseReason),
				"--jq", ".state_reason",
			)
			if _, err := runner.Run(ctx, reasonArgs...); err != nil {
				return IssueView{}, fmt.Errorf("change close reason of %s#%d: %w", input.Repo, input.Number, err)
			}
			stateChanged = true
		} else if targetState == "OPEN" && strings.EqualFold(before.State, "CLOSED") {
			reopenArgs := []string{"issue", "reopen", strconv.Itoa(input.Number), "--repo", input.Repo}
			if _, err := runner.Run(ctx, reopenArgs...); err != nil {
				return IssueView{}, fmt.Errorf("reopen issue %s#%d: %w", input.Repo, input.Number, err)
			}
			stateChanged = true
		}
	}
	if !hasEdits && !stateChanged {
		if err := verifyEditedIssue(before, before, input); err != nil {
			return IssueView{}, err
		}
		return before, nil
	}

	after, err := ViewIssue(ctx, runner, input.Repo, input.Number)
	if err != nil {
		return IssueView{}, fmt.Errorf("read back edited issue: %w", err)
	}

	if err := verifyEditedIssue(before, after, input); err != nil {
		return IssueView{}, err
	}
	after.AutomationSideEffects = projectStatusAutomation(before.ProjectItems, after.ProjectItems)
	return after, nil
}

// SelfAssignee is gh's alias for the authenticated user.
const SelfAssignee = "@me"

// ResolveSelfAssignees replaces gh's @me alias in each list with the
// authenticated user's login, so plans and readback compare real logins. It
// calls gh at most once, and only when the alias is present. Each list that
// contains the alias is replaced with a new slice.
func ResolveSelfAssignees(ctx context.Context, runner Runner, lists ...*[]string) error {
	login := ""
	for _, list := range lists {
		if list == nil || !containsName(*list, SelfAssignee) {
			continue
		}
		if login == "" {
			output, err := runner.Run(ctx, "api", "user", "--jq", ".login")
			if err != nil {
				return fmt.Errorf("resolve %s to the authenticated login: %w", SelfAssignee, err)
			}
			login = strings.TrimSpace(string(output))
			if login == "" || strings.ContainsAny(login, " \t\n") {
				return fmt.Errorf("resolve %s to the authenticated login: unexpected gh output %q", SelfAssignee, string(output))
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
