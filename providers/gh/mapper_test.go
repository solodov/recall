package gh

import (
	"reflect"
	"testing"
)

func TestResultsFromItemsMapsGitHubSelectors(t *testing.T) {
	items := []Item{{
		Path:        "internal/search.go",
		SHA:         "abcdef123456",
		HTMLURL:     "https://github.com/example/project/blob/abcdef/internal/search.go",
		Repository:  Repository{FullName: "example/project"},
		TextMatches: []TextMatch{{Fragment: "func Search()"}},
	}}

	results := ResultsFromItems(SelectorCode, items)
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetSelector() != string(SelectorCode) || textFieldValue(t, result, "path") != "internal/search.go" || textFieldValue(t, result, "snippet") != "func Search()" || result.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("code result = %#v", result)
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"path"}) || !reflect.DeepEqual(result.GetFormat().GetDetailFields(), []string{"snippet"}) {
		t.Fatalf("code format = %#v", result.GetFormat())
	}
	if result.GetTargets()[0].GetUri().GetUri() != "https://github.com/example/project/blob/abcdef/internal/search.go" {
		t.Fatalf("target = %#v", result.GetTargets())
	}
}

func TestResultsFromItemsMapsIssueRepositoryURL(t *testing.T) {
	results := ResultsFromItems(SelectorIssue, []Item{{
		Number:        12,
		Title:         "Fix parser",
		State:         "open",
		HTMLURL:       "https://github.com/example/project/issues/12",
		RepositoryURL: "https://api.github.com/repos/example/project",
		UpdatedAt:     "2026-04-29T11:00:00Z",
	}})
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetSelector() != string(SelectorIssue) || integerFieldValue(t, result, "number") != 12 || textFieldValue(t, result, "title") != "Fix parser" || textFieldValue(t, result, "state") != "open" || result.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("issue result = %#v", result)
	}
	if timestampFieldValue(t, result, "updated_at") == nil {
		t.Fatal("issue result missing updated_at field")
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"number", "title"}) || !reflect.DeepEqual(result.GetFormat().GetDetailFields(), []string{"state", "updated_at"}) {
		t.Fatalf("issue format = %#v", result.GetFormat())
	}
}

func TestResultsFromItemsMapsCommitAndRepo(t *testing.T) {
	commitResults := ResultsFromItems(SelectorCommit, []Item{{
		SHA:        "abcdef123456",
		HTMLURL:    "https://github.com/example/project/commit/abcdef123456",
		Repository: Repository{FullName: "example/project"},
		Commit:     Commit{Message: "Fix parser\n\nBody", Author: CommitActor{Name: "Contributor", Date: "2026-04-29T11:00:00Z"}},
	}})
	if len(commitResults) != 1 || commitResults[0].GetSelector() != string(SelectorCommit) || textFieldValue(t, commitResults[0], "sha") != "abcdef1" || textFieldValue(t, commitResults[0], "message") != "Fix parser" {
		t.Fatalf("commit results = %#v", commitResults)
	}
	if textFieldValue(t, commitResults[0], "author") != "Contributor" || timestampFieldValue(t, commitResults[0], "authored_at") == nil {
		t.Fatalf("commit fields = %#v", commitResults[0].GetFields())
	}

	repoResults := ResultsFromItems(SelectorRepo, []Item{{
		FullName:        "example/project",
		Description:     "Synthetic project",
		Language:        "Go",
		StargazersCount: 42,
		HTMLURL:         "https://github.com/example/project",
		UpdatedAt:       "2026-04-29T11:00:00Z",
	}})
	if len(repoResults) != 1 || repoResults[0].GetSelector() != string(SelectorRepo) || textFieldValue(t, repoResults[0], "name") != "example/project" || repoResults[0].GetGroup().GetTitle() != "example" {
		t.Fatalf("repo results = %#v", repoResults)
	}
	if textFieldValue(t, repoResults[0], "description") != "Synthetic project" || textFieldValue(t, repoResults[0], "language") != "Go" || integerFieldValue(t, repoResults[0], "stars") != 42 || timestampFieldValue(t, repoResults[0], "updated_at") == nil {
		t.Fatalf("repo fields = %#v", repoResults[0].GetFields())
	}
}
