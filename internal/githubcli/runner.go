package githubcli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner is the small command boundary used by the GitHub adapter. Tests use a
// fake runner; production uses the authenticated gh CLI already required by the
// project-administration workflow.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type inputRunner interface {
	RunInput(ctx context.Context, input []byte, args ...string) ([]byte, error)
}

// ExecRunner invokes gh without a shell.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return runGH(ctx, nil, args...)
}

// RunInput invokes gh with exact bytes on standard input. It is used for API
// endpoints whose JSON request body cannot be represented safely as flags.
func (ExecRunner) RunInput(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return runGH(ctx, input, args...)
}

func runGH(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("GitHub CLI (gh) is not installed; install gh and authenticate it before using GitHub commands")
	}
	command := exec.CommandContext(ctx, "gh", args...)
	if input != nil {
		command.Stdin = strings.NewReader(string(input))
	}
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		message := strings.TrimSpace(string(exitError.Stderr))
		if message != "" {
			return nil, fmt.Errorf("%s (command: gh %s)", message, summariseArgs(args))
		}
	}
	return nil, fmt.Errorf("%w (command: gh %s)", err, summariseArgs(args))
}

// maxArgDisplay bounds how much of one argument an error message repeats.
const maxArgDisplay = 120

// summariseArgs renders gh arguments for an error message. GraphQL documents
// and issue bodies can be dozens of lines long and would otherwise bury gh's
// own diagnosis, so they are replaced by a short description. Other long
// arguments are truncated. The result is for people, not for re-execution.
func summariseArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for index, arg := range args {
		previous := ""
		if index > 0 {
			previous = args[index-1]
		}
		parts = append(parts, summariseArg(previous, arg))
	}
	return strings.Join(parts, " ")
}

func summariseArg(previous, arg string) string {
	switch {
	case previous == "--body":
		return fmt.Sprintf("<body %d bytes>", len(arg))
	case previous == "--body-file" && arg != "-":
		return truncateArg(arg)
	case strings.HasPrefix(arg, "query="):
		lines := strings.Count(strings.TrimSpace(strings.TrimPrefix(arg, "query=")), "\n") + 1
		return fmt.Sprintf("query=<graphql %d lines>", lines)
	case strings.HasPrefix(arg, "body="):
		return fmt.Sprintf("body=<body %d bytes>", len(arg)-len("body="))
	case strings.HasPrefix(arg, "--body="):
		return fmt.Sprintf("--body=<body %d bytes>", len(arg)-len("--body="))
	}
	return truncateArg(arg)
}

func truncateArg(arg string) string {
	arg = strings.ReplaceAll(arg, "\n", " ")
	if len(arg) <= maxArgDisplay {
		return arg
	}
	return fmt.Sprintf("%s...<%d bytes>", strings.ToValidUTF8(arg[:maxArgDisplay], ""), len(arg))
}
