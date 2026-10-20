package contract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubprojectBindings(t *testing.T) {
	for _, tc := range []struct {
		name, metadata, provider string
		valid                    bool
	}{
		{"legacy", "", "subproject:tools", true},
		{"field", "| Sub-project storage | field |\n| Sub-project field type | SINGLE_SELECT |\n", "Tools", true},
		{"missing type", "| Sub-project storage | field |\n", "Tools", false},
		{"scoped", "| Sub-project storage | labels |\n| Sub-project label scope | project |\n", "subproject:work:tools", true},
		{"unscoped", "| Sub-project storage | labels |\n| Sub-project label scope | project |\n", "subproject:tools", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "contract.md")
			text := "| Key | Value |\n| --- | --- |\n" + tc.metadata + "\n## Sub-project vocabulary\n| Key | Provider value |\n| --- | --- |\n| tools | " + tc.provider + " |\n"
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			doc, err := parseDocument(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = validateSubproject(doc, "work")
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
