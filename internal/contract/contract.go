package contract

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const contractPath = ".projects/project.md"

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	ownerPattern      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	credentialPattern = regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9]{20,}|GH_TOKEN[[:space:]]*=|GITHUB_TOKEN[[:space:]]*=)`)
	defaultPriority   = map[string]string{"P0": "P0", "P1": "P1", "P2": "P2", "P3": "P3"}
	defaultClasses    = []string{"Task", "Bug", "Enhancement", "Data", "Analysis", "Deliverable", "Documentation", "Epic"}
)

type Configuration struct {
	Root       string   `json:"root"`
	Path       string   `json:"path"`
	Mode       string   `json:"mode"`
	Repository string   `json:"repository"`
	Project    *Project `json:"project,omitempty"`
	Routes     []Route  `json:"routes,omitempty"`
}

type FieldLocation struct {
	Location string `json:"location"`
	Field    string `json:"field"`
}

type Project struct {
	Key            string                   `json:"key,omitempty"`
	Owner          string                   `json:"owner"`
	OwnerType      string                   `json:"ownerType,omitempty"`
	Number         int                      `json:"number"`
	Title          string                   `json:"title"`
	Repository     string                   `json:"repository"`
	Routing        string                   `json:"routing"`
	Privacy        string                   `json:"privacy"`
	ContractPath   string                   `json:"contractPath"`
	Priority       map[string]string        `json:"priority,omitempty"`
	Pending        bool                     `json:"priorityPending"`
	FieldLocations map[string]FieldLocation `json:"fieldLocations,omitempty"`
	ClassValues    []string                 `json:"classValues,omitempty"`
	StatusValues   map[string]string        `json:"statusValues,omitempty"`
}

func (p Project) ResolvePriority(common string) (string, error) {
	if p.Pending {
		return "", fmt.Errorf("%s has Priority mapping status: pending", p.ContractPath)
	}
	commonUpper := strings.ToUpper(strings.TrimSpace(common))
	mapping := p.Priority
	if len(mapping) == 0 {
		mapping = defaultPriority
	}
	val, ok := mapping[commonUpper]
	if !ok {
		return "", fmt.Errorf("unknown priority %q; supported values are P0, P1, P2, P3", common)
	}
	return val, nil
}

func (p Project) ValidateClass(class string) (string, error) {
	values := p.ClassValues
	if len(values) == 0 {
		values = defaultClasses
	}
	for _, valid := range values {
		if strings.EqualFold(valid, strings.TrimSpace(class)) {
			return valid, nil
		}
	}
	return "", fmt.Errorf("invalid class %q; supported options in %s: %s", class, p.ContractPath, strings.Join(values, ", "))
}

func (p Project) ResolveStatus(status string) (string, error) {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "", errors.New("status must not be empty")
	}
	if len(p.StatusValues) > 0 {
		commonValues := make([]string, 0, len(p.StatusValues))
		for common, provider := range p.StatusValues {
			commonValues = append(commonValues, common)
			if strings.EqualFold(common, trimmed) || strings.EqualFold(provider, trimmed) {
				return provider, nil
			}
		}
		sort.Strings(commonValues)
		return "", fmt.Errorf("unknown status %q; declared common values in %s: %s", status, p.ContractPath, strings.Join(commonValues, ", "))
	}

	normalized := strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(trimmed))
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "todo", "to do":
		return "Todo", nil
	case "in progress", "inprogress":
		return "In progress", nil
	case "done", "complete", "completed":
		return "Done", nil
	default:
		return trimmed, nil
	}
}

type Route struct {
	Key          string  `json:"key"`
	RoutingLabel string  `json:"routingLabel"`
	Number       int     `json:"number"`
	ContractPath string  `json:"contractPath"`
	Project      Project `json:"project"`
}

type Selector struct {
	Key          string
	RoutingLabel string
	Number       int
}

func Load(root string) (*Configuration, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory: %s", absRoot)
	}
	path := filepath.Join(absRoot, filepath.FromSlash(contractPath))
	doc, err := parseDocument(path)
	if err != nil {
		return nil, err
	}
	mode, err := requiredMetadata(doc, "Mode")
	if err != nil {
		return nil, err
	}
	switch mode {
	case "single":
		project, err := validateProjectDocument(doc, "single")
		if err != nil {
			return nil, err
		}
		return &Configuration{Root: absRoot, Path: path, Mode: mode, Repository: project.Repository, Project: &project}, nil
	case "dispatcher":
		return validateDispatcher(absRoot, doc)
	default:
		return nil, fmt.Errorf("%s: Mode must be single or dispatcher", path)
	}
}

func (c *Configuration) Resolve(selector Selector) (Project, error) {
	if c.Mode == "single" {
		if c.Project == nil {
			return Project{}, errors.New("single contract has no Project")
		}
		if selector.Key != "" {
			return Project{}, errors.New("--project-key is only valid for a dispatcher contract")
		}
		if selector.RoutingLabel != "" {
			return Project{}, errors.New("--routing-label is only valid for a dispatcher contract")
		}
		if selector.Number != 0 && selector.Number != c.Project.Number {
			return Project{}, fmt.Errorf("Project number %d disagrees with the single contract's Project %d", selector.Number, c.Project.Number)
		}
		return *c.Project, nil
	}
	if selector.Key == "" && selector.RoutingLabel == "" && selector.Number == 0 {
		return Project{}, errors.New("dispatcher resolution requires --project-key, --routing-label or --project-number")
	}
	matches := make([]Route, 0, 1)
	for _, route := range c.Routes {
		if selector.Key != "" && route.Key != selector.Key {
			continue
		}
		if selector.RoutingLabel != "" && route.RoutingLabel != selector.RoutingLabel {
			continue
		}
		if selector.Number != 0 && route.Number != selector.Number {
			continue
		}
		matches = append(matches, route)
	}
	switch len(matches) {
	case 0:
		return Project{}, errors.New("the supplied selector does not match a configured Project route")
	case 1:
		return matches[0].Project, nil
	default:
		return Project{}, fmt.Errorf("the supplied selector matches %d Project routes", len(matches))
	}
}

type document struct {
	path     string
	text     string
	metadata map[string][]string
	sections map[string][][]string
}

func parseDocument(path string) (*document, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing Project contract: %s", path)
		}
		return nil, fmt.Errorf("read Project contract %s: %w", path, err)
	}
	defer file.Close()
	doc := &document{path: path, metadata: make(map[string][]string), sections: make(map[string][][]string)}
	var text strings.Builder
	section := ""
	inMetadata := true
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		text.WriteString(line)
		text.WriteByte('\n')
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			inMetadata = false
			continue
		}
		cells, ok := markdownRow(line)
		if !ok || separatorRow(cells) {
			continue
		}
		if inMetadata && len(cells) >= 2 && cells[0] != "Key" {
			doc.metadata[cells[0]] = append(doc.metadata[cells[0]], cells[1])
		}
		if section != "" {
			doc.sections[section] = append(doc.sections[section], cells)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Project contract %s: %w", path, err)
	}
	doc.text = text.String()
	return doc, nil
}

func markdownRow(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil, false
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) < 3 {
		return nil, false
	}
	cells := make([]string, 0, len(parts)-2)
	for _, part := range parts[1 : len(parts)-1] {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells, true
}

func separatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.Trim(cell, ":")
		if len(trimmed) < 3 || strings.Trim(trimmed, "-") != "" {
			return false
		}
	}
	return true
}

func requiredMetadata(doc *document, key string) (string, error) {
	values := doc.metadata[key]
	if len(values) == 0 || values[0] == "" {
		return "", fmt.Errorf("%s is missing table value: %s", doc.path, key)
	}
	if len(values) > 1 {
		return "", fmt.Errorf("%s declares table value %s more than once", doc.path, key)
	}
	return values[0], nil
}

func optionalMetadata(doc *document, key string) (string, error) {
	values := doc.metadata[key]
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 1 {
		return "", fmt.Errorf("%s declares table value %s more than once", doc.path, key)
	}
	return values[0], nil
}

func validateProjectDocument(doc *document, expectedMode string) (Project, error) {
	version, err := requiredMetadata(doc, "Contract version")
	if err != nil {
		return Project{}, err
	}
	if version != "1" {
		return Project{}, fmt.Errorf("%s has unsupported Contract version %q", doc.path, version)
	}
	mode, err := requiredMetadata(doc, "Mode")
	if err != nil {
		return Project{}, err
	}
	if mode != expectedMode {
		return Project{}, fmt.Errorf("%s must use Mode %s", doc.path, expectedMode)
	}
	project := Project{ContractPath: doc.path}
	if expectedMode == "project" {
		project.Key, err = requiredMetadata(doc, "Project key")
		if err != nil {
			return Project{}, err
		}
	}
	project.Repository, err = requiredMetadata(doc, "Issue repository")
	if err != nil {
		return Project{}, err
	}
	if !repositoryPattern.MatchString(project.Repository) {
		return Project{}, fmt.Errorf("%s has an invalid Issue repository", doc.path)
	}
	project.Owner, err = requiredMetadata(doc, "Project owner")
	if err != nil {
		return Project{}, err
	}
	if !ownerPattern.MatchString(project.Owner) {
		return Project{}, fmt.Errorf("%s has an invalid Project owner", doc.path)
	}
	project.OwnerType, err = optionalMetadata(doc, "Owner type")
	if err != nil {
		return Project{}, err
	}
	if project.OwnerType != "" && project.OwnerType != "user" && project.OwnerType != "organization" {
		return Project{}, fmt.Errorf("%s Owner type must be user or organization when supplied", doc.path)
	}
	numberText, err := requiredMetadata(doc, "Project number")
	if err != nil {
		return Project{}, err
	}
	project.Number, err = positiveInteger(numberText)
	if err != nil {
		return Project{}, fmt.Errorf("%s has an invalid Project number", doc.path)
	}
	project.Title, err = requiredMetadata(doc, "Project title")
	if err != nil {
		return Project{}, err
	}
	project.Routing, err = requiredMetadata(doc, "Routing")
	if err != nil {
		return Project{}, err
	}
	project.Privacy, err = requiredMetadata(doc, "Privacy")
	if err != nil {
		return Project{}, err
	}
	if _, ok := doc.sections["Field locations"]; !ok {
		return Project{}, fmt.Errorf("%s is missing Field locations", doc.path)
	}
	if !sectionHasFirstCell(doc.sections["Field locations"], "Priority") {
		return Project{}, fmt.Errorf("%s does not declare the Priority field location", doc.path)
	}
	project.Priority, project.Pending, err = validatePriority(doc)
	if err != nil {
		return Project{}, err
	}
	if err := validateColourTables(doc); err != nil {
		return Project{}, err
	}
	if err := validateStyle(doc, "Issue write-up style", map[string]bool{"": true, "direct": true, "tidy": true, "unrestricted": true}); err != nil {
		return Project{}, err
	}
	if err := validateStyle(doc, "Issue prose style", map[string]bool{"": true, "natural-direct": true}); err != nil {
		return Project{}, err
	}
	project.FieldLocations = make(map[string]FieldLocation)
	for _, row := range doc.sections["Field locations"] {
		if len(row) >= 3 && row[0] != "Common dimension" {
			project.FieldLocations[row[0]] = FieldLocation{Location: row[1], Field: row[2]}
		}
	}
	if rows, ok := doc.sections["Class values"]; ok {
		for _, row := range rows {
			if len(row) >= 1 && row[0] != "Option" && row[0] != "" {
				project.ClassValues = append(project.ClassValues, row[0])
			}
		}
	}
	if rows, ok := doc.sections["Status mapping"]; ok {
		project.StatusValues = make(map[string]string)
		for _, row := range rows {
			if len(row) >= 2 && row[0] != "Common value" && row[0] != "" {
				project.StatusValues[row[0]] = row[1]
			}
		}
	}
	if credentialPattern.MatchString(doc.text) {
		return Project{}, fmt.Errorf("%s appears to contain a credential", doc.path)
	}
	return project, nil
}

func validatePriority(doc *document) (map[string]string, bool, error) {
	rows, ok := doc.sections["Priority mapping"]
	if !ok {
		return nil, false, nil
	}
	pendingLine := "Priority mapping status: pending"
	pendingCount := 0
	for _, line := range strings.Split(doc.text, "\n") {
		if strings.TrimSpace(line) == pendingLine {
			pendingCount++
		}
	}
	mapping := map[string]string{}
	counts := map[string]int{}
	for _, row := range rows {
		if len(row) < 2 || row[0] == "Common value" {
			continue
		}
		switch row[0] {
		case "P0", "P1", "P2", "P3":
			counts[row[0]]++
			mapping[row[0]] = row[1]
		}
	}
	if pendingCount > 0 {
		if pendingCount != 1 {
			return nil, false, fmt.Errorf("%s must declare the pending Priority status exactly once", doc.path)
		}
		for _, common := range []string{"P0", "P1", "P2", "P3"} {
			if counts[common] > 0 {
				return nil, false, fmt.Errorf("%s mixes a pending Priority status with a %s mapping", doc.path, common)
			}
		}
		return nil, true, nil
	}
	providerSeen := map[string]bool{}
	for _, common := range []string{"P0", "P1", "P2", "P3"} {
		if counts[common] != 1 || mapping[common] == "" {
			return nil, false, fmt.Errorf("%s must map %s exactly once to a non-empty value", doc.path, common)
		}
		if providerSeen[mapping[common]] {
			return nil, false, fmt.Errorf("%s Priority mapping is not one-to-one", doc.path)
		}
		providerSeen[mapping[common]] = true
	}
	return mapping, false, nil
}

func validateColourTables(doc *document) error {
	allowed := map[string]bool{"BLUE": true, "GRAY": true, "GREEN": true, "ORANGE": true, "PINK": true, "PURPLE": true, "RED": true, "YELLOW": true}
	sectionNames := make([]string, 0, len(doc.sections))
	for name := range doc.sections {
		sectionNames = append(sectionNames, name)
	}
	sort.Strings(sectionNames)
	for _, name := range sectionNames {
		rows := doc.sections[name]
		inColours := false
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			if row[0] == "Option" && row[1] == "Colour" {
				inColours = true
				continue
			}
			if !inColours {
				continue
			}
			if row[0] == "" {
				return fmt.Errorf("%s has an empty option in a colour table", doc.path)
			}
			if !allowed[row[1]] {
				return fmt.Errorf("%s has unsupported colour %q for option %q", doc.path, row[1], row[0])
			}
		}
	}
	return nil
}

func validateStyle(doc *document, key string, allowed map[string]bool) error {
	value, err := optionalMetadata(doc, key)
	if err != nil {
		return err
	}
	if allowed[value] {
		return nil
	}
	if key == "Issue write-up style" {
		return fmt.Errorf("%s has unsupported Issue write-up style %q; use direct, tidy or unrestricted", doc.path, value)
	}
	return fmt.Errorf("%s has unsupported Issue prose style %q; use natural-direct", doc.path, value)
}

func validateDispatcher(root string, doc *document) (*Configuration, error) {
	version, err := requiredMetadata(doc, "Contract version")
	if err != nil {
		return nil, err
	}
	if version != "1" {
		return nil, fmt.Errorf("%s has unsupported Contract version %q", doc.path, version)
	}
	mode, err := requiredMetadata(doc, "Mode")
	if err != nil {
		return nil, err
	}
	if mode != "dispatcher" {
		return nil, fmt.Errorf("%s must use Mode dispatcher", doc.path)
	}
	repository, err := requiredMetadata(doc, "Issue repository")
	if err != nil {
		return nil, err
	}
	if !repositoryPattern.MatchString(repository) {
		return nil, fmt.Errorf("%s has an invalid Issue repository", doc.path)
	}
	if _, err := requiredMetadata(doc, "Privacy"); err != nil {
		return nil, err
	}
	if credentialPattern.MatchString(doc.text) {
		return nil, fmt.Errorf("%s appears to contain a credential", doc.path)
	}
	rows, ok := doc.sections["Routes"]
	if !ok {
		return nil, fmt.Errorf("%s is missing Routes", doc.path)
	}
	configuration := &Configuration{Root: root, Path: doc.path, Mode: "dispatcher", Repository: repository}
	keys := map[string]bool{}
	labels := map[string]bool{}
	numbers := map[int]bool{}
	for _, row := range rows {
		if len(row) < 4 || row[0] == "Project key" {
			continue
		}
		key, label, numberText, childRelative := row[0], row[1], row[2], row[3]
		if key == "" || label == "" || childRelative == "" {
			return nil, fmt.Errorf("%s has an incomplete route", doc.path)
		}
		number, err := positiveInteger(numberText)
		if err != nil {
			return nil, fmt.Errorf("%s has an invalid route Project number", doc.path)
		}
		cleanRelative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(childRelative)))
		if !strings.HasPrefix(childRelative, ".projects/projects/") || !strings.HasSuffix(childRelative, ".md") || strings.Contains(childRelative, "..") || cleanRelative != childRelative {
			return nil, fmt.Errorf("%s route contract must be under .projects/projects/", doc.path)
		}
		if keys[key] {
			return nil, fmt.Errorf("%s has a duplicate Project key", doc.path)
		}
		if labels[label] {
			return nil, fmt.Errorf("%s has a duplicate routing label", doc.path)
		}
		if numbers[number] {
			return nil, fmt.Errorf("%s has a duplicate Project number", doc.path)
		}
		keys[key], labels[label], numbers[number] = true, true, true
		childPath := filepath.Join(root, filepath.FromSlash(childRelative))
		childDoc, err := parseDocument(childPath)
		if err != nil {
			return nil, err
		}
		project, err := validateProjectDocument(childDoc, "project")
		if err != nil {
			return nil, err
		}
		if project.Key != key {
			return nil, fmt.Errorf("%s route key disagrees with %s", doc.path, childRelative)
		}
		if project.Routing != "label:"+label {
			return nil, fmt.Errorf("%s route label disagrees with %s", doc.path, childRelative)
		}
		if project.Number != number {
			return nil, fmt.Errorf("%s route number disagrees with %s", doc.path, childRelative)
		}
		if project.Repository != repository {
			return nil, fmt.Errorf("%s issue repository disagrees with %s", doc.path, childRelative)
		}
		configuration.Routes = append(configuration.Routes, Route{Key: key, RoutingLabel: label, Number: number, ContractPath: childPath, Project: project})
	}
	return configuration, nil
}

func positiveInteger(value string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, errors.New("not a positive integer")
	}
	return number, nil
}

func sectionHasFirstCell(rows [][]string, wanted string) bool {
	for _, row := range rows {
		if len(row) > 0 && row[0] == wanted {
			return true
		}
	}
	return false
}

func (c *Configuration) RouteChoices() []string {
	choices := make([]string, 0, len(c.Routes))
	for _, route := range c.Routes {
		choices = append(choices, fmt.Sprintf("%s (%s, Project %d)", route.Key, route.RoutingLabel, route.Number))
	}
	sort.Strings(choices)
	return choices
}
