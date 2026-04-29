package ripgrep

import (
	"path/filepath"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

func TestHitsFromMatchesMapsCodeMatchFields(t *testing.T) {
	root := t.TempDir()
	matchPath := filepath.Join(root, "src", "main.go")

	hits := HitsFromMatches([]Match{{
		Path:       matchPath,
		LineNumber: 42,
		Line:       `fmt.Println("foo")`,
		Submatches: []Submatch{{Text: "foo", Start: 13, End: 16}},
	}}, HitOptions{Roots: []string{root}})

	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.GetKind() != KindCodeMatch {
		t.Fatalf("kind = %q, want %q", hit.GetKind(), KindCodeMatch)
	}
	if hit.GetId() != "code_match:"+matchPath+":42:14" {
		t.Fatalf("id = %q", hit.GetId())
	}
	if hit.GetTitle() != "src/main.go:42:14" {
		t.Fatalf("title = %q, want relative file position", hit.GetTitle())
	}
	if hit.GetSnippet() != `fmt.Println("foo")` {
		t.Fatalf("snippet = %q", hit.GetSnippet())
	}
	if len(hit.GetTargets()) != 1 || hit.GetTargets()[0].GetFile().GetPath() != matchPath || hit.GetTargets()[0].GetFile().GetLine() != 42 || hit.GetTargets()[0].GetFile().GetColumn() != 14 {
		t.Fatalf("targets = %#v, want positioned file target", hit.GetTargets())
	}
	if hit.GetGroup().GetKey() != "src/main.go" || hit.GetGroup().GetTitle() != "src/main.go" {
		t.Fatalf("group = %#v, want file group", hit.GetGroup())
	}
	if len(hit.GetGroup().GetTargets()) != 1 || hit.GetGroup().GetTargets()[0].GetFile().GetPath() != matchPath {
		t.Fatalf("group targets = %#v, want file target", hit.GetGroup().GetTargets())
	}
}

func TestHitsFromMatchesEmitsOneHitPerMatchingLine(t *testing.T) {
	root := t.TempDir()
	hits := HitsFromMatches([]Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 3,
		Line:       "foo foo",
		Submatches: []Submatch{
			{Text: "foo", Start: 0, End: 3},
			{Text: "foo", Start: 4, End: 7},
		},
	}}, HitOptions{Roots: []string{root}})

	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want one hit per matching line", len(hits))
	}
	if hits[0].GetTitle() != "main.go:3:1" {
		t.Fatalf("title = %q, want first match position", hits[0].GetTitle())
	}
}

func TestHitsFromMatchesWithoutSubmatchesStillEmitsLineHit(t *testing.T) {
	root := t.TempDir()
	hits := HitsFromMatches([]Match{{
		Path:       filepath.Join(root, "main.go"),
		LineNumber: 7,
		Line:       "matched line",
	}}, HitOptions{Roots: []string{root}})

	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want fallback line hit", len(hits))
	}
	if hits[0].GetTitle() != "main.go:7:1" {
		t.Fatalf("title = %q, want column one fallback", hits[0].GetTitle())
	}
}

func TestMatchesToSearchResponseClonesWarnings(t *testing.T) {
	warning := &searchv1.Warning{Message: "missing", Code: proto.String(WarningRootMissing)}
	response := MatchesToSearchResponse(nil, []*searchv1.Warning{warning}, HitOptions{})
	warning.Message = "mutated"

	if len(response.GetWarnings()) != 1 {
		t.Fatalf("warning count = %d, want 1", len(response.GetWarnings()))
	}
	if response.GetWarnings()[0].GetMessage() != "missing" || response.GetWarnings()[0].GetCode() != WarningRootMissing {
		t.Fatalf("warnings = %#v, want cloned missing-root warning", response.GetWarnings())
	}
}
