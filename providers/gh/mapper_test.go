package gh

import "testing"

func TestHitsFromItemsMapsGitHubSelectors(t *testing.T) {
	items := []Item{{
		Path:        "internal/search.go",
		SHA:         "abcdef123456",
		HTMLURL:     "https://github.com/example/project/blob/abcdef/internal/search.go",
		Repository:  Repository{FullName: "example/project"},
		TextMatches: []TextMatch{{Fragment: "func Search()"}},
	}}

	hits := HitsFromItems(SelectorCode, items)
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.GetSelector() != string(SelectorCode) || hit.GetTitle() != "internal/search.go" || hit.GetSnippet() != "func Search()" || hit.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("code hit = %#v", hit)
	}
	if hit.GetTargets()[0].GetUri().GetUri() != "https://github.com/example/project/blob/abcdef/internal/search.go" {
		t.Fatalf("target = %#v", hit.GetTargets())
	}
}

func TestHitsFromItemsMapsIssueRepositoryURL(t *testing.T) {
	hits := HitsFromItems(SelectorIssue, []Item{{
		Number:        12,
		Title:         "Fix parser",
		State:         "open",
		HTMLURL:       "https://github.com/example/project/issues/12",
		RepositoryURL: "https://api.github.com/repos/example/project",
		UpdatedAt:     "2026-04-29T11:00:00Z",
	}})
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.GetSelector() != string(SelectorIssue) || hit.GetTitle() != "#12 Fix parser" || hit.GetSnippet() != "open" || hit.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("issue hit = %#v", hit)
	}
	if hit.GetOccurredAt() == nil {
		t.Fatal("issue hit missing occurred_at")
	}
}

func TestHitsFromItemsMapsCommitAndRepo(t *testing.T) {
	commitHits := HitsFromItems(SelectorCommit, []Item{{
		SHA:        "abcdef123456",
		HTMLURL:    "https://github.com/example/project/commit/abcdef123456",
		Repository: Repository{FullName: "example/project"},
		Commit:     Commit{Message: "Fix parser\n\nBody", Author: CommitActor{Name: "Contributor", Date: "2026-04-29T11:00:00Z"}},
	}})
	if len(commitHits) != 1 || commitHits[0].GetSelector() != string(SelectorCommit) || commitHits[0].GetTitle() != "abcdef1 Fix parser" {
		t.Fatalf("commit hits = %#v", commitHits)
	}

	repoHits := HitsFromItems(SelectorRepo, []Item{{
		FullName:        "example/project",
		Description:     "Synthetic project",
		Language:        "Go",
		StargazersCount: 42,
		HTMLURL:         "https://github.com/example/project",
		UpdatedAt:       "2026-04-29T11:00:00Z",
	}})
	if len(repoHits) != 1 || repoHits[0].GetSelector() != string(SelectorRepo) || repoHits[0].GetTitle() != "example/project" || repoHits[0].GetGroup().GetTitle() != "example" {
		t.Fatalf("repo hits = %#v", repoHits)
	}
}
