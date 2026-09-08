package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(val string) error {
	for _, part := range strings.Split(val, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
}

func readFileOrStdin(path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read standard input: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", path, err)
	}
	return string(data), nil
}

func resolveContractRepository(configuration *contract.Configuration, supplied string) (string, error) {
	declared := strings.TrimSpace(configuration.Repository)
	if declared == "" {
		return "", errors.New("the contract does not declare an issue repository")
	}
	if supplied != "" && !strings.EqualFold(strings.TrimSpace(supplied), declared) {
		return "", fmt.Errorf("--repo %s disagrees with contract repository %s", supplied, declared)
	}
	return declared, nil
}

func appendNameOnce(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func sortedFieldNames(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func runIssueCreate(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	flags := flag.NewFlagSet("issue create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	repo := flags.String("repo", "", "optional repository assertion (must agree with contract)")
	title := flags.String("title", "", "issue title (required)")
	body := flags.String("body", "", "issue body text")
	bodyFile := flags.String("body-file", "", "path to file containing issue body (or - for stdin)")
	var labels stringList
	flags.Var(&labels, "label", "issue label (may be repeated or comma-separated)")
	var assignees stringList
	flags.Var(&assignees, "assignee", "issue assignee (may be repeated or comma-separated)")
	milestone := flags.String("milestone", "", "milestone name")
	allowDuplicate := flags.Bool("allow-duplicate", false, "allow creation despite an existing issue with the exact same title")

	projectKey := flags.String("project-key", "", "exact dispatcher Project key")
	routingLabel := flags.String("routing-label", "", "exact dispatcher routing label")
	projectNumber := flags.Int("project-number", 0, "exact declared Project number")
	priority := flags.String("priority", "", "common Priority (P0, P1, P2, P3)")
	class := flags.String("class", "", "Class or Issue Type (e.g. Task, Bug)")
	status := flags.String("status", "", "Project Status (e.g. Todo, In progress, Done)")
	targetDate := flags.String("target-date", "", "Target date (YYYY-MM-DD)")

	apply := flags.Bool("apply", false, "execute the creation on GitHub (default plans only)")
	jsonOutput := flags.Bool("json", false, "output result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")

	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects issue create --title TITLE [flags]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "issue create does not take positional arguments")
	}
	if strings.TrimSpace(*title) == "" {
		return usageError(stderr, "--title is required")
	}
	*title = strings.TrimSpace(*title)
	if *body != "" && *bodyFile != "" {
		return usageError(stderr, "--body and --body-file are mutually exclusive")
	}

	bodyContent := *body
	if *bodyFile != "" {
		readContent, err := readFileOrStdin(*bodyFile)
		if err != nil {
			return operationError(stderr, "read body file", err)
		}
		bodyContent = readContent
	}

	configuration, err := contract.Load(*root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}

	targetRepo, err := resolveContractRepository(configuration, *repo)
	if err != nil {
		return usageError(stderr, err.Error())
	}

	hasProject := *projectKey != "" || *routingLabel != "" || *projectNumber > 0 ||
		*priority != "" || *class != "" || *status != "" || *targetDate != ""

	var resolvedProject contract.Project
	resolvedStatus := ""
	createLabels := append([]string(nil), labels...)
	if hasProject {
		p, err := configuration.Resolve(contract.Selector{
			Key:          *projectKey,
			RoutingLabel: *routingLabel,
			Number:       *projectNumber,
		})
		if err != nil {
			return operationError(stderr, "resolve Project", err)
		}
		resolvedProject = p

		if *priority != "" {
			if _, err := resolvedProject.ResolvePriority(*priority); err != nil {
				return operationError(stderr, "resolve priority", err)
			}
		}
		if *class != "" {
			if _, err := resolvedProject.ValidateClass(*class); err != nil {
				return operationError(stderr, "validate class", err)
			}
		}
		if *status != "" {
			resolvedStatus, err = resolvedProject.ResolveStatus(*status)
			if err != nil {
				return operationError(stderr, "resolve status", err)
			}
		}
		if *targetDate != "" {
			if err := githubcli.ValidateISODate(*targetDate); err != nil {
				return usageError(stderr, err.Error())
			}
		}
		if configuration.Mode == "dispatcher" && strings.HasPrefix(resolvedProject.Routing, "label:") {
			createLabels = appendNameOnce(createLabels, strings.TrimPrefix(resolvedProject.Routing, "label:"))
		}
	}

	stageCount := 2
	if *apply && hasProject {
		stageCount = 4
	} else if *apply {
		stageCount = 3
	}
	var exactTitleMatches []githubcli.IssueSummary
	if *allowDuplicate {
		progress(stderr, *quiet, "[1/%d] Skipping duplicate inspection by explicit request", stageCount)
	} else {
		progress(stderr, *quiet, "[1/%d] Inspecting %s for exact-title duplicates", stageCount, targetRepo)
		exactTitleMatches, err = githubcli.FindIssuesByExactTitle(ctx, runner, targetRepo, *title)
		if err != nil {
			return operationError(stderr, "inspect equivalent issues", err)
		}
	}
	if len(exactTitleMatches) > 0 {
		locations := make([]string, 0, len(exactTitleMatches))
		for _, match := range exactTitleMatches {
			locations = append(locations, match.URL)
		}
		return operationError(stderr, "inspect equivalent issues", fmt.Errorf("exact-title issue already exists: %s; edit that issue or pass --allow-duplicate deliberately", strings.Join(locations, ", ")))
	}

	if !*apply {
		progress(stderr, *quiet, "[2/2] Planning issue creation for %s", targetRepo)
		plan := map[string]any{
			"action":         "create_issue",
			"apply":          false,
			"repository":     targetRepo,
			"title":          *title,
			"allowDuplicate": *allowDuplicate,
		}
		if !*allowDuplicate {
			plan["exactTitleMatches"] = exactTitleMatches
		}
		if bodyContent != "" {
			plan["body"] = bodyContent
		}
		if len(createLabels) > 0 {
			plan["labels"] = []string(createLabels)
		}
		if len(assignees) > 0 {
			plan["assignees"] = []string(assignees)
		}
		if *milestone != "" {
			plan["milestone"] = *milestone
		}
		if hasProject {
			projPlan := map[string]any{
				"number": resolvedProject.Number,
				"owner":  resolvedProject.Owner,
				"title":  resolvedProject.Title,
			}
			if *priority != "" {
				prov, _ := resolvedProject.ResolvePriority(*priority)
				projPlan["priority"] = prov
			}
			if *class != "" {
				cls, _ := resolvedProject.ValidateClass(*class)
				projPlan["class"] = cls
			}
			if *status != "" {
				projPlan["status"] = resolvedStatus
			}
			if *targetDate != "" {
				projPlan["targetDate"] = *targetDate
			}
			plan["project"] = projPlan
		}

		if *jsonOutput {
			if err := writeJSON(stdout, plan); err != nil {
				return operationError(stderr, "write plan JSON", err)
			}
			return 0
		}

		fmt.Fprintf(stdout, "Planned issue creation:\n")
		fmt.Fprintf(stdout, "  Repository: %s\n", targetRepo)
		fmt.Fprintf(stdout, "  Title:      %s\n", *title)
		if len(createLabels) > 0 {
			fmt.Fprintf(stdout, "  Labels:     %s\n", strings.Join(createLabels, ", "))
		}
		if len(assignees) > 0 {
			fmt.Fprintf(stdout, "  Assignees:  %s\n", assignees.String())
		}
		if *milestone != "" {
			fmt.Fprintf(stdout, "  Milestone:  %s\n", *milestone)
		}
		if hasProject {
			fmt.Fprintf(stdout, "  Project:    %s/%d (%s)\n", resolvedProject.Owner, resolvedProject.Number, resolvedProject.Title)
			if *priority != "" {
				prov, _ := resolvedProject.ResolvePriority(*priority)
				fmt.Fprintf(stdout, "  Priority:   %s (%s)\n", *priority, prov)
			}
			if *class != "" {
				cls, _ := resolvedProject.ValidateClass(*class)
				fmt.Fprintf(stdout, "  Class:      %s\n", cls)
			}
			if *status != "" {
				fmt.Fprintf(stdout, "  Status:     %s\n", resolvedStatus)
			}
			if *targetDate != "" {
				fmt.Fprintf(stdout, "  TargetDate: %s\n", *targetDate)
			}
		}
		fmt.Fprintln(stdout, "\nPlan only. Supply --apply to create the issue on GitHub.")
		return 0
	}

	var preparedProject *githubcli.PreparedProjectItemMutation
	if hasProject {
		preparedProject, err = githubcli.PrepareProjectItemMutation(ctx, runner, githubcli.MutateProjectItemInput{
			Project:      resolvedProject,
			Repo:         targetRepo,
			Priority:     *priority,
			Class:        *class,
			Status:       *status,
			TargetDate:   *targetDate,
			AddIfMissing: true,
		})
		if err != nil {
			return operationError(stderr, "preflight Project configuration", err)
		}
	}
	progress(stderr, *quiet, "[2/%d] Creating issue in %s", stageCount, targetRepo)
	created, err := githubcli.CreateIssue(ctx, runner, githubcli.CreateIssueInput{
		Repo:      targetRepo,
		Title:     *title,
		Body:      bodyContent,
		Labels:    createLabels,
		Assignees: assignees,
		Milestone: *milestone,
	})
	if err != nil {
		return operationError(stderr, "create issue", err)
	}
	progress(stderr, *quiet, "[3/%d] Verified created issue %s#%d", stageCount, targetRepo, created.Number)

	var projectResult *githubcli.MutateProjectItemResult
	if hasProject {
		progress(stderr, *quiet, "[4/4] Adding issue to Project %s/%d and setting fields", resolvedProject.Owner, resolvedProject.Number)
		mutRes, err := preparedProject.Apply(ctx, runner, created.Number, created.URL)
		if err != nil {
			return operationError(stderr, "add issue to Project and update fields", fmt.Errorf("issue was created at %s; do not retry creation: %w", created.URL, err))
		}
		projectResult = &mutRes
	}

	if *jsonOutput {
		result := map[string]any{
			"action":  "create_issue",
			"applied": true,
			"issue":   created,
		}
		if projectResult != nil {
			result["projectItem"] = projectResult
		}
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write result JSON", err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "Created issue %s#%d: %s\n", targetRepo, created.Number, created.URL)
	fmt.Fprintf(stdout, "Title: %s\nState: %s\n", created.Title, created.State)
	if projectResult != nil {
		fmt.Fprintf(stdout, "Added to Project %s/%d (item %s)\n", resolvedProject.Owner, resolvedProject.Number, projectResult.ItemID)
		for _, name := range sortedFieldNames(projectResult.Fields) {
			fmt.Fprintf(stdout, "  %s: %s\n", name, projectResult.Fields[name])
		}
	}
	return 0
}

func runIssueEdit(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	flags := flag.NewFlagSet("issue edit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	repo := flags.String("repo", "", "optional repository assertion (must agree with contract)")
	issueNumber := flags.Int("issue", 0, "issue number to edit (required)")
	title := flags.String("title", "", "new issue title")
	body := flags.String("body", "", "new issue body text")
	bodyFile := flags.String("body-file", "", "path to file containing new body text (or - for stdin)")
	var addLabels stringList
	flags.Var(&addLabels, "add-label", "label to add (may be repeated or comma-separated)")
	var removeLabels stringList
	flags.Var(&removeLabels, "remove-label", "label to remove (may be repeated or comma-separated)")
	var addAssignees stringList
	flags.Var(&addAssignees, "add-assignee", "assignee to add (may be repeated or comma-separated)")
	var removeAssignees stringList
	flags.Var(&removeAssignees, "remove-assignee", "assignee to remove (may be repeated or comma-separated)")
	milestone := flags.String("milestone", "", "milestone name (empty string removes milestone)")
	state := flags.String("state", "", "target state: open or closed")
	closeReason := flags.String("close-reason", "completed", "reason when closing: completed or not_planned")

	apply := flags.Bool("apply", false, "execute the edit on GitHub (default plans only)")
	jsonOutput := flags.Bool("json", false, "output result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")

	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects issue edit --issue NUMBER [flags]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "issue edit does not take positional arguments")
	}
	if *issueNumber <= 0 {
		return usageError(stderr, "--issue must be a positive integer")
	}
	if *body != "" && *bodyFile != "" {
		return usageError(stderr, "--body and --body-file are mutually exclusive")
	}

	hasTitle := isFlagSet(flags, "title")
	hasBody := isFlagSet(flags, "body") || isFlagSet(flags, "body-file")
	hasMilestone := isFlagSet(flags, "milestone")
	hasState := isFlagSet(flags, "state")
	hasLabels := len(addLabels) > 0 || len(removeLabels) > 0
	hasAssignees := len(addAssignees) > 0 || len(removeAssignees) > 0

	if !hasTitle && !hasBody && !hasMilestone && !hasState && !hasLabels && !hasAssignees {
		return usageError(stderr, "no issue edits specified; supply at least one edit flag")
	}
	if hasState {
		switch strings.ToLower(strings.TrimSpace(*state)) {
		case "open", "closed":
		default:
			return usageError(stderr, "--state must be open or closed")
		}
	}
	if isFlagSet(flags, "close-reason") {
		if !hasState || !strings.EqualFold(strings.TrimSpace(*state), "closed") {
			return usageError(stderr, "--close-reason requires --state closed")
		}
		switch strings.ToLower(strings.TrimSpace(*closeReason)) {
		case "completed", "not_planned":
		default:
			return usageError(stderr, "--close-reason must be completed or not_planned")
		}
	}

	configuration, err := contract.Load(*root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}

	targetRepo, err := resolveContractRepository(configuration, *repo)
	if err != nil {
		return usageError(stderr, err.Error())
	}

	var newBody *string
	if hasBody {
		content := *body
		if *bodyFile != "" {
			readContent, err := readFileOrStdin(*bodyFile)
			if err != nil {
				return operationError(stderr, "read body file", err)
			}
			content = readContent
		}
		newBody = &content
	}

	var newTitle *string
	if hasTitle {
		newTitle = title
	}
	var newMilestone *string
	if hasMilestone {
		newMilestone = milestone
	}
	var newState *string
	if hasState {
		newState = state
	}

	if !*apply {
		progress(stderr, *quiet, "[1/2] Inspecting current issue %s#%d", targetRepo, *issueNumber)
		current, err := githubcli.ViewIssue(ctx, runner, targetRepo, *issueNumber)
		if err != nil {
			return operationError(stderr, "inspect issue", err)
		}
		progress(stderr, *quiet, "[2/2] Planning issue edits for %s#%d", targetRepo, *issueNumber)

		plan := map[string]any{
			"action":      "edit_issue",
			"apply":       false,
			"repository":  targetRepo,
			"issueNumber": *issueNumber,
			"current":     current,
			"delta": map[string]any{
				"title":           newTitle,
				"body":            newBody,
				"addLabels":       []string(addLabels),
				"removeLabels":    []string(removeLabels),
				"addAssignees":    []string(addAssignees),
				"removeAssignees": []string(removeAssignees),
				"milestone":       newMilestone,
				"state":           newState,
			},
		}
		if *jsonOutput {
			if err := writeJSON(stdout, plan); err != nil {
				return operationError(stderr, "write plan JSON", err)
			}
			return 0
		}

		fmt.Fprintf(stdout, "Planned issue edit for %s#%d:\n", targetRepo, *issueNumber)
		fmt.Fprintf(stdout, "  Current Title: %s\n", current.Title)
		if newTitle != nil {
			fmt.Fprintf(stdout, "  New Title:     %s\n", *newTitle)
		}
		if newBody != nil {
			fmt.Fprintf(stdout, "  New Body:      %d bytes (use --json to inspect exact text)\n", len(*newBody))
		}
		if len(addLabels) > 0 {
			fmt.Fprintf(stdout, "  Add Labels:    %s\n", addLabels.String())
		}
		if len(removeLabels) > 0 {
			fmt.Fprintf(stdout, "  Remove Labels: %s\n", removeLabels.String())
		}
		if len(addAssignees) > 0 {
			fmt.Fprintf(stdout, "  Add Assignees: %s\n", addAssignees.String())
		}
		if len(removeAssignees) > 0 {
			fmt.Fprintf(stdout, "  Rem Assignees: %s\n", removeAssignees.String())
		}
		if newState != nil {
			fmt.Fprintf(stdout, "  Target State:  %s\n", *newState)
		}
		fmt.Fprintln(stdout, "\nPlan only. Supply --apply to apply edits on GitHub.")
		return 0
	}

	progress(stderr, *quiet, "[1/3] Inspecting current issue %s#%d", targetRepo, *issueNumber)
	progress(stderr, *quiet, "[2/3] Applying requested edits to %s#%d", targetRepo, *issueNumber)
	edited, err := githubcli.EditIssue(ctx, runner, githubcli.EditIssueInput{
		Repo:            targetRepo,
		Number:          *issueNumber,
		Title:           newTitle,
		Body:            newBody,
		AddLabels:       addLabels,
		RemoveLabels:    removeLabels,
		AddAssignees:    addAssignees,
		RemoveAssignees: removeAssignees,
		Milestone:       newMilestone,
		State:           newState,
		CloseReason:     *closeReason,
	})
	if err != nil {
		return operationError(stderr, "edit issue", err)
	}
	progress(stderr, *quiet, "[3/3] Verified edits on issue %s#%d", targetRepo, *issueNumber)

	if *jsonOutput {
		result := map[string]any{
			"action":  "edit_issue",
			"applied": true,
			"issue":   edited,
		}
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write result JSON", err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "Updated issue %s#%d: %s\n", targetRepo, edited.Number, edited.URL)
	fmt.Fprintf(stdout, "Title: %s\nState: %s\n", edited.Title, edited.State)
	return 0
}

func runProjectItemAdd(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	flags := flag.NewFlagSet("project item-add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	projectKey := flags.String("project-key", "", "exact dispatcher Project key")
	routingLabel := flags.String("routing-label", "", "exact dispatcher routing label")
	projectNumber := flags.Int("project-number", 0, "exact declared Project number")
	issueNumber := flags.Int("issue", 0, "issue number to add")
	repo := flags.String("repo", "", "optional repository assertion (must agree with contract)")
	url := flags.String("url", "", "URL of the issue or pull request to add")

	apply := flags.Bool("apply", false, "execute the addition on GitHub (default plans only)")
	jsonOutput := flags.Bool("json", false, "output result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")

	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects project item-add [--issue NUMBER | --url URL] [flags]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "project item-add does not take positional arguments")
	}
	if *issueNumber < 0 {
		return usageError(stderr, "--issue must be a positive integer")
	}
	if *url != "" && *issueNumber != 0 {
		return usageError(stderr, "--issue and --url are mutually exclusive")
	}
	if *url == "" && *issueNumber == 0 {
		return usageError(stderr, "either --issue or --url is required")
	}

	configuration, err := contract.Load(*root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}
	project, err := configuration.Resolve(contract.Selector{
		Key:          *projectKey,
		RoutingLabel: *routingLabel,
		Number:       *projectNumber,
	})
	if err != nil {
		return operationError(stderr, "resolve Project", err)
	}

	targetRepo, err := resolveContractRepository(configuration, *repo)
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if project.Repository != "" && !strings.EqualFold(project.Repository, targetRepo) {
		return operationError(stderr, "resolve Project", fmt.Errorf("Project repository %s disagrees with contract repository %s", project.Repository, targetRepo))
	}
	target, err := githubcli.ResolveGitHubItemTarget(targetRepo, *issueNumber, *url)
	if err != nil {
		return usageError(stderr, err.Error())
	}

	if !*apply {
		progress(stderr, *quiet, "[1/2] Inspecting exact Project membership for %s", target.URL)
		current, err := githubcli.QueryProjectItem(ctx, runner, project, target)
		if err != nil {
			return operationError(stderr, "inspect Project membership", err)
		}
		progress(stderr, *quiet, "[2/2] Planning item addition to Project %s/%d (%s)", project.Owner, project.Number, project.Title)
		plan := map[string]any{
			"action":        "project_item_add",
			"apply":         false,
			"project":       compactProject(project),
			"url":           target.URL,
			"alreadyMember": current != nil,
			"wouldAdd":      current == nil,
		}
		if current != nil {
			plan["current"] = current
		}
		if *jsonOutput {
			if err := writeJSON(stdout, plan); err != nil {
				return operationError(stderr, "write plan JSON", err)
			}
			return 0
		}
		fmt.Fprintf(stdout, "Planned addition to Project %s/%d (%s):\n", project.Owner, project.Number, project.Title)
		fmt.Fprintf(stdout, "  URL: %s\n", target.URL)
		if current != nil {
			fmt.Fprintf(stdout, "  No change: already a member as item %s\n", current.ItemID)
		} else {
			fmt.Fprintln(stdout, "  Change: add Project membership")
		}
		fmt.Fprintln(stdout, "\nPlan only. Supply --apply to execute any required addition on GitHub.")
		return 0
	}

	progress(stderr, *quiet, "[1/3] Inspecting exact Project membership")
	progress(stderr, *quiet, "[2/3] Applying the membership addition if required")
	item, added, err := githubcli.EnsureProjectItem(ctx, runner, project, target)
	if err != nil {
		return operationError(stderr, "ensure Project membership", err)
	}
	progress(stderr, *quiet, "[3/3] Independently verified Project membership (item %s)", item.ItemID)

	if *jsonOutput {
		result := map[string]any{
			"action":        "project_item_add",
			"applied":       added,
			"alreadyMember": !added,
			"itemId":        item.ItemID,
			"url":           target.URL,
			"project":       compactProject(project),
		}
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write result JSON", err)
		}
		return 0
	}

	if added {
		fmt.Fprintf(stdout, "Added %s to Project %s/%d (item %s)\n", target.URL, project.Owner, project.Number, item.ItemID)
	} else {
		fmt.Fprintf(stdout, "%s was already in Project %s/%d (item %s); no mutation was needed\n", target.URL, project.Owner, project.Number, item.ItemID)
	}
	return 0
}

func runProjectItemEdit(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	flags := flag.NewFlagSet("project item-edit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	projectKey := flags.String("project-key", "", "exact dispatcher Project key")
	routingLabel := flags.String("routing-label", "", "exact dispatcher routing label")
	projectNumber := flags.Int("project-number", 0, "exact declared Project number")
	issueNumber := flags.Int("issue", 0, "issue number whose item to edit")
	repo := flags.String("repo", "", "optional repository assertion (must agree with contract)")
	url := flags.String("url", "", "URL of the item to edit")

	priority := flags.String("priority", "", "common Priority value (P0, P1, P2, P3)")
	class := flags.String("class", "", "Class value")
	status := flags.String("status", "", "Status value")
	targetDate := flags.String("target-date", "", "Target date (YYYY-MM-DD)")
	var clearFields stringList
	flags.Var(&clearFields, "clear", "field name to clear (may be repeated or comma-separated)")

	apply := flags.Bool("apply", false, "execute the edit on GitHub (default plans only)")
	jsonOutput := flags.Bool("json", false, "output result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")

	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects project item-edit [--issue NUMBER | --url URL] [flags]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "project item-edit does not take positional arguments")
	}
	if *issueNumber < 0 {
		return usageError(stderr, "--issue must be a positive integer")
	}
	if *url != "" && *issueNumber != 0 {
		return usageError(stderr, "--issue and --url are mutually exclusive")
	}
	if *url == "" && *issueNumber == 0 {
		return usageError(stderr, "either --issue or --url is required")
	}
	if *priority == "" && *class == "" && *status == "" && *targetDate == "" && len(clearFields) == 0 {
		return usageError(stderr, "at least one field change (--priority, --class, --status, --target-date, or --clear) is required")
	}

	configuration, err := contract.Load(*root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}
	project, err := configuration.Resolve(contract.Selector{
		Key:          *projectKey,
		RoutingLabel: *routingLabel,
		Number:       *projectNumber,
	})
	if err != nil {
		return operationError(stderr, "resolve Project", err)
	}

	targetRepo, err := resolveContractRepository(configuration, *repo)
	if err != nil {
		return usageError(stderr, err.Error())
	}
	if project.Repository != "" && !strings.EqualFold(project.Repository, targetRepo) {
		return operationError(stderr, "resolve Project", fmt.Errorf("Project repository %s disagrees with contract repository %s", project.Repository, targetRepo))
	}
	target, err := githubcli.ResolveGitHubItemTarget(targetRepo, *issueNumber, *url)
	if err != nil {
		return usageError(stderr, err.Error())
	}

	resolvedStatus := ""
	if *priority != "" {
		if _, err := project.ResolvePriority(*priority); err != nil {
			return operationError(stderr, "resolve priority", err)
		}
	}
	if *class != "" {
		if _, err := project.ValidateClass(*class); err != nil {
			return operationError(stderr, "validate class", err)
		}
	}
	if *status != "" {
		resolvedStatus, err = project.ResolveStatus(*status)
		if err != nil {
			return operationError(stderr, "resolve status", err)
		}
	}
	if *targetDate != "" {
		if err := githubcli.ValidateISODate(*targetDate); err != nil {
			return usageError(stderr, err.Error())
		}
	}

	if !*apply {
		progress(stderr, *quiet, "[1/2] Inspecting live schema, exact membership, and current fields")
		current, err := githubcli.InspectProjectItemMutation(ctx, runner, githubcli.MutateProjectItemInput{
			Project:     project,
			Repo:        targetRepo,
			URL:         target.URL,
			Priority:    *priority,
			Class:       *class,
			Status:      *status,
			TargetDate:  *targetDate,
			ClearFields: clearFields,
		})
		if err != nil {
			return operationError(stderr, "inspect Project mutation", err)
		}
		progress(stderr, *quiet, "[2/2] Planning field updates on Project %s/%d (%s)", project.Owner, project.Number, project.Title)
		plan := map[string]any{
			"action":  "project_item_edit",
			"apply":   false,
			"project": compactProject(project),
			"url":     target.URL,
			"current": current,
			"delta":   map[string]any{},
		}
		delta := plan["delta"].(map[string]any)
		if *priority != "" {
			prov, _ := project.ResolvePriority(*priority)
			delta["priority"] = prov
		}
		if *class != "" {
			cls, _ := project.ValidateClass(*class)
			delta["class"] = cls
		}
		if *status != "" {
			delta["status"] = resolvedStatus
		}
		if *targetDate != "" {
			delta["targetDate"] = *targetDate
		}
		if len(clearFields) > 0 {
			delta["clear"] = []string(clearFields)
		}

		if *jsonOutput {
			if err := writeJSON(stdout, plan); err != nil {
				return operationError(stderr, "write plan JSON", err)
			}
			return 0
		}

		fmt.Fprintf(stdout, "Planned Project field updates for %s on Project %s/%d:\n", target.URL, project.Owner, project.Number)
		if *priority != "" {
			prov, _ := project.ResolvePriority(*priority)
			fmt.Fprintf(stdout, "  Priority:   %s (%s)\n", *priority, prov)
		}
		if *class != "" {
			cls, _ := project.ValidateClass(*class)
			fmt.Fprintf(stdout, "  Class:      %s\n", cls)
		}
		if *status != "" {
			fmt.Fprintf(stdout, "  Status:     %s\n", resolvedStatus)
		}
		if *targetDate != "" {
			fmt.Fprintf(stdout, "  TargetDate: %s\n", *targetDate)
		}
		if len(clearFields) > 0 {
			fmt.Fprintf(stdout, "  Clear:      %s\n", clearFields.String())
		}
		fmt.Fprintln(stdout, "\nPlan only. Supply --apply to update Project fields on GitHub.")
		return 0
	}

	progress(stderr, *quiet, "[1/3] Resolving Project schema and item identity")
	progress(stderr, *quiet, "[2/3] Applying field updates to Project item")
	result, err := githubcli.MutateProjectItem(ctx, runner, githubcli.MutateProjectItemInput{
		Project:     project,
		Repo:        targetRepo,
		IssueNumber: 0,
		URL:         target.URL,
		Priority:    *priority,
		Class:       *class,
		Status:      *status,
		TargetDate:  *targetDate,
		ClearFields: clearFields,
	})
	if err != nil {
		return operationError(stderr, "mutate Project item", err)
	}
	progress(stderr, *quiet, "[3/3] Independently verified requested values and other scalar Project fields")

	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write result JSON", err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "Updated Project item %s on Project %s/%d:\n", result.ItemID, project.Owner, project.Number)
	for _, name := range sortedFieldNames(result.Fields) {
		fmt.Fprintf(stdout, "  %s: %s\n", name, result.Fields[name])
	}
	return 0
}

func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func compactProject(project contract.Project) map[string]any {
	return map[string]any{
		"number": project.Number,
		"owner":  project.Owner,
		"title":  project.Title,
	}
}
