package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/githubcli"
)

type commandFunc func(ctx context.Context, args []string, stdout, stderr io.Writer, runner githubcli.Runner) int

type subcommand struct {
	group   string
	name    string
	aliases []string
	summary string
	run     commandFunc
}

type commandGroup struct {
	name        string
	subcommands []*subcommand
}

// commandGroups is the single source for dispatch, the global usage and each
// group's help. It is assigned in init because the commands themselves print
// usage derived from it.
var commandGroups []*commandGroup

func init() {
	commandGroups = []*commandGroup{
		{name: "contract", subcommands: []*subcommand{
			{name: "validate", summary: "Validate the complete .projects contract without GitHub access.",
				run: func(_ context.Context, args []string, stdout, stderr io.Writer, _ githubcli.Runner) int {
					return runContractValidate(args, stdout, stderr)
				}},
		}},
		{name: "issue", subcommands: []*subcommand{
			{name: "create", summary: "Plan or create an issue on GitHub with verified readback.", run: runIssueCreate},
			{name: "edit", summary: "Plan or edit an issue on GitHub with verified readback.", run: runIssueEdit},
		}},
		{name: "project", subcommands: []*subcommand{
			{name: "item-list", aliases: []string{"items"}, summary: "Resolve one declared Project and read every item with a count check.", run: runProjectItemList},
			{name: "item-add", summary: "Plan or add an issue to a declared Project.", run: runProjectItemAdd},
			{name: "item-edit", summary: "Plan or edit field values on a Project item with verified readback.", run: runProjectItemEdit},
			{name: "setup-fields", summary: "Plan or apply the shared one-time Project field profile.", run: runProjectSetupFields},
			{name: "setup-backlog-view", summary: "Plan or apply the shared Backlog table view.", run: runProjectSetupBacklogView},
		}},
		{name: "update", subcommands: []*subcommand{
			{name: "check", summary: "Check the latest GitHub release without installing anything.", run: runUpdateCheck},
		}},
	}
	for _, group := range commandGroups {
		for _, command := range group.subcommands {
			command.group = group.name
		}
	}
}

func findGroup(name string) *commandGroup {
	for _, group := range commandGroups {
		if group.name == name {
			return group
		}
	}
	return nil
}

func (g *commandGroup) find(name string) *subcommand {
	for _, command := range g.subcommands {
		if command.name == name {
			return command
		}
		for _, alias := range command.aliases {
			if alias == name {
				return command
			}
		}
	}
	return nil
}

func (c *subcommand) path() string {
	return c.group + " " + c.name
}

func (c *subcommand) label() string {
	label := c.path()
	if len(c.aliases) > 0 {
		label += " (" + strings.Join(c.aliases, ", ") + ")"
	}
	return label
}

// printUsage writes the subcommand's own flag usage. Every command parses its
// flags before doing any work, so --help returns without side effects.
func (c *subcommand) printUsage(writer io.Writer) {
	c.run(context.Background(), []string{"--help"}, io.Discard, writer, nil)
}

func (g *commandGroup) usage() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Usage: projects %s <subcommand> [flags]\n\nSubcommands:\n", g.name)
	writeCommandRows(&builder, g.subcommands)
	fmt.Fprintf(&builder, "\nRun \"projects %s <subcommand> --help\" for subcommand flags.\n", g.name)
	return builder.String()
}

func usageText() string {
	var builder strings.Builder
	builder.WriteString("projects is an optional, deterministic backend for GitHub Project administration.\n\nUsage:\n")
	all := make([]*subcommand, 0)
	for _, group := range commandGroups {
		for _, command := range group.subcommands {
			fmt.Fprintf(&builder, "  projects %s [flags]\n", command.path())
			all = append(all, command)
		}
	}
	builder.WriteString("  projects version [--json]    (also --version, -v)\n\nCommands:\n")
	writeCommandRows(&builder, all)
	fmt.Fprintf(&builder, "  %-34s%s\n", "version", "Show the installed build version.")
	builder.WriteString(`
Commands that read the contract use --root when given; otherwise they use the
nearest directory, from the current one upwards within the enclosing Git
repository, that contains .projects/project.md.

Run "projects <command> --help" or "projects help <group>" for details.
`)
	return builder.String()
}

func writeCommandRows(builder *strings.Builder, commands []*subcommand) {
	for _, command := range commands {
		fmt.Fprintf(builder, "  %-34s%s\n", command.label(), command.summary)
	}
}

// commandOutput is the stderr writer handed to a subcommand. It lets the shared
// usageError helper print that subcommand's usage without changing every call.
type commandOutput struct {
	io.Writer
	command *subcommand
}

func isHelpFlag(arg string) bool {
	switch arg {
	case "-h", "-help", "--help", "--h":
		return true
	}
	return false
}

// helpRequested reports whether flag parsing would stop at a help flag. Values
// after "--" are positional and are not treated as help.
func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if isHelpFlag(arg) {
			return true
		}
	}
	return false
}

func runHelpTopic(ctx context.Context, topic []string, stdout, stderr io.Writer) int {
	switch topic[0] {
	case "version":
		return runVersion([]string{"--help"}, stdout, stdout)
	case "help":
		fmt.Fprint(stdout, usageText())
		return 0
	}
	group := findGroup(topic[0])
	if group == nil {
		return usageError(stderr, fmt.Sprintf("unknown help topic %q", topic[0]))
	}
	if len(topic) == 1 {
		fmt.Fprint(stdout, group.usage())
		return 0
	}
	command := group.find(topic[1])
	if command == nil {
		return groupUsageError(stderr, group, fmt.Sprintf("unknown %s subcommand %q", group.name, topic[1]))
	}
	return command.run(ctx, []string{"--help"}, stdout, stdout, nil)
}
