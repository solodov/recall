package gh

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Item is the subset of GitHub REST search JSON fields needed for recall results.
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

// ResultsFromItems maps GitHub search items into grouped structured URI results.
func ResultsFromItems(selector Selector, items []Item) []*searchv1.SearchResponse_Result {
	results := make([]*searchv1.SearchResponse_Result, 0, len(items))
	for _, item := range items {
		if result := resultFromItem(selector, item); result != nil {
			results = append(results, result)
		}
	}
	return results
}

func resultFromItem(selector Selector, item Item) *searchv1.SearchResponse_Result {
	switch selector {
	case SelectorCode:
		return codeResult(item)
	case SelectorCommit:
		return commitResult(item)
	case SelectorIssue:
		return issueLikeResult(SelectorIssue, item)
	case SelectorPR:
		return issueLikeResult(SelectorPR, item)
	case SelectorRepo:
		return repoResult(item)
	default:
		return nil
	}
}

func codeResult(item Item) *searchv1.SearchResponse_Result {
	repo := repositoryName(item)
	path := firstNonEmpty(item.Path, item.Name)
	uri := itemURL(item)
	if repo == "" || path == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchResponse_Result{
		Id:       stableID(SelectorCode, repo, path, item.SHA),
		Selector: string(SelectorCode),
		Fields: fields(
			textField("path", path),
			textField("repository", repo),
			textField("snippet", firstTextFragment(item.TextMatches)),
		),
		Targets: []*searchv1.OpenTarget{uriTarget(uri)},
		Group:   repoGroup(repo),
		Format:  resultFormat([]string{"path"}, []string{"snippet"}),
	}
}

func commitResult(item Item) *searchv1.SearchResponse_Result {
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
	return &searchv1.SearchResponse_Result{
		Id:       stableID(SelectorCommit, repo, item.SHA),
		Selector: string(SelectorCommit),
		Fields: fields(
			textField("sha", sha),
			textField("message", message),
			textField("author", item.Commit.Author.Name),
			timestampField("authored_at", parseTime(item.Commit.Author.Date)),
		),
		Targets: []*searchv1.OpenTarget{uriTarget(uri)},
		Group:   repoGroup(repo),
		Format:  resultFormat([]string{"sha", "message"}, []string{"author", "authored_at"}),
	}
}

func issueLikeResult(selector Selector, item Item) *searchv1.SearchResponse_Result {
	repo := repositoryName(item)
	uri := itemURL(item)
	title := singleLine(item.Title)
	if repo == "" || item.Number == 0 || title == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchResponse_Result{
		Id:       stableID(selector, repo, fmt.Sprintf("%d", item.Number)),
		Selector: string(selector),
		Fields: fields(
			integerField("number", int64(item.Number)),
			textField("title", title),
			textField("state", item.State),
			timestampField("updated_at", parseTime(firstNonEmpty(item.UpdatedAt, item.CreatedAt))),
		),
		Targets: []*searchv1.OpenTarget{uriTarget(uri)},
		Group:   repoGroup(repo),
		Format:  resultFormat([]string{"number", "title"}, []string{"state", "updated_at"}),
	}
}

func repoResult(item Item) *searchv1.SearchResponse_Result {
	fullName := firstNonEmpty(item.FullName, repositoryName(item))
	uri := itemURL(item)
	if fullName == "" || uri == "" {
		return nil
	}
	return &searchv1.SearchResponse_Result{
		Id:       stableID(SelectorRepo, fullName),
		Selector: string(SelectorRepo),
		Fields: fields(
			textField("name", fullName),
			textField("description", singleLine(item.Description)),
			textField("language", item.Language),
			integerField("stars", int64(item.StargazersCount)),
			timestampField("updated_at", parseTime(item.UpdatedAt)),
		),
		Targets: []*searchv1.OpenTarget{uriTarget(uri)},
		Group:   ownerGroup(fullName),
		Format:  resultFormat([]string{"name"}, []string{"description", "language", "stars", "updated_at"}),
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

func fields(candidates ...*searchv1.SearchResponse_Result_Field) []*searchv1.SearchResponse_Result_Field {
	result := make([]*searchv1.SearchResponse_Result_Field, 0, len(candidates))
	for _, field := range candidates {
		if field != nil {
			result = append(result, field)
		}
	}
	return result
}

func textField(key string, value string) *searchv1.SearchResponse_Result_Field {
	value = singleLine(value)
	if value == "" {
		return nil
	}
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Text{Text: value},
	}
}

func integerField(key string, value int64) *searchv1.SearchResponse_Result_Field {
	if value == 0 {
		return nil
	}
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Integer{Integer: value},
	}
}

func timestampField(key string, value time.Time) *searchv1.SearchResponse_Result_Field {
	if value.IsZero() {
		return nil
	}
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Timestamp{Timestamp: timestamppb.New(value)},
	}
}

func resultFormat(titleFields []string, detailFields []string) *searchv1.SearchResponse_Result_Format {
	return &searchv1.SearchResponse_Result_Format{TitleFields: titleFields, DetailFields: detailFields}
}

func uriTarget(uri string) *searchv1.OpenTarget {
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: uri}}}
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
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
