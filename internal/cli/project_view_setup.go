package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

func runProjectSetupBacklogView(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int {
	flags := flag.NewFlagSet("project setup-backlog-view", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root containing .projects/project.md")
	projectKey := flags.String("project-key", "", "exact dispatcher Project key")
	routingLabel := flags.String("routing-label", "", "exact dispatcher routing label")
	projectNumber := flags.Int("project-number", 0, "exact declared Project number")
	apply := flags.Bool("apply", false, "apply and verify the standard Backlog view setup (default plans only)")
	jsonOutput := flags.Bool("json", false, "write plan/result as JSON")
	quiet := flags.Bool("quiet", false, "hide progress messages")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: projects project setup-backlog-view [flags]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "project setup-backlog-view does not take positional arguments")
	}
	if *projectNumber < 0 {
		return usageError(stderr, "--project-number must be a positive integer")
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

	progress(stderr, *quiet, "[2/3] Inspecting fields and views for Project %s/%d", project.Owner, project.Number)
	if !*apply {
		plan, err := githubcli.PlanStandardBacklogView(ctx, runner, project)
		if err != nil {
			return operationError(stderr, "plan standard Backlog view", err)
		}
		progress(stderr, *quiet, "[3/3] Planned %d Backlog-view changes", len(plan.Changes))
		if *jsonOutput {
			if err := writeJSON(stdout, plan); err != nil {
				return operationError(stderr, "write Backlog-view plan", err)
			}
			return 0
		}
		fmt.Fprintf(stdout, "Standard Backlog view plan for %s/%d (%s):\n", project.Owner, project.Number, project.Title)
		fmt.Fprintf(stdout, "  Visible fields: %s\n", strings.Join(plan.VisibleFields, " | "))
		if len(plan.Changes) == 0 {
			fmt.Fprintln(stdout, "  No changes required.")
		} else {
			for _, change := range plan.Changes {
				fmt.Fprintf(stdout, "  - %s", change.Action)
				if change.Detail != "" {
					fmt.Fprintf(stdout, ": %s", change.Detail)
				}
				fmt.Fprintln(stdout)
			}
		}
		fmt.Fprintln(stdout, "\nPlan only. Supply --apply to perform and independently verify the setup.")
		return 0
	}

	progress(stderr, *quiet, "[3/3] Applying and independently verifying the standard Backlog view")
	result, err := githubcli.ApplyStandardBacklogView(ctx, runner, project)
	if err != nil {
		return operationError(stderr, "apply standard Backlog view", err)
	}
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return operationError(stderr, "write Backlog-view result", err)
		}
		return 0
	}
	fmt.Fprintf(stdout, "Verified standard Backlog view for %s/%d (%s).\n", project.Owner, project.Number, project.Title)
	fmt.Fprintf(stdout, "Visible fields: %s\n", strings.Join(result.VisibleFields, " | "))
	if len(result.Applied) == 0 {
		fmt.Fprintln(stdout, "No changes were required.")
		return 0
	}
	for _, change := range result.Applied {
		fmt.Fprintf(stdout, "  - %s", change.Action)
		if change.Detail != "" {
			fmt.Fprintf(stdout, ": %s", change.Detail)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}
