# Recall-compatible search providers

`recall` treats every configured source as an implementation of `recall.search.v1.SearchProvider`. A provider owns its source-specific storage, authentication, query dialect, indexing, and local result ordering. `recall` owns loading the operator registry, selecting providers, invoking provider RPCs, validating responses, blending provider-local ranks, and rendering results.

The operator registry lives at `$XDG_CONFIG_HOME/recall/config.txtpb`, falling back to `$HOME/.config/recall/config.txtpb`. Registry entries declare provider availability and transport only; they do not name a search method, filters, or indexing behavior.

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

Selectors identify provider-local search surfaces. Providers advertise them through `ListCapabilities`, return them on every result, and may use `selector_hints` to avoid expensive unrequested work. Use the proto comments as the authoritative selector taxonomy reference; provider fields use provider-local `object:match` selectors such as `file:content`, while recall presents full `source:object:match` selectors to operators.

When `limit` is absent or zero, providers should return every reasonable match. A positive limit is a provider-local soft cap.

Provider responses should return best-first structured results with stable IDs, selectors, typed fields, provider-suggested `format`, open targets, optional groups, optional native scores, and warnings. Rendered titles, snippets, timestamps, line numbers, statuses, and user-visible identifiers are fields. Native scores are preserved for diagnostics, but cross-source ranking uses provider-local result order and configured provider weight.

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

Add `limit` only when you want to cap provider-local results:

```bash
printf 'query: "rollout"\nlimit: 10\n' |
  recall-example-provider /recall.search.v1.SearchProvider/Search
```

List advertised surfaces with:

```bash
printf '' | recall-example-provider /recall.search.v1.SearchProvider/ListCapabilities
```

## Go provider SDK

Go providers should import the public SDK instead of `internal` packages:

```go
import (
	"context"

	recallprovider "github.com/solodov/recall/provider"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

type Provider struct{}

func (Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{{Selector: "note:content", Title: "Notes"}}}, nil
}

func (Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	return &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{{
		Id:       "note:1",
		Selector: "note:content",
		Fields: []*searchv1.SearchResponse_Result_Field{{
			Key:   "title",
			Value: &searchv1.SearchResponse_Result_Field_Text{Text: request.GetQuery()},
		}},
		Format: &searchv1.SearchResponse_Result_Format{TitleFields: []string{"title"}},
	}}}, nil
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

- Bash history can search a local file, SQLite FTS table, or source-specific index and return command hits.
- Schedule providers can own recurrence expansion, time windows, attendees, and calendar authentication.
- Message providers can own OAuth, API quotas, labels, snippets, and thread URIs.
- API-backed sources can run as stdio providers or gRPC services while keeping credentials and caching outside recall core.

This keeps providers independently deployable and lets operators enable or disable each source through config.

## Aggregate indexing

A giant local aggregate index is an optimization, not the baseline architecture. If one is useful, implement it as another `SearchProvider` with its own provider ID and registry entry. It can ingest whatever source exports it understands, then answer `Search` like every other provider.

Do not overload `recall.search.v1.SearchProvider.Search` with export, sync, or index-maintenance concerns. If export or indexing becomes a shared capability, add a separate protobuf service or a new versioned package once the requirements are clear. Federated search remains the stable baseline for sources that cannot or should not export their data.
