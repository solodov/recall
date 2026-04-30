# Recall-compatible search providers

`recall` treats every configured source as an implementation of
`recall.search.v1.SearchProvider`. A provider owns its source-specific storage,
authentication, query dialect, indexing, and local result ordering. `recall`
owns loading the operator registry, selecting providers, invoking the search RPC,
validating responses, blending provider-local ranks, and rendering results.

The operator registry lives at `$XDG_CONFIG_HOME/recall/config.txtpb`, falling
back to `$HOME/.config/recall/config.txtpb`. Registry entries declare provider
availability and transport only; they do not name a search method, filters, or
indexing behavior.

## Provider shape

A new source should expose the existing search service:

```proto
service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;
  optional uint32 limit = 2;
  repeated string kind_hints = 3;
}
```

The raw query is intentionally provider-owned. Bash history, calendar, Gmail,
local notes, and other future providers can each map the same query text to the
search semantics that make sense for that source. Recall-level flags such as
`--source`, `--kind`, and `--grouped` remain orchestration or presentation
controls. `kind_hints` is advisory: providers may use it to avoid expensive work,
but recall still post-filters returned hits by kind. When `limit` is absent,
providers should return every reasonable match.

Provider responses should return best-first hits with stable IDs, kinds, titles,
open targets, optional groups, optional source-domain timestamps, optional native
scores, and warnings. Native scores are preserved for diagnostics, but cross-source
ranking uses provider-local result order and configured provider weight.

## Stdio providers

One-shot stdio providers are RPC servers for a single call. `recall` starts the
configured command and args, appends the RPC path as the final argument, writes
the request payload to stdin, reads the response payload from stdout, and treats
stderr as diagnostics.

For search, recall invokes:

```bash
provider [provider args] /recall.search.v1.SearchProvider/Search
```

Providers should parse the final path argument as:

```text
/<protobuf service>/<method>
```

The stdin and stdout bytes remain only the selected method's request and response
payloads. Providers should auto-detect protobuf binary vs textproto input and
mirror the same format for the response. This keeps `recall` efficient with
binary protobuf while letting operators pipe textproto directly to a provider:

```bash
printf 'query: "rollout"\n' |
  recall-example-provider /recall.search.v1.SearchProvider/Search
```

Add `limit` only when you want to cap provider-local results:

```bash
printf 'query: "rollout"\nlimit: 10\n' |
  recall-example-provider /recall.search.v1.SearchProvider/Search
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

func (Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	return &searchv1.SearchResponse{}, nil
}

func main() {
	_ = recallprovider.ServeSearch(context.Background(), Provider{})
}
```

The SDK serves one `/recall.search.v1.SearchProvider/Search` stdio call,
auto-detects binary protobuf or textproto input, mirrors the response format,
and provides `RequestedLimit` and `RequestedKinds` helpers for optional caller
hints.

## Open targets

Hits and groups can expose typed open targets. `FileTarget` carries an absolute
path plus optional 1-based line and column, while `UriTarget` carries a generic
URI. Human output wraps primary targets in OSC 8 `recall://open?...` links so a
terminal helper can pass the URL to `recall-open`.

`recall-open` loads the same registry and matches optional `openers` by source,
kind, target type, and URI scheme. Opener commands are local operator config and
are executed without a shell; if no opener matches, `recall-open` falls back to
the platform opener on the original path or URI. A generic `target_types: "file"`
opener can act as the default editor for every file link, including grouped
source labels that link back to the provider block in the loaded registry.
Source- or kind-specific openers remain supported and override generic defaults
when both match.

## Future sources

Future sources should integrate as independent providers instead of expanding the
core request schema:

- Bash history can search a local file, SQLite FTS table, or source-specific
  index and return command hits.
- Schedule providers can own recurrence expansion, time windows, attendees, and
  calendar authentication.
- Message providers can own OAuth, API quotas, labels, snippets, and thread URIs.
- API-backed sources can run as stdio providers or gRPC services while keeping
  credentials and caching outside recall core.

This keeps providers independently deployable and lets operators enable or
disable each source through config.

## Aggregate indexing

A giant local aggregate index is an optimization, not the baseline architecture.
If one is useful, implement it as another `SearchProvider` with its own provider
ID and registry entry. It can ingest whatever source exports it understands, then
answer `Search` like every other provider.

Do not overload `recall.search.v1.SearchProvider.Search` with export, sync, or
index-maintenance concerns. If export or indexing becomes a shared capability,
add a separate protobuf service or a new versioned package once the requirements
are clear. Federated search remains the stable baseline for sources that cannot
or should not export their data.
