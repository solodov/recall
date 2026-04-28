---
id: 20260428-131631-federated-recall-cli-protobuf-searchprovider
title: Federated recall CLI with protobuf SearchProvider contract
status: implementing
created: 2026-04-28
updated: 2026-04-28
currentPhase: 5
externalRef: 
origin: 
---

# Federated recall CLI with protobuf SearchProvider contract

## Outcome

Implement `recall` as a federated personal-search CLI with a durable provider RPC contract, an operator-owned provider registry, negotiated stdio payload encoding, and a working Go reference provider.

The intended system shape is:

- Search providers own source-specific indexing, auth, query translation, and local result ordering.
- `recall` owns provider discovery, safe execution, fan-out, partial failure handling, result blending, grouping layout, and rendering.
- Every configured provider is expected to serve `recall.search.v1.SearchProvider.Search`; service and method are not operator config.
- The operator-owned `.txtpb` config declares which providers exist and how to reach them, not which RPC or encoding to use.
- Stdio providers are treated as one-shot RPC servers backed by stdin/stdout.
- Stdio payload encoding is negotiated: providers advertise supported encodings, `recall` selects one, and the selected encoding is communicated as call metadata.
- Binary protobuf remains the preferred production encoding. Textproto is available for diagnostics/tests when the provider advertises support.
- The same search contract can be bound to one-shot stdio providers or network providers such as gRPC services.
- A first-party Go example provider demonstrates both RPC server responsibilities: capability advertisement and search handling.
- A sample `.txtpb` config registers that provider without service, method, or encoding fields.

The primary UX remains query-first:

```bash
recall alice meeting
recall "deploy notes"
recall --source org,shiny "bleve ranking"
recall --kind event,email "peter next week"
recall --grouped "project foo"
```

`--source` routes among configured providers. `--kind` can be implemented as `recall` post-filtering over returned hit kinds. Providers receive only `query` and `limit`, then return best-first results with named URIs, optional grouping, optional source-domain time, optional native score, and warnings.

Breaking provider API changes create a new package such as `recall.search.v2`. Additive protobuf-compatible changes remain in `recall.search.v1`. Config schema evolution follows the same rule with `recall.config.v1`.

## Phases

- [x] 1. Establish shared protobuf surfaces with search, config, and stdio RPC control boundaries
- [x] 2. Keep the .txtpb registry focused on provider availability
- [x] 3. Negotiate stdio payload encoding through provider capabilities
- [x] 4. Hide transports behind a typed search-provider client
- [ ] 5. Establish recall as the query-first orchestrator
- [ ] 6. Implement a Go example stdio RPC provider and sample config
- [ ] 7. Normalize and validate provider responses before ranking
- [ ] 8. Centralize rendering around named URIs and groups
- [ ] 9. Blend provider-local result order instead of provider scores
- [ ] 10. Adapt org-search and shiny as real providers
- [ ] 11. Keep query semantics source-specific
- [ ] 12. Leave room for future providers and aggregate indexing

## Phase Details

### Phase 1: Establish shared protobuf surfaces with search, config, and stdio RPC control boundaries

Create stable protobuf surfaces under `proto/`:

```text
proto/recall/search/v1/search.proto
proto/recall/config/v1/config.proto
proto/recall/rpc/v1/rpc.proto
```

`search.proto` defines the provider-facing search service:

```proto
syntax = "proto3";

package recall.search.v1;

import "google/protobuf/timestamp.proto";

service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;
  uint32 limit = 2;
}

message SearchResponse {
  repeated SearchHit hits = 1;
  repeated Warning warnings = 2;
}

message SearchHit {
  string id = 1;
  string kind = 2;
  string title = 3;
  optional string snippet = 4;
  optional double score = 5;
  repeated NamedUri uris = 6;
  SearchGroup group = 7;
  google.protobuf.Timestamp occurred_at = 8;
}

message NamedUri {
  string name = 1;
  string uri = 2;
}

message SearchGroup {
  string key = 1;
  string title = 2;
  repeated NamedUri uris = 3;
}

message Warning {
  string message = 1;
  optional string code = 2;
}
```

`config.proto` defines only the operator-owned provider registry:

```proto
syntax = "proto3";

package recall.config.v1;

message RecallConfig {
  repeated Provider providers = 1;
}

message Provider {
  string id = 1;
  bool enabled = 2;

  oneof transport {
    StdioTransport stdio = 10;
    GrpcTransport grpc = 11;
  }

  double weight = 20;
  uint32 timeout_ms = 21;
  uint32 default_limit = 22;
}

message StdioTransport {
  string command = 1;
  repeated string args = 2;
  map<string, string> env = 3;
}

message GrpcTransport {
  string endpoint = 1;
}
```

`rpc.proto` defines stdio RPC control-plane concepts, especially payload encoding negotiation:

```proto
syntax = "proto3";

package recall.rpc.v1;

enum PayloadEncoding {
  PAYLOAD_ENCODING_UNSPECIFIED = 0;
  PAYLOAD_ENCODING_PROTOBUF_BINARY = 1;
  PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO = 2;
}

service StdioRpcControl {
  rpc GetCapabilities(StdioRpcCapabilitiesRequest)
      returns (StdioRpcCapabilitiesResponse);
}

message StdioRpcCapabilitiesRequest {}

message StdioRpcCapabilitiesResponse {
  repeated PayloadEncoding supported_encodings = 1;
  PayloadEncoding preferred_encoding = 2;
}
```

The control surface is separate from config because encoding is a server capability, not operator policy.

### Phase 2: Keep the .txtpb registry focused on provider availability

The provider registry answers “what providers are available and how do I reach them.”

Example:

```proto
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 30
  stdio {
    command: "recall-example-provider"
  }
}

providers {
  id: "remote-mail"
  enabled: true
  weight: 1.0
  timeout_ms: 2500
  default_limit: 30
  grpc {
    endpoint: "dns:///mail-search.internal:443"
  }
}
```

It intentionally omits:

- `service`
- `method`
- `encoding`

Those are owned by `recall` and the provider RPC protocol. The config loader should validate provider IDs, enabled state, exactly one transport, required command/endpoint fields, positive timeout/default limit values, and reserved environment variable conflicts.

### Phase 3: Negotiate stdio payload encoding through provider capabilities

For stdio, `recall` first asks the provider what payload encodings it supports, then uses the best mutually supported encoding for the search call.

Because encoding negotiation has a bootstrap problem, the capability RPC should use a mandatory binary protobuf control call. That does not mean search must use binary; it only gives `recall` a reliable way to ask the server what it supports.

Capability call metadata:

```text
RECALL_RPC_SERVICE=recall.rpc.v1.StdioRpcControl
RECALL_RPC_METHOD=GetCapabilities
RECALL_RPC_ENCODING=protobuf_binary
```

Search call metadata:

```text
RECALL_RPC_SERVICE=recall.search.v1.SearchProvider
RECALL_RPC_METHOD=Search
RECALL_RPC_ENCODING=protobuf_binary | protobuf_textproto
```

The stdin/stdout bytes remain only the selected method’s request and response payloads. Service, method, and encoding are call metadata supplied by `recall`, not fields in the operator config.

Selection policy:

- prefer binary protobuf for normal searches when advertised;
- prefer textproto for diagnostics/golden tests when requested and advertised;
- fail the provider clearly if there is no common supported encoding;
- cache capabilities within the `recall` process so multiple calls do not repeat discovery unnecessarily.

### Phase 4: Hide transports behind a typed search-provider client

The orchestrator should call one typed boundary:

```text
SearchProvider.Search(SearchRequest) -> SearchResponse
```

Below that boundary:

- the stdio runner handles process execution, capability discovery, encoding selection, call metadata, stdin/stdout payloads, stderr diagnostics, timeouts, and decode errors;
- the gRPC runner calls `/recall.search.v1.SearchProvider/Search` with a deadline derived from provider config.

The stdio runner remains generic over protobuf messages. Search-specific behavior stays above it.

### Phase 5: Establish recall as the query-first orchestrator

`recall QUERY` loads the `.txtpb` provider registry, selects enabled providers, builds one `SearchRequest`, dispatches to selected providers, validates responses, blends or groups results, and renders output.

Management and diagnostics can be explicit subcommands later, such as:

```bash
recall providers
recall doctor
recall explain QUERY
```

The main path remains search-first and provider-agnostic.

### Phase 6: Implement a Go example stdio RPC provider and sample config

The Go example provider should prove the full server side:

- handle `recall.rpc.v1.StdioRpcControl.GetCapabilities`;
- advertise binary protobuf and textproto support;
- handle `recall.search.v1.SearchProvider.Search`;
- decode stdin using the selected call encoding;
- encode stdout using the same selected encoding;
- write diagnostics only to stderr.

The search implementation can use a small deterministic fixture, but should exercise IDs, kinds, titles, snippets, named URIs, optional groups, optional scores, and warnings.

The sample `.txtpb` config should register only the provider command and policy fields. That proves discovery, capability negotiation, search invocation, validation, and rendering without hard-coded providers.

### Phase 7: Normalize and validate provider responses before ranking

`recall` should annotate hits with the configured provider ID and validate semantic contract requirements before ranking or rendering.

Validation should catch:

- failed capability discovery;
- no mutually supported stdio payload encoding;
- invalid payload for the negotiated encoding;
- successful stdio exit with missing or undecodable stdout;
- non-zero stdio exit, timeout, or interruption;
- failed gRPC status;
- missing hit `id`, `kind`, or `title`;
- malformed URI or group objects;
- invalid timestamps;
- malformed warnings.

A bad provider should degrade that source without discarding usable results from other providers.

### Phase 8: Centralize rendering around named URIs and groups

Providers return data only. `recall` owns presentation.

The default human renderer should:

- link the result title to the first result URI when present;
- show later URIs as secondary actions;
- show source and kind as lightweight context;
- show snippets when present;
- show `occurred_at` for time-bearing results;
- group by source and provider group when grouped output is requested;
- link group headings to the first group URI when present;
- preserve all fields in machine-readable output.

This keeps source-native UX possible without provider-specific renderers.

### Phase 9: Blend provider-local result order instead of provider scores

Providers return hits best-first. `recall` should derive cross-provider ranking from provider-local position:

```text
derived_rank = 1 + hit_index_in_provider_response
blended_score = provider_weight / (k + derived_rank)
```

Provider-native `score` remains useful for diagnostics, but not for direct cross-provider comparison.

Later boosts can stay explicit: provider weight, recency from `occurred_at`, exact-title/phrase matches, and source/kind intent from recall-level flags.

### Phase 10: Adapt org-search and shiny as real providers

After the shared protos, config loader, negotiated stdio RPC runner, sample config, and Go example provider work, adapt existing corpora.

`org-search` should serve `SearchProvider.Search` over the same stdio RPC path and map results into the shared contract:

- `kind: "org_entry"`;
- cleaned headline as title;
- org-roam URI as the primary URI;
- file URI as a secondary URI when available;
- file-based group key/title/URI;
- optional Bleve score for diagnostics;
- no explicit rank field.

`shiny` should follow the same provider shape while preserving its own index and query semantics. Both should be enabled or disabled through operator `.txtpb` config.

### Phase 11: Keep query semantics source-specific

The v1 request stays intentionally small:

```proto
message SearchRequest {
  string query = 1;
  uint32 limit = 2;
}
```

`recall` flags are orchestration and presentation controls. Providers translate query text into their native semantics: Bleve, Gmail search, calendar matching, bash history matching, or another source-specific strategy.

If richer shared semantics become necessary, add compatible v1 fields with clear fallback behavior or introduce `recall.search.v2` for incompatible changes.

### Phase 12: Leave room for future providers and aggregate indexing

Bash history, calendar, Gmail, and other sources can each become independent `SearchProvider` implementations with their own storage and auth strategies.

A giant aggregate index remains a later optimization. The clean path is to implement it as another provider. If export/indexing becomes a shared capability, define it as a separate service rather than overloading search.

## Plan Notes

Agreed. Encoding should be a **provider-advertised capability**, not operator config. The registry says how to reach the provider; the provider server says which stdio payload encodings it supports; `recall` chooses one and communicates that choice as call metadata.

## Summary

Implement `recall` as a federated personal-search CLI with a durable provider RPC contract, an operator-owned provider registry, negotiated stdio payload encoding, and a working Go reference provider.

The intended system shape is:

- Search providers own source-specific indexing, auth, query translation, and local result ordering.
- `recall` owns provider discovery, safe execution, fan-out, partial failure handling, result blending, grouping layout, and rendering.
- Every configured provider is expected to serve `recall.search.v1.SearchProvider.Search`; service and method are not operator config.
- The operator-owned `.txtpb` config declares which providers exist and how to reach them, not which RPC or encoding to use.
- Stdio providers are treated as one-shot RPC servers backed by stdin/stdout.
- Stdio payload encoding is negotiated: providers advertise supported encodings, `recall` selects one, and the selected encoding is communicated as call metadata.
- Binary protobuf remains the preferred production encoding. Textproto is available for diagnostics/tests when the provider advertises support.
- The same search contract can be bound to one-shot stdio providers or network providers such as gRPC services.
- A first-party Go example provider demonstrates both RPC server responsibilities: capability advertisement and search handling.
- A sample `.txtpb` config registers that provider without service, method, or encoding fields.

The primary UX remains query-first:

```bash
recall alice meeting
recall "deploy notes"
recall --source org,shiny "bleve ranking"
recall --kind event,email "peter next week"
recall --grouped "project foo"
```

`--source` routes among configured providers. `--kind` can be implemented as `recall` post-filtering over returned hit kinds. Providers receive only `query` and `limit`, then return best-first results with named URIs, optional grouping, optional source-domain time, optional native score, and warnings.

Breaking provider API changes create a new package such as `recall.search.v2`. Additive protobuf-compatible changes remain in `recall.search.v1`. Config schema evolution follows the same rule with `recall.config.v1`.

## Implementation details

### 1. Establish shared protobuf surfaces with search, config, and stdio RPC control boundaries

Create stable protobuf surfaces under `proto/`:

```text
proto/recall/search/v1/search.proto
proto/recall/config/v1/config.proto
proto/recall/rpc/v1/rpc.proto
```

`search.proto` defines the provider-facing search service:

```proto
syntax = "proto3";

package recall.search.v1;

import "google/protobuf/timestamp.proto";

service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;
  uint32 limit = 2;
}

message SearchResponse {
  repeated SearchHit hits = 1;
  repeated Warning warnings = 2;
}

message SearchHit {
  string id = 1;
  string kind = 2;
  string title = 3;
  optional string snippet = 4;
  optional double score = 5;
  repeated NamedUri uris = 6;
  SearchGroup group = 7;
  google.protobuf.Timestamp occurred_at = 8;
}

message NamedUri {
  string name = 1;
  string uri = 2;
}

message SearchGroup {
  string key = 1;
  string title = 2;
  repeated NamedUri uris = 3;
}

message Warning {
  string message = 1;
  optional string code = 2;
}
```

`config.proto` defines only the operator-owned provider registry:

```proto
syntax = "proto3";

package recall.config.v1;

message RecallConfig {
  repeated Provider providers = 1;
}

message Provider {
  string id = 1;
  bool enabled = 2;

  oneof transport {
    StdioTransport stdio = 10;
    GrpcTransport grpc = 11;
  }

  double weight = 20;
  uint32 timeout_ms = 21;
  uint32 default_limit = 22;
}

message StdioTransport {
  string command = 1;
  repeated string args = 2;
  map<string, string> env = 3;
}

message GrpcTransport {
  string endpoint = 1;
}
```

`rpc.proto` defines stdio RPC control-plane concepts, especially payload encoding negotiation:

```proto
syntax = "proto3";

package recall.rpc.v1;

enum PayloadEncoding {
  PAYLOAD_ENCODING_UNSPECIFIED = 0;
  PAYLOAD_ENCODING_PROTOBUF_BINARY = 1;
  PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO = 2;
}

service StdioRpcControl {
  rpc GetCapabilities(StdioRpcCapabilitiesRequest)
      returns (StdioRpcCapabilitiesResponse);
}

message StdioRpcCapabilitiesRequest {}

message StdioRpcCapabilitiesResponse {
  repeated PayloadEncoding supported_encodings = 1;
  PayloadEncoding preferred_encoding = 2;
}
```

The control surface is separate from config because encoding is a server capability, not operator policy.

### 2. Keep the `.txtpb` registry focused on provider availability

The provider registry answers “what providers are available and how do I reach them.”

Example:

```proto
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 30
  stdio {
    command: "recall-example-provider"
  }
}

providers {
  id: "remote-mail"
  enabled: true
  weight: 1.0
  timeout_ms: 2500
  default_limit: 30
  grpc {
    endpoint: "dns:///mail-search.internal:443"
  }
}
```

It intentionally omits:

- `service`
- `method`
- `encoding`

Those are owned by `recall` and the provider RPC protocol. The config loader should validate provider IDs, enabled state, exactly one transport, required command/endpoint fields, positive timeout/default limit values, and reserved environment variable conflicts.

### 3. Negotiate stdio payload encoding through provider capabilities

For stdio, `recall` first asks the provider what payload encodings it supports, then uses the best mutually supported encoding for the search call.

Because encoding negotiation has a bootstrap problem, the capability RPC should use a mandatory binary protobuf control call. That does not mean search must use binary; it only gives `recall` a reliable way to ask the server what it supports.

Capability call metadata:

```text
RECALL_RPC_SERVICE=recall.rpc.v1.StdioRpcControl
RECALL_RPC_METHOD=GetCapabilities
RECALL_RPC_ENCODING=protobuf_binary
```

Search call metadata:

```text
RECALL_RPC_SERVICE=recall.search.v1.SearchProvider
RECALL_RPC_METHOD=Search
RECALL_RPC_ENCODING=protobuf_binary | protobuf_textproto
```

The stdin/stdout bytes remain only the selected method’s request and response payloads. Service, method, and encoding are call metadata supplied by `recall`, not fields in the operator config.

Selection policy:

- prefer binary protobuf for normal searches when advertised;
- prefer textproto for diagnostics/golden tests when requested and advertised;
- fail the provider clearly if there is no common supported encoding;
- cache capabilities within the `recall` process so multiple calls do not repeat discovery unnecessarily.

### 4. Hide transports behind a typed search-provider client

The orchestrator should call one typed boundary:

```text
SearchProvider.Search(SearchRequest) -> SearchResponse
```

Below that boundary:

- the stdio runner handles process execution, capability discovery, encoding selection, call metadata, stdin/stdout payloads, stderr diagnostics, timeouts, and decode errors;
- the gRPC runner calls `/recall.search.v1.SearchProvider/Search` with a deadline derived from provider config.

The stdio runner remains generic over protobuf messages. Search-specific behavior stays above it.

### 5. Establish `recall` as the query-first orchestrator

`recall QUERY` loads the `.txtpb` provider registry, selects enabled providers, builds one `SearchRequest`, dispatches to selected providers, validates responses, blends or groups results, and renders output.

Management and diagnostics can be explicit subcommands later, such as:

```bash
recall providers
recall doctor
recall explain QUERY
```

The main path remains search-first and provider-agnostic.

### 6. Implement a Go example stdio RPC provider and sample config

The Go example provider should prove the full server side:

- handle `recall.rpc.v1.StdioRpcControl.GetCapabilities`;
- advertise binary protobuf and textproto support;
- handle `recall.search.v1.SearchProvider.Search`;
- decode stdin using the selected call encoding;
- encode stdout using the same selected encoding;
- write diagnostics only to stderr.

The search implementation can use a small deterministic fixture, but should exercise IDs, kinds, titles, snippets, named URIs, optional groups, optional scores, and warnings.

The sample `.txtpb` config should register only the provider command and policy fields. That proves discovery, capability negotiation, search invocation, validation, and rendering without hard-coded providers.

### 7. Normalize and validate provider responses before ranking

`recall` should annotate hits with the configured provider ID and validate semantic contract requirements before ranking or rendering.

Validation should catch:

- failed capability discovery;
- no mutually supported stdio payload encoding;
- invalid payload for the negotiated encoding;
- successful stdio exit with missing or undecodable stdout;
- non-zero stdio exit, timeout, or interruption;
- failed gRPC status;
- missing hit `id`, `kind`, or `title`;
- malformed URI or group objects;
- invalid timestamps;
- malformed warnings.

A bad provider should degrade that source without discarding usable results from other providers.

### 8. Centralize rendering around named URIs and groups

Providers return data only. `recall` owns presentation.

The default human renderer should:

- link the result title to the first result URI when present;
- show later URIs as secondary actions;
- show source and kind as lightweight context;
- show snippets when present;
- show `occurred_at` for time-bearing results;
- group by source and provider group when grouped output is requested;
- link group headings to the first group URI when present;
- preserve all fields in machine-readable output.

This keeps source-native UX possible without provider-specific renderers.

### 9. Blend provider-local result order instead of provider scores

Providers return hits best-first. `recall` should derive cross-provider ranking from provider-local position:

```text
derived_rank = 1 + hit_index_in_provider_response
blended_score = provider_weight / (k + derived_rank)
```

Provider-native `score` remains useful for diagnostics, but not for direct cross-provider comparison.

Later boosts can stay explicit: provider weight, recency from `occurred_at`, exact-title/phrase matches, and source/kind intent from recall-level flags.

### 10. Adapt `org-search` and `shiny` as real providers

After the shared protos, config loader, negotiated stdio RPC runner, sample config, and Go example provider work, adapt existing corpora.

`org-search` should serve `SearchProvider.Search` over the same stdio RPC path and map results into the shared contract:

- `kind: "org_entry"`;
- cleaned headline as title;
- org-roam URI as the primary URI;
- file URI as a secondary URI when available;
- file-based group key/title/URI;
- optional Bleve score for diagnostics;
- no explicit rank field.

`shiny` should follow the same provider shape while preserving its own index and query semantics. Both should be enabled or disabled through operator `.txtpb` config.

### 11. Keep query semantics source-specific

The v1 request stays intentionally small:

```proto
message SearchRequest {
  string query = 1;
  uint32 limit = 2;
}
```

`recall` flags are orchestration and presentation controls. Providers translate query text into their native semantics: Bleve, Gmail search, calendar matching, bash history matching, or another source-specific strategy.

If richer shared semantics become necessary, add compatible v1 fields with clear fallback behavior or introduce `recall.search.v2` for incompatible changes.

### 12. Leave room for future providers and aggregate indexing

Bash history, calendar, Gmail, and other sources can each become independent `SearchProvider` implementations with their own storage and auth strategies.

A giant aggregate index remains a later optimization. The clean path is to implement it as another provider. If export/indexing becomes a shared capability, define it as a separate service rather than overloading search.

## Working Log

### 2026-04-28 — Planning turn
Request:
  config file is expected to exist in xdg hierarchy, worth specifying ~/.config/recall/config.txtpb
Response:
  Yes. Add this to the config boundary:

  - Canonical config path: `$XDG_CONFIG_HOME/recall/config.txtpb`
  - Fallback when `XDG_CONFIG_HOME` is unset: `~/.config/recall/config.txtpb`
  - The file is operator-owned and expected to exist; `recall` should not hard-code providers or silently auto-discover them.
  - Config path resolution should be centralized in the config loader, not repeated across commands.
  - Missing config should fail clearly with the resolved path and point to the sample config.
  - The Go sample provider should ship with a sample `config.txtpb` that can be copied into `~/.config/recall/config.txtpb`.

  Phase 2 should read more like:

  > `recall` loads the provider registry from the XDG config hierarchy: `$XDG_CONFIG_HOME/recall/config.txtpb`, falling back to `$HOME/.config/recall/config.txtpb`. This textproto file is operator-owned and is the only source of provider availability. It declares which providers are enabled and how to reach them, while service, method, and encoding remain owned by the recall RPC protocol.

  The sample config example can stay the same, but should be explicitly described as the contents of `~/.config/recall/config.txtpb` for the reference provider setup.
