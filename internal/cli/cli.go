package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/MiguelRodo/github-projects-skill/internal/buildinfo"
	"github.com/MiguelRodo/github-projects-skill/internal/contract"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
	updatecheck "github.com/MiguelRodo/github-projects-skill/internal/update"
)

// Run executes the CLI and returns a process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, client githubcli.Client) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText())
		return 2
	}
	switch args[0] {
	case "help", "--help", "-h", "-help":
		if args[0] == "help" && len(args) > 1 {
			return runHelpTopic(ctx, args[1:], stdout, stderr)
		}
		fmt.Fprint(stdout, usageText())
		return 0
	case "version", "--version", "-v":
		return runVersion(args[1:], stdout, stderr)
	}
	group := findGroup(args[0])
	if group == nil {
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
	if len(args) < 2 {
		return groupUsageError(stderr, group, fmt.Sprintf("%s requires a subcommand", group.name))
	}
	if isHelpFlag(args[1]) || args[1] == "help" {
		fmt.Fprint(stdout, group.usage())
		return 0
	}
	command := group.find(args[1])
	if command == nil {
		return groupUsageError(stderr, group, fmt.Sprintf("unknown %s subcommand %q", group.name, args[1]))
	}
	rest := args[2:]
	if helpRequested(rest) {
		// Requested help is command output: send it to stdout and succeed.
		return command.run(ctx, rest, stdout, stdout, client)
	}
	return command.run(ctx, rest, stdout, &commandOutput{Writer: stderr, command: command}, client)
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write build information as JSON")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects version [--json]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "version does not take positional arguments")
	}
	info := buildinfo.Current()
	if *jsonOutput {
		if err := writeJSON(stdout, info); err != nil {
			return operationError(stderr, "write version", err)
		}
		return 0
	}
	fmt.Fprintf(stdout, "projects %s (commit %s, built %s)\n", displayVersion(info.Version), info.Commit, info.Date)
	return 0
}

func runContractValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("contract validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	jsonOutput := flags.Bool("json", false, "write the validated contract summary as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects contract validate [--root DIRECTORY] [--json] [--quiet]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "contract validate does not take positional arguments")
	}
	resolvedRoot, err := contractRoot(flags, *root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}
	progress(stderr, *quiet, "[1/1] Validating %s/.projects/project.md", strings.TrimRight(resolvedRoot, "/"))
	configuration, err := contract.Load(resolvedRoot)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}
	if *jsonOutput {
		if err := writeJSON(stdout, configuration); err != nil {
			return operationError(stderr, "write contract result", err)
		}
		return 0
	}
	if configuration.Mode == "single" {
		fmt.Fprintf(stdout, "Valid single Project contract: %s (%s/%d).\n", configuration.Path, configuration.Project.Owner, configuration.Project.Number)
		return 0
	}
	if len(configuration.Routes) == 0 {
		fmt.Fprintf(stdout, "Valid empty Project dispatcher: %s (no routes configured).\n", configuration.Path)
		return 0
	}
	fmt.Fprintf(stdout, "Valid Project dispatcher: %s (%d routes).\n", configuration.Path, len(configuration.Routes))
	return 0
}

func runProjectItemList(ctx context.Context, args []string, stdout, stderr io.Writer, client githubcli.Client) int {
	flags := flag.NewFlagSet("project item-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	projectKey := flags.String("project-key", "", "exact dispatcher Project key")
	routingLabel := flags.String("routing-label", "", "exact dispatcher routing label")
	projectNumber := flags.Int("project-number", 0, "exact declared Project number")
	format := flags.String("format", "table", "output format: table or json")
	jsonOutput := flags.Bool("json", false, "shorthand for --format json")
	quiet := flags.Bool("quiet", false, "hide progress messages")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects project item-list [flags]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "project item-list does not take positional arguments")
	}
	if *projectNumber < 0 {
		return usageError(stderr, "--project-number must be a positive integer")
	}
	if *jsonOutput {
		*format = "json"
	}
	if *format != "table" && *format != "json" {
		return usageError(stderr, "--format must be table or json")
	}

	progress(stderr, *quiet, "[1/3] Validating and resolving the repository contract")
	configuration, err := loadContract(flags, *root)
	if err != nil {
		return operationError(stderr, "validate contract", err)
	}
	project, err := resolveProject(configuration, contract.Selector{
		Key:          *projectKey,
		RoutingLabel: *routingLabel,
		Number:       *projectNumber,
	})
	if err != nil {
		return operationError(stderr, "resolve Project", err)
	}

	progress(stderr, *quiet, "[2/3] Reading every item from Project %s/%d (%s)", project.Owner, project.Number, project.Title)
	snapshot, err := githubcli.ReadAllProjectItems(ctx, client, project)
	if err != nil {
		return operationError(stderr, "read complete Project item set", err)
	}
	progress(stderr, *quiet, "[3/3] Verified a complete read of %d Project items", snapshot.TotalCount)

	if *format == "json" {
		if err := writeJSON(stdout, snapshot); err != nil {
			return operationError(stderr, "write Project items", err)
		}
		return 0
	}
	if err := writeItemTable(stdout, snapshot); err != nil {
		return operationError(stderr, "write Project items", err)
	}
	return 0
}

func runUpdateCheck(ctx context.Context, args []string, stdout, stderr io.Writer, client githubcli.Client) int {
	flags := flag.NewFlagSet("update check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write the update result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects update check [--json] [--quiet]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "update check does not take positional arguments")
	}
	progress(stderr, *quiet, "[1/1] Checking the latest projects release")
	result, err := updatecheck.Check(ctx, client, buildinfo.Current().Version)
	if err != nil {
		return operationError(stderr, "check for updates", err)
	}
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write update result", err)
		}
		return 0
	}
	latest := displayVersion(result.Latest)
	if result.NoPublishedRelease {
		latest = "none"
	}
	fmt.Fprintf(stdout, "Installed: %s\nLatest:    %s\n", displayVersion(result.Installed), latest)
	switch {
	case result.NoPublishedRelease:
		fmt.Fprintln(stdout, "Status:    no projects release has been published yet")
	case result.Development:
		fmt.Fprintln(stdout, "Status:    development build; no package upgrade was attempted")
	case result.UpdateAvailable:
		fmt.Fprintln(stdout, "Status:    update available; ask the local operator to upgrade the package")
	default:
		fmt.Fprintln(stdout, "Status:    up to date")
	}
	return 0
}

type tableItem struct {
	Title      string `json:"title"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	Class      string `json:"class"`
	Repository string `json:"repository"`
	Content    struct {
		Number     int    `json:"number"`
		Repository string `json:"repository"`
		Title      string `json:"title"`
		Type       string `json:"type"`
	} `json:"content"`
}

func writeItemTable(writer io.Writer, snapshot githubcli.ProjectItems) error {
	if snapshot.TotalCount == 0 {
		_, err := fmt.Fprintf(writer, "Project %s/%d has no items.\n", snapshot.Project.Owner, snapshot.Project.Number)
		return err
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "TYPE\tREPOSITORY\tNUMBER\tSTATUS\tPRIORITY\tCLASS\tTITLE"); err != nil {
		return err
	}
	for _, raw := range snapshot.Items {
		var item tableItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("decode item for table output: %w", err)
		}
		title := item.Content.Title
		if title == "" {
			title = item.Title
		}
		repository := item.Content.Repository
		if repository == "" {
			repository = item.Repository
		}
		number := "-"
		if item.Content.Number > 0 {
			number = strconv.Itoa(item.Content.Number)
		}
		kind := item.Content.Type
		if kind == "" {
			kind = "DraftIssue"
		}
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			cleanCell(kind), cleanCell(repository), number, cleanCell(item.Status),
			cleanCell(item.Priority), cleanCell(item.Class), cleanCell(title),
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func cleanCell(value string) string {
	replacer := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	return strings.TrimSpace(replacer.Replace(value))
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func progress(writer io.Writer, quiet bool, format string, args ...any) {
	if quiet {
		return
	}
	fmt.Fprintf(writer, format+"\n", args...)
}

// operationError reports a failed stage. Failures caused by the supplied
// Project selector are usage errors: they exit 2 and list the configured routes.
func operationError(stderr io.Writer, stage string, err error) int {
	var selector *selectorError
	if errors.As(err, &selector) {
		message := err.Error()
		if len(selector.routes) > 0 && !strings.Contains(message, "configured routes:") {
			message += "; configured routes: " + strings.Join(selector.routes, ", ")
		}
		fmt.Fprintf(stderr, "projects: %s: %s\n", stage, message)
		return 2
	}
	fmt.Fprintf(stderr, "projects: %s: %v\n", stage, err)
	return 1
}

// usageError reports a mistake in the supplied arguments and exits 2. Inside a
// subcommand it repeats that subcommand's usage rather than the global list.
func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "projects: %s\n\n", message)
	if output, ok := stderr.(*commandOutput); ok {
		output.command.printUsage(output.Writer)
		return 2
	}
	fmt.Fprint(stderr, usageText())
	return 2
}

func groupUsageError(stderr io.Writer, group *commandGroup, message string) int {
	fmt.Fprintf(stderr, "projects: %s\n\n%s", message, group.usage())
	return 2
}

func displayVersion(value string) string {
	if value == "dev" || value == "unknown" || value == "" {
		return value
	}
	return "v" + strings.TrimPrefix(value, "v")
}
