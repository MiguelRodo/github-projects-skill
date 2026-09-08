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

const usageText = `projects is an optional, deterministic backend for GitHub Project administration.

Usage:
  projects contract validate [flags]
  projects issue create [flags]
  projects issue edit [flags]
  projects project item-list [flags]
  projects project item-add [flags]
  projects project item-edit [flags]
  projects update check [flags]
  projects version [--json]

Commands:
  contract validate   Validate the complete .projects contract without GitHub access.
  issue create        Plan or create an issue on GitHub with verified readback.
  issue edit          Plan or edit an issue on GitHub with verified readback.
  project item-list   Resolve one declared Project and read every item with a count check.
  project item-add    Plan or add an issue to a declared Project.
  project item-edit   Plan or edit field values on a Project item with verified readback.
  update check        Check the latest GitHub release without installing anything.
  version             Show the installed build version.

Run "projects <command> --help" for command flags.
`

// Run executes the CLI and returns a process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usageText)
		return 0
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "contract":
		if len(args) >= 2 && args[1] == "validate" {
			return runContractValidate(args[2:], stdout, stderr)
		}
		return usageError(stderr, "contract requires the validate subcommand")
	case "issue":
		if len(args) >= 2 {
			switch args[1] {
			case "create":
				return runIssueCreate(ctx, args[2:], stdout, stderr, runner)
			case "edit":
				return runIssueEdit(ctx, args[2:], stdout, stderr, runner)
			}
		}
		return usageError(stderr, "issue requires the create or edit subcommand")
	case "project":
		if len(args) >= 2 {
			switch args[1] {
			case "item-list", "items":
				return runProjectItemList(ctx, args[2:], stdout, stderr, runner)
			case "item-add":
				return runProjectItemAdd(ctx, args[2:], stdout, stderr, runner)
			case "item-edit":
				return runProjectItemEdit(ctx, args[2:], stdout, stderr, runner)
			}
		}
		return usageError(stderr, "project requires the item-list, item-add, or item-edit subcommand")
	case "update":
		if len(args) >= 2 && args[1] == "check" {
			return runUpdateCheck(ctx, args[2:], stdout, stderr, runner)
		}
		return usageError(stderr, "update requires the check subcommand")
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
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
	progress(stderr, *quiet, "[1/1] Validating %s/.projects/project.md", strings.TrimRight(*root, "/"))
	configuration, err := contract.Load(*root)
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

func runProjectItemList(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
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
		if choices := configuration.RouteChoices(); len(choices) > 0 {
			err = fmt.Errorf("%w; configured routes: %s", err, strings.Join(choices, ", "))
		}
		return operationError(stderr, "resolve Project", err)
	}

	progress(stderr, *quiet, "[2/3] Reading every item from Project %s/%d (%s)", project.Owner, project.Number, project.Title)
	snapshot, err := githubcli.ReadAllProjectItems(ctx, runner, project)
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

func runUpdateCheck(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
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
	result, err := updatecheck.Check(ctx, runner, buildinfo.Current().Version)
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

func operationError(stderr io.Writer, stage string, err error) int {
	fmt.Fprintf(stderr, "projects: %s: %v\n", stage, err)
	return 1
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "projects: %s\n\n%s", message, usageText)
	return 2
}

func displayVersion(value string) string {
	if value == "dev" || value == "unknown" || value == "" {
		return value
	}
	return "v" + strings.TrimPrefix(value, "v")
}
