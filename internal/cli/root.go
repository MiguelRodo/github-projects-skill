package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

const contractFile = ".projects/project.md"

// loadContract loads the contract from the explicit --root, or from the root
// found by discoverContractRoot when --root was not supplied.
func loadContract(flags *flag.FlagSet, root string) (*contract.Configuration, error) {
	resolved, err := contractRoot(flags, root)
	if err != nil {
		return nil, err
	}
	return contract.Load(resolved)
}

// contractRoot returns the explicit --root unchanged. Otherwise it discovers
// the root from the current directory.
func contractRoot(flags *flag.FlagSet, root string) (string, error) {
	explicit := false
	flags.Visit(func(set *flag.Flag) {
		if set.Name == "root" {
			explicit = true
		}
	})
	if explicit {
		return root, nil
	}
	working, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w; pass --root", err)
	}
	return discoverContractRoot(working)
}

// discoverContractRoot walks up from start to the nearest directory containing
// .projects/project.md. The walk stops at the first directory that contains
// .git, so a repository without its own contract never silently borrows the
// contract of an enclosing workspace or parent repository: mutations would
// otherwise reach the wrong Project. Outside any Git repository the walk can
// continue to the filesystem root.
func discoverContractRoot(start string) (string, error) {
	directory, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w; pass --root", err)
	}
	for {
		if exists(filepath.Join(directory, filepath.FromSlash(contractFile))) {
			return directory, nil
		}
		if exists(filepath.Join(directory, ".git")) {
			return "", fmt.Errorf("missing Project contract: no %s in %s or its parents within the repository %s; run from the repository or pass --root", contractFile, start, directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("missing Project contract: no %s in %s or any parent directory; run from the repository or pass --root", contractFile, start)
		}
		directory = parent
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// selectorError marks a Project resolution failure caused by the supplied
// selector flags, or by their absence where a dispatcher needs one. The shared
// operationError helper reports it as a usage error with the configured routes.
type selectorError struct {
	err    error
	routes []string
}

func (e *selectorError) Error() string { return e.err.Error() }

func (e *selectorError) Unwrap() error { return e.err }

// resolveProject resolves the declared Project and classifies selector mistakes.
func resolveProject(configuration *contract.Configuration, selector contract.Selector) (contract.Project, error) {
	project, err := configuration.Resolve(selector)
	if err == nil {
		return project, nil
	}
	if selector != (contract.Selector{}) || configuration.Mode == "dispatcher" {
		return project, &selectorError{err: err, routes: configuration.RouteChoices()}
	}
	return project, err
}
