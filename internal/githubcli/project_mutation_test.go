package githubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MiguelRodo/github-projects-skill/internal/contract"
)

func projectSchemaJSON() []byte {
	return []byte(`{
  "data": {
    "user": {
      "projectV2": {
        "id": "PVT_123",
        "number": 40,
        "title": "Planning",
        "fields": {
          "nodes": [
            {
              "__typename": "ProjectV2SingleSelectField",
              "id": "FIELD_STATUS",
              "name": "Status",
              "dataType": "SINGLE_SELECT",
              "options": [
                {"id": "OPT_TODO", "name": "Todo"},
                {"id": "OPT_IN_PROGRESS", "name": "In progress"},
                {"id": "OPT_DONE", "name": "Done"}
              ]
            },
            {
              "__typename": "ProjectV2SingleSelectField",
              "id": "FIELD_PRIORITY",
              "name": "Priority",
              "dataType": "SINGLE_SELECT",
              "options": [
                {"id": "OPT_P0", "name": "P0"},
                {"id": "OPT_P1", "name": "P1"},
                {"id": "OPT_P2", "name": "P2"},
                {"id": "OPT_P3", "name": "P3"}
              ]
            },
            {
              "__typename": "ProjectV2SingleSelectField",
              "id": "FIELD_CLASS",
              "name": "Class",
              "dataType": "SINGLE_SELECT",
              "options": [
                {"id": "OPT_TASK", "name": "Task"},
                {"id": "OPT_BUG", "name": "Bug"}
              ]
            },
            {
              "__typename": "ProjectV2Field",
              "id": "FIELD_TARGET_DATE",
              "name": "Target date",
              "dataType": "DATE"
            }
          ]
        }
      }
    }
  }
}`)
}

func projectItemQueryArgs(owner, repo, kind string, number int) []string {
	return []string{
		"api", "graphql",
		"-f", "query=" + ProjectItemQuery(kind),
		"-f", "owner=" + owner,
		"-f", "repo=" + repo,
		"-F", fmt.Sprintf("number=%d", number),
	}
}

func projectItemQueryJSON(status, priority, class string) []byte {
	return []byte(`{
  "data": {
    "repository": {
      "target": {
        "url": "https://github.com/owner/repo/issues/42",
        "projectItems": {
          "nodes": [{
            "id": "PVTI_ITEM_42",
            "isArchived": false,
            "project": {
              "id": "PVT_123",
              "number": 40,
              "title": "Planning",
              "owner": {"login": "octo-user"}
            },
            "fieldValues": {
              "nodes": [
                {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"` + status + `","optionId":"OPT_STATUS","field":{"id":"FIELD_STATUS","name":"Status","dataType":"SINGLE_SELECT"}},
                {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"` + priority + `","optionId":"OPT_PRIORITY","field":{"id":"FIELD_PRIORITY","name":"Priority","dataType":"SINGLE_SELECT"}},
                {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"` + class + `","optionId":"OPT_CLASS","field":{"id":"FIELD_CLASS","name":"Class","dataType":"SINGLE_SELECT"}}
              ],
              "pageInfo": {"hasNextPage": false}
            }
          }],
          "pageInfo": {"hasNextPage": false}
        }
      }
    }
  }
}`)
}

func missingProjectItemQueryJSON() []byte {
	return []byte(`{"data":{"repository":{"target":{"url":"https://github.com/owner/repo/issues/42","projectItems":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
}

func TestQueryProjectSchema(t *testing.T) {
	p := contract.Project{
		Owner:     "octo-user",
		OwnerType: "user",
		Number:    40,
		Title:     "Planning",
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args: []string{
				"api", "graphql",
				"-f", "query=" + ProjectSchemaQuery("user"),
				"-f", "login=octo-user",
				"-F", "number=40",
			},
			output: projectSchemaJSON(),
		},
	}}

	schema, err := QueryProjectSchema(context.Background(), fake, p)
	if err != nil {
		t.Fatalf("QueryProjectSchema error = %v", err)
	}
	if schema.ID != "PVT_123" || schema.Number != 40 {
		t.Fatalf("schema = %+v", schema)
	}
	statusField, ok := schema.FindField("Status")
	if !ok {
		t.Fatal("Status field not found")
	}
	optID, ok := statusField.FindOptionID("In progress")
	if !ok || optID != "OPT_IN_PROGRESS" {
		t.Fatalf("option id = %q, want OPT_IN_PROGRESS", optID)
	}
}

func TestValidateProjectItemMutationConfigurationNeedsNoIssueTarget(t *testing.T) {
	p := contract.Project{
		Owner:     "octo-user",
		OwnerType: "user",
		Number:    40,
		Title:     "Planning",
		Priority:  map[string]string{"P1": "P1"},
		FieldLocations: map[string]contract.FieldLocation{
			"Priority": {Location: "project field", Field: "Priority"},
		},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{{
		args: []string{
			"api", "graphql",
			"-f", "query=" + ProjectSchemaQuery("user"),
			"-f", "login=octo-user",
			"-F", "number=40",
		},
		output: projectSchemaJSON(),
	}}}

	err := ValidateProjectItemMutationConfiguration(context.Background(), fake, MutateProjectItemInput{
		Project:  p,
		Repo:     "owner/repo",
		Priority: "P1",
	})
	if err != nil {
		t.Fatalf("ValidateProjectItemMutationConfiguration error = %v", err)
	}
}

func TestMutateProjectItem(t *testing.T) {
	p := contract.Project{
		Owner:     "octo-user",
		OwnerType: "user",
		Number:    40,
		Title:     "Planning",
		Priority: map[string]string{
			"P0": "P0",
			"P1": "P1",
			"P2": "P2",
			"P3": "P3",
		},
		ClassValues: []string{"Task", "Bug"},
		FieldLocations: map[string]contract.FieldLocation{
			"Priority": {Location: "project field", Field: "Priority"},
			"Class":    {Location: "project field", Field: "Class"},
			"Status":   {Location: "project field", Field: "Status"},
		},
	}

	fake := &fakeRunner{t: t, responses: []fakeResponse{
		// 1. QueryProjectSchema
		{
			args: []string{
				"api", "graphql",
				"-f", "query=" + ProjectSchemaQuery("user"),
				"-f", "login=octo-user",
				"-F", "number=40",
			},
			output: projectSchemaJSON(),
		},
		// 2. One target-centred read before mutation.
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		// 3. EditProjectItemField for Priority.
		{
			args: []string{
				"project", "item-edit",
				"--id", "PVTI_ITEM_42",
				"--project-id", "PVT_123",
				"--field-id", "FIELD_PRIORITY",
				"--single-select-option-id", "OPT_P1",
			},
			output: []byte("{}"),
		},
		// 4. EditProjectItemField for Status.
		{
			args: []string{
				"project", "item-edit",
				"--id", "PVTI_ITEM_42",
				"--project-id", "PVT_123",
				"--field-id", "FIELD_STATUS",
				"--single-select-option-id", "OPT_IN_PROGRESS",
			},
			output: []byte("{}"),
		},
		// 5. One independent readback verifies both edits.
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("In progress", "P1", "Task")},
	}}

	result, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{
		Project:     p,
		Repo:        "owner/repo",
		IssueNumber: 42,
		Priority:    "P1",
		Status:      "In progress",
	})
	if err != nil {
		t.Fatalf("MutateProjectItem error = %v", err)
	}
	if result.ItemID != "PVTI_ITEM_42" {
		t.Fatalf("result.ItemID = %q, want PVTI_ITEM_42", result.ItemID)
	}
	if result.Fields["Priority"] != "P1" || result.Fields["Status"] != "In progress" {
		t.Fatalf("result.Fields = %+v", result.Fields)
	}
	if fake.calls != 5 {
		t.Fatalf("GitHub calls = %d, want 5 for schema + one initial read + two edits + one readback", fake.calls)
	}
}

func TestMutateProjectItemRejectsPendingPriority(t *testing.T) {
	p := contract.Project{
		Owner:        "octo-user",
		OwnerType:    "user",
		Number:       40,
		Pending:      true,
		ContractPath: ".projects/project.md",
	}
	fake := &fakeRunner{t: t}

	_, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{
		Project:     p,
		Repo:        "owner/repo",
		IssueNumber: 42,
		Priority:    "P1",
	})
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("error = %v, want pending priority error", err)
	}
}

func TestResolveGitHubItemTargetRejectsDisagreementAndSupportsPullRequests(t *testing.T) {
	t.Parallel()
	if _, err := ResolveGitHubItemTarget("owner/repo", 0, "https://github.com/other/repo/issues/4"); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("repository disagreement error = %v", err)
	}
	target, err := ResolveGitHubItemTarget("owner/repo", 0, "https://github.com/owner/repo/pull/7")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != "pull" || target.Number != 7 || target.URL != "https://github.com/owner/repo/pull/7" {
		t.Fatalf("target = %+v", target)
	}
}

func TestQueryProjectSchemaRejectsIncompleteFields(t *testing.T) {
	p := contract.Project{Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning"}
	incomplete := strings.Replace(string(projectSchemaJSON()), `"fields": {`, `"fields": {"pageInfo":{"hasNextPage":true},`, 1)
	fake := &fakeRunner{t: t, responses: []fakeResponse{{
		args: []string{
			"api", "graphql",
			"-f", "query=" + ProjectSchemaQuery("user"),
			"-f", "login=octo-user",
			"-F", "number=40",
		},
		output: []byte(incomplete),
	}}}
	_, err := QueryProjectSchema(context.Background(), fake, p)
	if err == nil || !strings.Contains(err.Error(), "incomplete schema") {
		t.Fatalf("error = %v, want incomplete schema", err)
	}
}

func TestEnsureProjectItemRejectsReadbackIDMismatch(t *testing.T) {
	p := contract.Project{Owner: "octo-user", Number: 40, Title: "Planning", ContractPath: ".projects/project.md"}
	target, err := ResolveGitHubItemTarget("owner/repo", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: missingProjectItemQueryJSON()},
		{args: []string{"project", "item-add", "40", "--owner", "octo-user", "--url", "https://github.com/owner/repo/issues/42", "--format", "json"}, output: []byte(`{"id":"PVTI_RETURNED"}`)},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: []byte(strings.Replace(string(projectItemQueryJSON("Todo", "P2", "Task")), "PVTI_ITEM_42", "PVTI_OBSERVED", 1))},
	}}
	_, _, err = EnsureProjectItem(context.Background(), fake, p, target)
	if err == nil || !strings.Contains(err.Error(), "ID readback disagrees") {
		t.Fatalf("error = %v, want ID disagreement", err)
	}
}

func TestMutateProjectItemRejectsFieldReadbackMismatch(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority:       map[string]string{"P0": "P0", "P1": "P1", "P2": "P2", "P3": "P3"},
		FieldLocations: map[string]contract.FieldLocation{"Priority": {Location: "project field", Field: "Priority"}},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "graphql", "-f", "query=" + ProjectSchemaQuery("user"), "-f", "login=octo-user", "-F", "number=40"}, output: projectSchemaJSON()},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		{args: []string{"project", "item-edit", "--id", "PVTI_ITEM_42", "--project-id", "PVT_123", "--field-id", "FIELD_PRIORITY", "--single-select-option-id", "OPT_P1"}, output: []byte(`{}`)},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
	}}
	_, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", IssueNumber: 42, Priority: "P1"})
	if err == nil || !strings.Contains(err.Error(), "readback disagrees") {
		t.Fatalf("error = %v, want readback disagreement", err)
	}
}

func TestMutateProjectItemRejectsUnrelatedScalarProjectFieldChange(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority:       map[string]string{"P0": "P0", "P1": "P1", "P2": "P2", "P3": "P3"},
		FieldLocations: map[string]contract.FieldLocation{"Priority": {Location: "project field", Field: "Priority"}},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "graphql", "-f", "query=" + ProjectSchemaQuery("user"), "-f", "login=octo-user", "-F", "number=40"}, output: projectSchemaJSON()},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		{args: []string{"project", "item-edit", "--id", "PVTI_ITEM_42", "--project-id", "PVT_123", "--field-id", "FIELD_PRIORITY", "--single-select-option-id", "OPT_P1"}, output: []byte(`{}`)},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P1", "Bug")},
	}}
	_, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", IssueNumber: 42, Priority: "P1"})
	if err == nil || !strings.Contains(err.Error(), "unrelated scalar Project item value changed") {
		t.Fatalf("error = %v, want unrelated-field preservation failure", err)
	}
}

func TestQueryProjectItemRejectsIncompleteMembership(t *testing.T) {
	p := contract.Project{Owner: "octo-user", Number: 40, Title: "Planning"}
	target, err := ResolveGitHubItemTarget("owner/repo", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	incomplete := strings.Replace(string(missingProjectItemQueryJSON()), `"hasNextPage":false`, `"hasNextPage":true`, 1)
	fake := &fakeRunner{t: t, responses: []fakeResponse{{
		args: projectItemQueryArgs("owner", "repo", "issues", 42), output: []byte(incomplete),
	}}}
	_, err = QueryProjectItem(context.Background(), fake, p, target)
	if err == nil || !strings.Contains(err.Error(), "incomplete membership read") {
		t.Fatalf("error = %v, want incomplete membership failure", err)
	}
}

func TestProjectItemQuerySupportsPullRequests(t *testing.T) {
	t.Parallel()
	query := ProjectItemQuery("pull")
	if !strings.Contains(query, "target: pullRequest(number: $number)") {
		t.Fatalf("pull-request query = %q", query)
	}
	if ProjectItemQuery("draft") != "" {
		t.Fatal("unsupported target kind returned a query")
	}
}

func TestMutateProjectItemDoesNotAddMembershipImplicitly(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority:       map[string]string{"P0": "P0", "P1": "P1", "P2": "P2", "P3": "P3"},
		FieldLocations: map[string]contract.FieldLocation{"Priority": {Location: "project field", Field: "Priority"}},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "graphql", "-f", "query=" + ProjectSchemaQuery("user"), "-f", "login=octo-user", "-F", "number=40"}, output: projectSchemaJSON()},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: missingProjectItemQueryJSON()},
	}}
	_, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", IssueNumber: 42, Priority: "P1"})
	if err == nil || !strings.Contains(err.Error(), "item-add explicitly") {
		t.Fatalf("error = %v, want explicit membership guidance", err)
	}
}

func TestMutateOrganizationIssuePriorityWithVerifiedPreservation(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority:       map[string]string{"P0": "Urgent", "P1": "High", "P2": "Medium", "P3": "Low"},
		FieldLocations: map[string]contract.FieldLocation{"Priority": {Location: "organization issue field", Field: "Priority"}},
	}
	fieldDefinitions := []byte(`[[{"id":11,"name":"Priority","data_type":"single_select","options":[{"id":101,"name":"High"},{"id":102,"name":"Medium"}]}]]`)
	beforeValues := []byte(`[[{"issue_field_id":11,"issue_field_name":"Priority","data_type":"single_select","value":102,"single_select_option":{"id":102,"name":"Medium"}},{"issue_field_id":12,"issue_field_name":"Effort","data_type":"single_select","value":201,"single_select_option":{"id":201,"name":"Low"}}]]`)
	afterValues := []byte(`[[{"issue_field_id":11,"issue_field_name":"Priority","data_type":"single_select","value":101,"single_select_option":{"id":101,"name":"High"}},{"issue_field_id":12,"issue_field_name":"Effort","data_type":"single_select","value":201,"single_select_option":{"id":201,"name":"Low"}}]]`)
	fieldArgs := []string{"api", "--paginate", "--slurp", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "orgs/owner/issue-fields?per_page=100"}
	valueArgs := []string{"api", "--paginate", "--slurp", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values?per_page=100"}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: fieldArgs, output: fieldDefinitions},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		{args: valueArgs, output: beforeValues},
		{
			args:  []string{"api", "--method", "POST", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values", "--input", "-"},
			input: []byte(`{"issue_field_values":[{"field_id":11,"value":"High"}]}`), output: afterValues,
		},
		{args: valueArgs, output: afterValues},
	}}
	result, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", IssueNumber: 42, Priority: "P1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields["Priority"] != "High" {
		t.Fatalf("result fields = %+v", result.Fields)
	}
	if fake.calls != 5 {
		t.Fatalf("GitHub calls = %d, want one definition read, membership read, value read, write and readback", fake.calls)
	}
}

func TestSetOrganizationIssueFieldRejectsUnrelatedFieldChange(t *testing.T) {
	beforeValues := []byte(`[[{"issue_field_id":11,"issue_field_name":"Priority","data_type":"single_select","value":102,"single_select_option":{"id":102,"name":"Medium"}},{"issue_field_id":12,"issue_field_name":"Effort","data_type":"single_select","value":201,"single_select_option":{"id":201,"name":"Low"}}]]`)
	afterValues := []byte(`[[{"issue_field_id":11,"issue_field_name":"Priority","data_type":"single_select","value":101,"single_select_option":{"id":101,"name":"High"}},{"issue_field_id":12,"issue_field_name":"Effort","data_type":"single_select","value":202,"single_select_option":{"id":202,"name":"High"}}]]`)
	valueArgs := []string{"api", "--paginate", "--slurp", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values?per_page=100"}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: valueArgs, output: beforeValues},
		{
			args:  []string{"api", "--method", "POST", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values", "--input", "-"},
			input: []byte(`{"issue_field_values":[{"field_id":11,"value":"High"}]}`), output: afterValues,
		},
		{args: valueArgs, output: afterValues},
	}}
	target, err := ResolveGitHubItemTarget("owner/repo", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = setOrganizationIssueFields(context.Background(), fake, target, []organizationFieldChange{{
		Field: organizationIssueField{ID: 11, Name: "Priority", DataType: "single_select"}, Desired: "High",
	}})
	if err == nil || !strings.Contains(err.Error(), "unrelated organization issue field changed") {
		t.Fatalf("error = %v, want unrelated-field preservation failure", err)
	}
}

func TestMutateProjectItemSetsAndVerifiesOrganizationIssueTypeAsClass(t *testing.T) {
	p := contract.Project{
		Owner:       "octo-user",
		OwnerType:   "user",
		Number:      40,
		Title:       "Planning",
		ClassValues: []string{"Task"},
		FieldLocations: map[string]contract.FieldLocation{
			"Class": {Location: "organization issue type", Field: "Issue Type"},
		},
	}
	viewFields := "number,title,body,state,stateReason,labels,assignees,milestone,issueType,projectItems,url"
	beforeIssue := []byte(`{"number":42,"title":"Example","body":"Body","state":"OPEN","stateReason":"","labels":[],"assignees":[],"milestone":null,"issueType":null,"projectItems":[],"url":"https://github.com/owner/repo/issues/42"}`)
	afterIssue := []byte(`{"number":42,"title":"Example","body":"Body","state":"OPEN","stateReason":"","labels":[],"assignees":[],"milestone":null,"issueType":{"name":"Task"},"projectItems":[],"url":"https://github.com/owner/repo/issues/42"}`)
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{
			args: []string{
				"api", "--paginate", "--slurp",
				"-H", "Accept: application/vnd.github+json",
				"-H", "X-GitHub-Api-Version: 2026-03-10",
				"repos/owner/repo/issue-types?per_page=100",
			},
			output: []byte(`[[{"id":410,"name":"Task"}]]`),
		},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", viewFields}, output: beforeIssue},
		{args: []string{"issue", "edit", "42", "--repo", "owner/repo", "--type", "Task"}, output: []byte(`{}`)},
		{args: []string{"issue", "view", "42", "--repo", "owner/repo", "--json", viewFields}, output: afterIssue},
	}}

	result, err := MutateProjectItem(context.Background(), fake, MutateProjectItemInput{
		Project:     p,
		Repo:        "owner/repo",
		IssueNumber: 42,
		Class:       "Task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields["Class"] != "Task" {
		t.Fatalf("result fields = %+v, want Class=Task", result.Fields)
	}
}

func TestValidateISODate(t *testing.T) {
	t.Parallel()
	if err := ValidateISODate("2028-02-29"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"2027-02-29", "2026-9-04", "tomorrow"} {
		if err := ValidateISODate(value); err == nil {
			t.Fatalf("ValidateISODate(%q) error = nil", value)
		}
	}
}

func TestProjectFieldNamesCannotCollideWithMetadata(t *testing.T) {
	target, err := ResolveGitHubItemTarget("owner/repo", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ID", "Content", "Project", "isArchived", "meta:id"} {
		t.Run(name, func(t *testing.T) {
			decode := func(fieldName, itemID string) ProjectItemState {
				var node graphQLProjectItemNode
				payload := fmt.Sprintf(`{"id":%q,"fieldValues":{"nodes":[{"__typename":"ProjectV2ItemFieldTextValue","text":"keep","field":{"id":"FIELD_CUSTOM","name":%q,"dataType":"TEXT"}}]}}`, itemID, fieldName)
				if err := json.Unmarshal([]byte(payload), &node); err != nil {
					t.Fatal(err)
				}
				state, err := decodeGraphQLProjectItem(node, target)
				if err != nil {
					t.Fatal(err)
				}
				return state
			}
			before := decode(name, "ITEM")
			if before.Fields[name] != "keep" {
				t.Fatalf("fields = %+v", before.Fields)
			}
			if !projectItemPreserved(before, decode(strings.ToLower(name), "ITEM")) {
				t.Fatal("field-name casing alone changed the preservation result")
			}
			if projectItemPreserved(before, decode(name, "DIFFERENT_ITEM"), name) {
				t.Fatal("excluding a custom field also excluded the item identity")
			}
		})
	}
}

func TestOrganizationIssueFieldsUseOneBatchAndOneReadback(t *testing.T) {
	target, err := ResolveGitHubItemTarget("owner/repo", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	valueArgs := []string{"api", "--paginate", "--slurp", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values?per_page=100"}
	after := []byte(`[[{"issue_field_id":11,"value":"High"},{"issue_field_id":12,"value":"Task"}]]`)
	changes := []organizationFieldChange{
		{Field: organizationIssueField{ID: 11, Name: "Priority"}, Desired: "High"},
		{Field: organizationIssueField{ID: 12, Name: "Class"}, Desired: "Task"},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: valueArgs, output: []byte(`[[{"issue_field_id":11,"value":"Medium"}]]`)},
		{
			args:  []string{"api", "--method", "POST", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/owner/repo/issues/42/issue-field-values", "--input", "-"},
			input: []byte(`{"issue_field_values":[{"field_id":11,"value":"High"},{"field_id":12,"value":"Task"}]}`), output: []byte(`{}`),
		},
		{args: valueArgs, output: after},
	}}
	changed, err := setOrganizationIssueFields(context.Background(), fake, target, changes)
	if err != nil || !changed || fake.calls != 3 {
		t.Fatalf("changed=%v, error=%v, calls=%d; want one read, one write and one readback", changed, err, fake.calls)
	}
	noop := &fakeRunner{t: t, responses: []fakeResponse{{args: valueArgs, output: after}}}
	changed, err = setOrganizationIssueFields(context.Background(), noop, target, changes)
	if err != nil || changed || noop.calls != 1 {
		t.Fatalf("no-op changed=%v, error=%v, calls=%d; want one read only", changed, err, noop.calls)
	}
}

func TestPreparedProjectMutationReusesDefinitionsAndBindsTarget(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority:       map[string]string{"P1": "P1"},
		FieldLocations: map[string]contract.FieldLocation{"Priority": {Location: "project field", Field: "Priority"}},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "graphql", "-f", "query=" + ProjectSchemaQuery("user"), "-f", "login=octo-user", "-F", "number=40"}, output: projectSchemaJSON()},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P2", "Task")},
		{args: []string{"project", "item-edit", "--id", "PVTI_ITEM_42", "--project-id", "PVT_123", "--field-id", "FIELD_PRIORITY", "--single-select-option-id", "OPT_P1"}, output: []byte(`{}`)},
		{args: projectItemQueryArgs("owner", "repo", "issues", 42), output: projectItemQueryJSON("Todo", "P1", "Task")},
	}}
	prepared, err := PrepareProjectItemMutation(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", Priority: "P1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Apply(context.Background(), fake, 42, "https://github.com/other/repo/issues/42"); err == nil {
		t.Fatal("prepared mutation accepted a different repository")
	}
	result, err := prepared.Apply(context.Background(), fake, 42, "https://github.com/owner/repo/issues/42")
	if err != nil || result.Fields["Priority"] != "P1" || fake.calls != 4 {
		t.Fatalf("result=%+v, error=%v, calls=%d; want one schema read for preflight and apply", result, err, fake.calls)
	}
}

func TestPreparedMutationAllowsSameFieldNameAtDifferentProviderLocations(t *testing.T) {
	p := contract.Project{
		Owner: "octo-user", OwnerType: "user", Number: 40, Title: "Planning",
		Priority: map[string]string{"P1": "P1"}, ClassValues: []string{"Task"},
		FieldLocations: map[string]contract.FieldLocation{
			"Priority": {Location: "project field", Field: "Priority"},
			"Class":    {Location: "organization issue field", Field: "Priority"},
		},
	}
	fake := &fakeRunner{t: t, responses: []fakeResponse{
		{args: []string{"api", "graphql", "-f", "query=" + ProjectSchemaQuery("user"), "-f", "login=octo-user", "-F", "number=40"}, output: projectSchemaJSON()},
		{
			args:   []string{"api", "--paginate", "--slurp", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2026-03-10", "orgs/owner/issue-fields?per_page=100"},
			output: []byte(`[[{"id":11,"name":"Priority","data_type":"single_select","options":[{"id":101,"name":"Task"}]}]]`),
		},
	}}
	_, err := PrepareProjectItemMutation(context.Background(), fake, MutateProjectItemInput{Project: p, Repo: "owner/repo", Priority: "P1", Class: "Task"})
	if err != nil {
		t.Fatal(err)
	}
}
