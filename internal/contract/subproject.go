package contract

import (
	"fmt"
	"regexp"
	"strings"
)

// SubprojectBinding records a stable storage choice; provider IDs are always
// discovered from GitHub immediately before a write.
type SubprojectBinding struct {
	Storage    string            `json:"storage"`
	FieldType  string            `json:"fieldType,omitempty"`
	Field      string            `json:"field,omitempty"`
	LabelScope string            `json:"labelScope,omitempty"`
	Vocabulary map[string]string `json:"vocabulary,omitempty"`
}

func validateSubproject(doc *document, projectKey string) (SubprojectBinding, error) {
	b := SubprojectBinding{Vocabulary: map[string]string{}}
	var err error
	for key, target := range map[string]*string{"Sub-project storage": &b.Storage, "Sub-project field": &b.Field, "Sub-project label scope": &b.LabelScope} {
		*target, err = optionalMetadata(doc, key)
		if err != nil {
			return b, err
		}
	}
	seen := map[string]bool{}
	keyPattern := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	for _, row := range doc.tableRows("Sub-project vocabulary", "Key") {
		if len(row) < 2 || !keyPattern.MatchString(row[0]) || row[1] == "" || b.Vocabulary[row[0]] != "" || seen[row[1]] {
			return b, fmt.Errorf("%s: Sub-project vocabulary must have unique keys and provider values", doc.path)
		}
		b.Vocabulary[row[0]], seen[row[1]] = row[1], true
	}
	explicit := b.Storage != ""
	if !explicit {
		if len(b.Vocabulary) > 0 {
			b.Storage = "labels"
		} else {
			b.Storage = "none"
		}
	}
	switch b.Storage {
	case "field":
		if b.Field == "" {
			b.Field = "Sub-project"
		}
		typ, err := optionalMetadata(doc, "Sub-project field type")
		if err != nil {
			return b, err
		}
		b.FieldType = typ
		if typ != "SINGLE_SELECT" {
			return b, fmt.Errorf("%s: field storage requires Sub-project field type SINGLE_SELECT", doc.path)
		}
	case "labels":
		if explicit && b.LabelScope != "shared" && b.LabelScope != "project" {
			return b, fmt.Errorf("%s: label storage requires project or shared scope", doc.path)
		}
		if b.LabelScope == "project" {
			for _, label := range b.Vocabulary {
				if projectKey == "" || !strings.HasPrefix(label, "subproject:"+projectKey+":") {
					return b, fmt.Errorf("%s: labels must be scoped to this Project key", doc.path)
				}
			}
		}
	case "none":
		if len(b.Vocabulary) != 0 {
			return b, fmt.Errorf("%s: Sub-project vocabulary requires storage", doc.path)
		}
	default:
		return b, fmt.Errorf("%s: unsupported Sub-project storage %q", doc.path, b.Storage)
	}
	return b, nil
}
