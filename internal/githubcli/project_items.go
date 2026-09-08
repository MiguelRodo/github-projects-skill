package githubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

const firstItemLimit = 10000

// ProjectIdentity is provider readback for the exact Project selected by the
// repository contract.
type ProjectIdentity struct {
	Number int    `json:"number"`
	Owner  string `json:"owner"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// ProjectItems is a complete, count-checked Project item snapshot.
type ProjectItems struct {
	Project    ProjectIdentity   `json:"project"`
	TotalCount int               `json:"totalCount"`
	Items      []json.RawMessage `json:"items"`
}

type projectView struct {
	Number int `json:"number"`
	Owner  struct {
		Login string `json:"login"`
	} `json:"owner"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type itemList struct {
	Items      []json.RawMessage `json:"items"`
	TotalCount int               `json:"totalCount"`
}

// ReadAllProjectItems verifies Project identity, asks gh for a deliberately
// generous page, and refuses to call a partial response complete. If the
// Project is larger than the first limit, it retries once with the observed
// count plus room for concurrent additions.
func ReadAllProjectItems(ctx context.Context, runner Runner, expected contract.Project) (ProjectItems, error) {
	identity, err := readProjectIdentity(ctx, runner, expected)
	if err != nil {
		return ProjectItems{}, err
	}

	list, err := readItemList(ctx, runner, expected, firstItemLimit)
	if err != nil {
		return ProjectItems{}, err
	}
	if len(list.Items) != list.TotalCount && list.TotalCount > firstItemLimit {
		// The margin makes a small concurrent addition likely to be included. The
		// count check below still refuses a changing or incomplete snapshot.
		limit := list.TotalCount + 100
		if limit < list.TotalCount { // integer overflow guard
			limit = list.TotalCount
		}
		list, err = readItemList(ctx, runner, expected, limit)
		if err != nil {
			return ProjectItems{}, err
		}
	}
	if len(list.Items) != list.TotalCount {
		return ProjectItems{}, fmt.Errorf(
			"Project item read is incomplete or changed during pagination: received %d of %d items; retry the command",
			len(list.Items), list.TotalCount,
		)
	}

	return ProjectItems{
		Project:    identity,
		TotalCount: list.TotalCount,
		Items:      list.Items,
	}, nil
}

func readProjectIdentity(ctx context.Context, runner Runner, expected contract.Project) (ProjectIdentity, error) {
	output, err := runner.Run(
		ctx,
		"project", "view", strconv.Itoa(expected.Number),
		"--owner", expected.Owner,
		"--format", "json",
	)
	if err != nil {
		return ProjectIdentity{}, fmt.Errorf("read Project identity: %w", err)
	}
	var observed projectView
	if err := json.Unmarshal(output, &observed); err != nil {
		return ProjectIdentity{}, fmt.Errorf("decode Project identity: %w", err)
	}
	if observed.Number != expected.Number || observed.Owner.Login != expected.Owner {
		return ProjectIdentity{}, fmt.Errorf(
			"Project identity disagrees with %s: got %s/%d, want %s/%d",
			expected.ContractPath,
			observed.Owner.Login,
			observed.Number,
			expected.Owner,
			expected.Number,
		)
	}
	if observed.Title != expected.Title {
		return ProjectIdentity{}, fmt.Errorf(
			"Project title disagrees with %s: got %q, want %q",
			expected.ContractPath,
			observed.Title,
			expected.Title,
		)
	}
	return ProjectIdentity{
		Number: observed.Number,
		Owner:  observed.Owner.Login,
		Title:  observed.Title,
		URL:    observed.URL,
	}, nil
}

func readItemList(ctx context.Context, runner Runner, project contract.Project, limit int) (itemList, error) {
	output, err := runner.Run(
		ctx,
		"project", "item-list", strconv.Itoa(project.Number),
		"--owner", project.Owner,
		"--limit", strconv.Itoa(limit),
		"--format", "json",
	)
	if err != nil {
		return itemList{}, fmt.Errorf("read Project items: %w", err)
	}
	var result itemList
	if err := json.Unmarshal(output, &result); err != nil {
		return itemList{}, fmt.Errorf("decode Project items: %w", err)
	}
	if result.TotalCount < 0 {
		return itemList{}, fmt.Errorf("decode Project items: negative totalCount %d", result.TotalCount)
	}
	return result, nil
}
