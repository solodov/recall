# Recall-compatible search providers

`recall` treats every configured source as an implementation of `recall.search.v1.SearchProvider`. A provider owns its source-specific storage, authentication, query dialect, indexing, and local result ordering. `recall` owns loading the operator registry, selecting providers, invoking provider RPCs, validating responses, blending provider-local ranks, and rendering results.

The operator registry lives in `$XDG_CONFIG_HOME/recall`, falling back to `$HOME/.config/recall`. Recall loads direct `*.txtpb` files in lexical order and merges them into one registry. Registry entries declare provider availability and transport only; they do not name a search method, filters, or indexing behavior.

The authoritative implementer contract is `proto/recall/search/v1/search.proto`. Use its comments for selector taxonomy, result field rules, validation constraints, and open-target boundaries.

## Provider shape

A new source should expose the search service from `proto/recall/search/v1/search.proto`:

```proto
service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc ListCapabilities(ListCapabilitiesRequest) returns (ListCapabilitiesResponse);
}

message SearchRequest {
  string query = 1;
  optional uint32 limit = 2;
  repeated string selector_hints = 3;
}
```

The raw query is intentionally provider-owned. Bash history, calendar, local notes, remote APIs, and other providers can each map the same query text to the search semantics that make sense for that source.

Selectors identify provider-local search surfaces. Providers advertise them through `ListCapabilities`, return them on every result, and may use `selector_hints` to avoid expensive unrequested work. Provider fields use provider-local `object:match` selectors such as `file:content`, while recall presents full `source:object:match` selectors to operators.

When `limit` is absent or zero, providers should return every reasonable match. A positive limit is a provider-local soft cap.

## Structured result model

Provider responses return best-first `SearchResponse.results` entries. Each result separates identity, display data, and opening data:

- `id` is stable provider-local machine identity; do not include the recall provider id.
- `selector` is the provider-local `object:match` surface that produced the result.
- `fields` are typed facts for rendered and machine-readable data: titles, snippets, timestamps, line numbers, ticket keys, statuses, authors, and counts.
- `format.title_fields` and `format.detail_fields` select which field keys appear in human output.
- fields not selected by `format` still remain in JSON output.
- `targets` and group targets are for opening only; do not put display-only data such as message timestamps in open targets.

A complete textproto result looks like this:

```textproto
results {
  id: "note:1"
  selector: "note:content"
  fields { key: "title" text: "Incident review notes" }
  fields { key: "snippet" text: "Rollback succeeded after cache invalidation." }
  fields { key: "updated_at" timestamp { seconds: 1777387800 } }
  targets { file { path: "/tmp/incident-review.md" } }
  group {
    key: "notebook:operations"
    title: "Operations notebook"
    targets { file { path: "/tmp" } }
  }
  format {
    title_fields: "title"
    detail_fields: "updated_at"
    detail_fields: "snippet"
  }
}
warnings {
  message: "served from a cached local index"
  code: "cache_used"
}
```

Native scores are preserved for diagnostics, but cross-source ranking uses provider-local result order and configured provider weight.

## Compatibility boundary

`recall.search.v1` now accepts only the structured `SearchResponse.results` shape documented in `proto/recall/search/v1/search.proto`. Providers that still emit the old hit-shaped response (`SearchHit`, `hits`, `title`, `snippet`, `occurred_at`, or display-only timestamp data on `UriTarget`) are incompatible.

There is no legacy decoder, compatibility shim, request/response translator, or mixed old/new validation path in this repository. External and sibling providers are not migrated here; their owners should update them independently from the proto contract and the first-party provider examples.

Validation for this repo should use in-repo configs and first-party providers. Do not rely on an operator's personal config as an acceptance check when it may include external providers that have not migrated yet.

## Stdio providers

One-shot stdio providers are RPC servers for a single call. `recall` starts the configured command and args, appends the RPC path as the final argument, writes the request payload to stdin, reads the response payload from stdout, and treats stderr as diagnostics.

Recall invokes these method paths:

```text
/recall.search.v1.SearchProvider/Search
/recall.search.v1.SearchProvider/ListCapabilities
```

Providers should parse the final path argument as:

```text
/<protobuf service>/<method>
```

The stdin and stdout bytes remain only the selected method's request and response payloads. Providers should auto-detect protobuf binary vs textproto input and mirror the same format for the response. This keeps `recall` efficient with binary protobuf while letting operators pipe textproto directly to a provider:

```bash
printf 'query: "rollout"\nselector_hints: "note:content"\n' |
  recall-example-provider /recall.search.v1.SearchProvider/Search
```

Because stdin is textproto, stdout is a structured textproto `SearchResponse` with `results { fields { ... } format { ... } }`.

Add `limit` only when you want to cap provider-local results:

```bash
printf 'query: "rollout"\nlimit: 10\n' |
  recall-example-provider /recall.search.v1.SearchProvider/Search
```

List advertised surfaces with:

```bash
printf '' | recall-example-provider /recall.search.v1.SearchProvider/ListCapabilities
```

## Provider implementation does not require code generation

The Go SDK is a convenience, not a requirement. A stdio provider can be any executable that accepts the final RPC path argument, reads one protobuf request from stdin, writes one protobuf response to stdout, and exits non-zero for fatal errors.

Normal recall calls send compact binary protobuf requests. Providers may respond with binary protobuf or textproto because recall auto-detects response format. That means a shell script can use `protoc --decode=recall.search.v1.SearchRequest` to inspect stdin, branch on `/recall.search.v1.SearchProvider/Search` or `/recall.search.v1.SearchProvider/ListCapabilities`, and print a textproto response. Small wrappers can start this way and move to generated types or the Go SDK only when that reduces maintenance cost.

## Go provider SDK

Go providers should import the public SDK instead of `internal` packages:

```go
import (
	"context"
	"time"

	recallprovider "github.com/solodov/recall/provider"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Provider struct{}

func (Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{{Selector: "note:content", Title: "Notes"}}}, nil
}

func (Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	result := &searchv1.SearchResponse_Result{
		Id:       "note:1",
		Selector: "note:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			{Key: "title", Value: &searchv1.SearchResponse_Result_Field_Text{Text: request.GetQuery()}},
			{Key: "snippet", Value: &searchv1.SearchResponse_Result_Field_Text{Text: "Matched note body"}},
			{Key: "updated_at", Value: &searchv1.SearchResponse_Result_Field_Timestamp{Timestamp: timestamppb.New(time.Now().UTC())}},
		},
		Targets: []*searchv1.OpenTarget{{Target: &searchv1.OpenTarget_File{File: &searchv1.FileTarget{Path: "/tmp/note.md"}}}},
		Format: &searchv1.SearchResponse_Result_Format{
			TitleFields:  []string{"title"},
			DetailFields: []string{"updated_at", "snippet"},
		},
	}
	return &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{result}}, nil
}

func main() {
	_ = recallprovider.ServeSearch(context.Background(), Provider{})
}
```

The SDK serves one `SearchProvider` stdio call, auto-detects binary protobuf or textproto input, mirrors the response format, and provides `RequestedLimit` and `RequestedSelectors` helpers for optional caller inputs.

## Open targets

Results and groups can expose typed open targets. `FileTarget` carries an absolute path plus optional 1-based line and column, while `UriTarget` carries a generic URI. Human output wraps primary targets in OSC 8 `recall://open?...` links so a terminal helper can pass the URL to `recall-open`.

`recall-open` loads the same registry and matches optional `openers` by source, selector, target type, and URI scheme. Opener commands are local operator config and are executed without a shell; if no opener matches, `recall-open` falls back to the platform opener on the original path or URI. A generic `target_types: "file"` opener can act as the default editor for every file link, including grouped source labels that link back to the provider block in the loaded registry. Source- or selector-specific openers remain supported and override generic defaults when both match.

## Future sources

Future sources should integrate as independent providers instead of expanding the core request schema:

- Bash history can search a local file, SQLite FTS table, or source-specific index and return command results.
- Schedule providers can own recurrence expansion, time windows, attendees, and calendar authentication.
- Message providers can own OAuth, API quotas, labels, snippets, and thread URIs.
- API-backed sources can run as stdio providers or gRPC services while keeping credentials and caching outside recall core.

This keeps providers independently deployable and lets operators enable or disable each source through config.

## Aggregate indexing

A giant local aggregate index is an optimization, not the baseline architecture. If one is useful, implement it as another `SearchProvider` with its own provider ID and registry entry. It can ingest whatever source exports it understands, then answer `Search` like every other provider.

Do not overload `recall.search.v1.SearchProvider.Search` with export, sync, or index-maintenance concerns. If export or indexing becomes a shared capability, add a separate protobuf service or a new versioned package once the requirements are clear. Federated search remains the stable baseline for sources that cannot or should not export their data.
