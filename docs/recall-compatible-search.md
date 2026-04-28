# Recall-compatible search providers

`recall` treats every configured source as an implementation of
`recall.search.v1.SearchProvider`. A provider owns its source-specific storage,
authentication, query dialect, indexing, and local result ordering. `recall`
owns loading the operator registry, selecting providers, invoking the search RPC,
validating responses, blending provider-local ranks, and rendering results.

The operator registry lives at `$XDG_CONFIG_HOME/recall/config.txtpb`, falling
back to `$HOME/.config/recall/config.txtpb`. Registry entries declare provider
availability and transport only; they do not name a search method, payload
encoding, filters, or indexing behavior.

## Provider shape

A new source should expose the existing search service:

```proto
service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;
  uint32 limit = 2;
}
```

The raw query is intentionally provider-owned. Bash history, calendar, Gmail,
local notes, and other future providers can each map the same query text to the
search semantics that make sense for that source. Recall-level flags such as
`--source`, `--kind`, and `--grouped` remain orchestration or presentation
controls and are not added to `SearchRequest`.

Provider responses should return best-first hits with stable IDs, kinds, titles,
named URIs, optional groups, optional source-domain timestamps, optional native
scores, and warnings. Native scores are preserved for diagnostics, but cross-source
ranking uses provider-local result order and configured provider weight.

## Stdio providers

One-shot stdio providers are RPC servers for a single call. `recall` supplies
call metadata in reserved `RECALL_RPC_*` environment variables, writes the
encoded request payload to stdin, reads the encoded response payload from stdout,
and treats stderr as diagnostics.

Stdio providers first serve `recall.rpc.v1.StdioRpcControl.GetCapabilities` with
binary protobuf so recall can discover supported payload encodings. Search calls
then use the selected encoding for both request and response payloads.

## Future sources

Future sources should integrate as independent providers instead of expanding the
core request schema:

- Bash history can search a local file, SQLite FTS table, or source-specific
  index and return command hits.
- Calendar providers can own recurrence expansion, time windows, attendees, and
  calendar authentication.
- Gmail providers can own OAuth, API quotas, labels, snippets, and thread URIs.
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
