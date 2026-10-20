package githubcli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

// Setup creates a configured field with a schema-only blank choice. It never
// assigns that choice to items or converts a legacy label binding implicitly.
func subprojectSetupProfile(project contract.Project, state setupState) (string, []standardOption, error) {
	if project.Subproject.Storage != "field" {
		return "", nil, nil
	}
	name := project.Subproject.Field
	if name == "" {
		name = "Sub-project"
	}
	switch strings.ToLower(name) {
	case "status", "priority", "class", "due date", "target date":
		return "", nil, fmt.Errorf("Sub-project field %q conflicts with another standard dimension", name)
	}
	current, exists := state.project.Fields[strings.ToLower(name)]
	if exists && current.Name != name {
		return "", nil, fmt.Errorf("Sub-project field name %q does not exactly match configured %q", current.Name, name)
	}
	options := make([]standardOption, 0, len(current.Options)+1+len(project.Subproject.Vocabulary))
	seen := map[string]string{}
	for _, option := range current.Options {
		folded := strings.ToLower(option.Name)
		if _, exists := seen[folded]; exists {
			return "", nil, fmt.Errorf("duplicate Sub-project option %q", option.Name)
		}
		seen[folded] = option.Name
		options = append(options, standardOption{Name: option.Name, Color: strings.ToUpper(option.Color)})
	}
	values := make([]string, 0, len(project.Subproject.Vocabulary))
	for _, value := range project.Subproject.Vocabulary {
		values = append(values, value)
	}
	sort.Strings(values)
	for _, value := range append([]string{"Not assigned"}, values...) {
		if existing, exists := seen[strings.ToLower(value)]; exists {
			if existing != value {
				return "", nil, fmt.Errorf("Sub-project option %q differs only by case from required %q", existing, value)
			}
			continue
		}
		seen[strings.ToLower(value)] = value
		options = append(options, standardOption{Name: value, Color: "GRAY"})
	}
	return name, options, nil
}

func verifySubprojectSetupPreservation(project contract.Project, before, after setupState) error {
	if project.Subproject.Storage != "field" {
		return nil
	}
	name := project.Subproject.Field
	if name == "" {
		name = "Sub-project"
	}
	old, exists := before.project.Fields[strings.ToLower(name)]
	if !exists {
		return nil
	}
	current, exists := after.project.Fields[strings.ToLower(name)]
	if !exists || current.ID != old.ID || current.Name != name {
		return fmt.Errorf("Sub-project field identity was not preserved")
	}
	last := -1
	for _, original := range old.Options {
		index := -1
		for i, option := range current.Options {
			if option.ID == original.ID {
				if index >= 0 || option != original {
					return fmt.Errorf("Sub-project option %q identity, name, colour or description was not preserved", original.ID)
				}
				index = i
			}
		}
		if index < 0 || index <= last {
			return fmt.Errorf("Sub-project option %q or relative order was not preserved", original.ID)
		}
		last = index
	}
	return nil
}
