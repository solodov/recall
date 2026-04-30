package gh

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Item is the subset of GitHub REST search JSON fields needed for recall hits.
type Item struct {
	Name            string       `json:"name"`
	Path            string       `json:"path"`
	SHA             string       `json:"sha"`
	HTMLURL         string       `json:"html_url"`
	URL             string       `json:"url"`
	Number          int          `json:"number"`
	Title           string       `json:"title"`
	State           string       `json:"state"`
	Description     string       `json:"description"`
	FullName        string       `json:"full_name"`
	Language        string       `json:"language"`
	StargazersCount int          `json:"stargazers_count"`
	RepositoryURL   string       `json:"repository_url"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
	Repository      Repository   `json:"repository"`
	Commit          Commit       `json:"commit"`
	TextMatches     []TextMatch  `json:"text_matches"`
	PullRequest     *PullRequest `json:"pull_request"`
}

type Repository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	Name     string `json:"name"`
	Owner    Owner  `json:"owner"`
}

type Owner struct {
	Login string `json:"login"`
}

type Commit struct {
	Message string      `json:"message"`
	Author  CommitActor `json:"author"`
}

type CommitActor struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

type TextMatch struct {
	Fragment string `json:"fragment"`
}

type PullRequest struct{}

// HitsFromItems maps GitHub search items into grouped recall URI hits.
func HitsFromItems(selector Selector, items []Item) []*searchv1.SearchHit {
	hits := make([]*searchv1.SearchHit, 0, len(items))
	for _, item := range items {
		if hit := hitFromItem(selector, item); hit != nil {
			hits = append(hits, hit)
		}
	}
	return hits
}

func hitFromItem(selector Selector, item Item) *searchv1.SearchHit {
	switch selector {
	case SelectorCode:
		return codeHit(item)
	case SelectorCommit:
		return commitHit(item)
	case SelectorIssue:
		return issueLikeHit(SelectorIssue, item)
	case SelectorPR:
		return issueLikeHit(SelectorPR, item)
	case SelectorRepo:
		return repoHit(item)
	default:
		return nil
	}
}

func codeHit(item Item) *searchv1.SearchHit {
	repo := repositoryName(item)
	path := firstNonEmpty(item.Path, item.Name)
	uri := itemURL(item)
	if repo == "" || path == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchHit{
		Id:       stableID(SelectorCode, repo, path, item.SHA),
		Selector: string(SelectorCode),
		Title:    path,
		Snippet:  optionalString(firstTextFragment(item.TextMatches)),
		Targets:  []*searchv1.OpenTarget{uriTarget(uri)},
		Group:    repoGroup(repo),
	}
}

func commitHit(item Item) *searchv1.SearchHit {
	repo := repositoryName(item)
	sha := shortSHA(item.SHA)
	message := firstLine(item.Commit.Message)
	uri := itemURL(item)
	if repo == "" || sha == "" || uri == "" {
		return nil
	}
	if message == "" {
		message = sha
	}
	return &searchv1.SearchHit{
		Id:         stableID(SelectorCommit, repo, item.SHA),
		Selector:   string(SelectorCommit),
		Title:      strings.TrimSpace(sha + " " + message),
		Snippet:    optionalString(item.Commit.Author.Name),
		Targets:    []*searchv1.OpenTarget{uriTarget(uri)},
		Group:      repoGroup(repo),
		OccurredAt: parseTimestamp(item.Commit.Author.Date),
	}
}

func issueLikeHit(selector Selector, item Item) *searchv1.SearchHit {
	repo := repositoryName(item)
	uri := itemURL(item)
	if repo == "" || item.Number == 0 || strings.TrimSpace(item.Title) == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchHit{
		Id:         stableID(selector, repo, fmt.Sprintf("%d", item.Number)),
		Selector:   string(selector),
		Title:      fmt.Sprintf("#%d %s", item.Number, singleLine(item.Title)),
		Snippet:    optionalString(item.State),
		Targets:    []*searchv1.OpenTarget{uriTarget(uri)},
		Group:      repoGroup(repo),
		OccurredAt: parseTimestamp(firstNonEmpty(item.UpdatedAt, item.CreatedAt)),
	}
}

func repoHit(item Item) *searchv1.SearchHit {
	fullName := firstNonEmpty(item.FullName, repositoryName(item))
	uri := itemURL(item)
	if fullName == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchHit{
		Id:         stableID(SelectorRepo, fullName),
		Selector:   string(SelectorRepo),
		Title:      fullName,
		Snippet:    optionalString(repoSnippet(item)),
		Targets:    []*searchv1.OpenTarget{uriTarget(uri)},
		Group:      ownerGroup(fullName),
		OccurredAt: parseTimestamp(item.UpdatedAt),
	}
}

func repoGroup(repo string) *searchv1.SearchGroup {
	return &searchv1.SearchGroup{Key: "repo:" + repo, Title: repo, Targets: repoTargets(repo)}
}

func ownerGroup(fullName string) *searchv1.SearchGroup {
	owner := strings.SplitN(fullName, "/", 2)[0]
	if owner == "" {
		owner = "GitHub"
	}
	return &searchv1.SearchGroup{Key: "owner:" + owner, Title: owner}
}

func repoTargets(repo string) []*searchv1.OpenTarget {
	if repo == "" {
		return nil
	}
	return []*searchv1.OpenTarget{uriTarget("https://github.com/" + repo)}
}

func repositoryName(item Item) string {
	if item.Repository.FullName != "" {
		return item.Repository.FullName
	}
	if item.Repository.Owner.Login != "" && item.Repository.Name != "" {
		return item.Repository.Owner.Login + "/" + item.Repository.Name
	}
	if item.FullName != "" {
		return item.FullName
	}
	return repoFromAPIURL(item.RepositoryURL)
}

func repoFromAPIURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	const marker = "/repos/"
	index := strings.Index(parsed.Path, marker)
	if index == -1 {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(parsed.Path[index:], marker), "/")
}

func itemURL(item Item) string {
	return firstNonEmpty(item.HTMLURL, item.Repository.HTMLURL, item.URL)
}

func firstTextFragment(matches []TextMatch) string {
	for _, match := range matches {
		if fragment := singleLine(match.Fragment); fragment != "" {
			return fragment
		}
	}
	return ""
}

func repoSnippet(item Item) string {
	parts := []string{}
	if item.Description != "" {
		parts = append(parts, singleLine(item.Description))
	}
	if item.Language != "" {
		parts = append(parts, item.Language)
	}
	if item.StargazersCount > 0 {
		parts = append(parts, fmt.Sprintf("%d stars", item.StargazersCount))
	}
	return strings.Join(parts, " · ")
}

func uriTarget(uri string) *searchv1.OpenTarget {
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: uri}}}
}

func optionalString(value string) *string {
	value = singleLine(value)
	if value == "" {
		return nil
	}
	return proto.String(value)
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}

func stableID(parts ...any) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(fmt.Sprint(part))
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ":")
}

func shortSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 7 {
		return value
	}
	return value[:7]
}

func firstLine(value string) string {
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		value = value[:index]
	}
	return singleLine(value)
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
