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
	current := state.project.Fields[strings.ToLower(name)]
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
