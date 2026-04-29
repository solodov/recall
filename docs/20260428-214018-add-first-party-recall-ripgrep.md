---
id: 20260428-214018-add-first-party-recall-ripgrep
title: Add a first-party `recall-ripgrep-provider` implemented with the public Go provider SDK. `recall
status: done
created: 2026-04-29
updated: 2026-04-29
currentPhase: 
externalRef: 
origin: 
---

# Add a first-party `recall-ripgrep-provider` implemented with the public Go provider SDK. `recall

## Outcome

Add a first-party `recall-ripgrep-provider` implemented with the public Go provider SDK. `recall` remains provider-agnostic: it sends a `SearchRequest` over the existing stdio RPC path, and the ripgrep provider translates the provider-owned query language into a safe `rg` invocation.

The main data flow should be:

```text
recall CLI
  -> stdio SearchProvider.Search call
  -> recall-ripgrep-provider
  -> provider SDK path/input decoding
  -> ripgrep query parser
  -> root resolver filters configured roots
  -> rg --json process for existing roots only
  -> SearchResponse with code-match hits and optional warnings
  -> recall renderer
```

The important boundary is that ripgrep-specific concepts stay inside the provider. `recall` does not learn about code search, glob syntax, test files, root validation, ripgrep JSON, or query operators. It only sees normalized `SearchHit` data with useful titles, snippets, URIs, file-based groups, and provider warnings.

Initial query support should be intentionally small but shaped for extension:

```text
foo -in:test
```

Meaning:

- search code for literal text `foo`;
- exclude paths classified as test files or test directories;
- search only configured roots that currently exist;
- missing roots are a no-op, not a provider failure;
- return all matches when `limit` is absent, otherwise stop at the requested provider limit.

This gives us a useful provider now while leaving room to grow the query language behind a parser boundary.

## Phases

- [x] 1. Establish the first built-in provider shape
- [x] 2. Define a small query AST for ripgrep search
- [x] 3. Translate -in:test into ripgrep path exclusions
- [x] 4. Resolve roots and skip missing paths
- [x] 5. Encapsulate ripgrep execution and JSON parsing
- [x] 6. Map ripgrep matches into recall-friendly hits
- [x] 7. Wire distribution, examples, and operator config
- [x] 8. Test the seams, not ripgrep itself

## Phase Details

### Phase 1: Establish the first built-in provider shape

Add a built-in provider package and binary while keeping the SDK as the only stdio integration surface used by provider code.

Recommended shape:

```text
providers/ripgrep/                 # provider implementation
cmd/recall-ripgrep-provider/        # provider binary
```

The provider package should implement:

```go
type Provider struct { ... }

func (p *Provider) Search(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)
```

The binary should do only process setup: parse provider-specific flags, construct the provider, and call:

```go
recallprovider.ServeSearch(ctx, provider)
```

or `ServeSearchWithOptions` when flags need to pass remaining args through cleanly.

This keeps the new provider aligned with the SDK contract and makes it an example of how future first-party providers should be added.

### Phase 2: Define a small query AST for ripgrep search

Put query interpretation behind a parser boundary instead of scattering string checks through the provider.

Initial query language:

- free text becomes the ripgrep search text;
- `-in:test` excludes test files;
- no positive text is an invalid provider query.

The parser should produce a small provider-owned shape, for example:

```go
type Query struct {
    Pattern        string
    ExcludedScopes []Scope
}

type Scope string

const ScopeTest Scope = "test"
```

For the first version, treat the remaining non-operator text as a literal search phrase. That is closer to a user-facing search language than raw regex. The provider can translate it to `rg --fixed-strings`.

The boundary matters because future support for `in:docs`, quoted phrases, regex mode, Boolean terms, or path fields can extend the parser without changing the provider SDK or recall core.

### Phase 3: Translate -in:test into ripgrep path exclusions

Implement `in:test` as a named scope, not as hard-coded ad hoc globs at the call site.

For the initial exclusion, map test scope to path patterns such as:

```text
**/*_test.*
**/*.test.*
**/*.spec.*
**/test/**
**/tests/**
**/__tests__/**
```

The provider should translate `-in:test` into `rg --glob '!pattern'` arguments. This keeps the query language independent from ripgrep’s exact glob syntax and lets the scope mapping evolve.

Only `-in:test` needs to work initially. A positive `in:test` can be rejected as unsupported for now or parsed into the AST but not wired until inclusion semantics are designed.

### Phase 4: Resolve roots and skip missing paths

Add a root-resolution boundary before ripgrep execution. Provider roots come from repeatable `--root PATH` flags and default to the current directory when no root is configured.

The provider should stat roots at search time, because configured paths can appear or disappear while the provider binary stays unchanged. Root handling should be:

- existing file or directory roots are searched;
- missing roots are skipped and do not make the provider fail;
- if every configured root is missing, the provider returns an empty `SearchResponse` without invoking `rg`;
- skipped roots should be reported as provider warnings so configuration problems are visible without breaking federated search.

A warning shape such as this is enough:

```text
message: "ripgrep root does not exist: /path/to/repo"
code:    "ripgrep_root_missing"
```

This makes optional or machine-specific roots safe in shared configs while still giving the operator enough diagnostics to fix stale paths.

### Phase 5: Encapsulate ripgrep execution and JSON parsing

Run ripgrep through an argv builder, never a shell command. The runner should own process execution, context cancellation, stdout parsing, and ripgrep exit semantics.

The provider should call ripgrep roughly as:

```text
rg --json --fixed-strings --line-number --column --with-filename --color=never PATTERN EXISTING_ROOT...
```

Provider flags should include:

```text
--root PATH     repeatable, defaults to current directory
--rg PATH       optional ripgrep binary path, defaults to rg
```

Use `rg --json` so the provider can parse structured match events instead of scraping terminal output. Treat ripgrep exit code `1` as “no matches”, not an error. Treat other non-zero exits as provider errors, including stderr diagnostics.

For `SearchRequest.limit`:

- absent or zero means collect all ripgrep matches;
- positive means collect that many hits and then stop the ripgrep process cleanly.

This execution boundary is also the main testing seam: most tests can use a fake rg executable or runner rather than requiring ripgrep to be installed.

### Phase 6: Map ripgrep matches into recall-friendly hits

Return one `SearchHit` per matching line or submatch, with enough structure for recall’s existing renderer to look good without code-specific renderer changes.

Suggested hit shape:

```text
kind:  "code_match"
id:    stable root/path/line/column identity
title: "path/to/file.go:42:7"
snippet: source line text
uris:
  - name: "file", uri: "file:///absolute/path/to/file.go"
group:
  key:   relative file path or file URI
  title: relative file path
  uris:
    - name: "file", uri: "file:///absolute/path/to/file.go"
```

The group should be the file, so `recall --grouped` naturally clusters code matches by source file. The title and snippet should carry line/column and matched context so the default human renderer is already useful.

Warnings from skipped roots should be returned alongside hits. That keeps missing-root diagnostics visible without changing hit normalization or recall rendering.

### Phase 7: Wire distribution, examples, and operator config

Add the provider binary to the build/install workflow alongside existing binaries.

Example config:

```textproto
providers {
  id: "code"
  enabled: true
  weight: 1.0
  timeout_ms: 5000
  default_limit: 50
  stdio {
    command: "recall-ripgrep-provider"
    args: "--root"
    args: "/path/to/repo"
  }
}
```

Direct provider debugging should work with textproto:

```bash
printf 'query: "foo -in:test"\n' |
  recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

Document that missing `--root` paths are skipped as a no-op and surfaced as warnings. This is useful for shared or portable configs where not every workspace exists on every machine.

Add a short docs section that explains the provider’s role, the supported query subset, root behavior, and how `-in:test` maps to test-file exclusions. Keep core recall docs provider-agnostic; provider-specific docs can live with the ripgrep provider.

### Phase 8: Test the seams, not ripgrep itself

Tests should cover the provider boundaries:

- query parsing:
  - `foo`
  - `foo -in:test`
  - missing positive text
  - unsupported scope/operator behavior;
- scope-to-glob translation for `-in:test`;
- root resolution:
  - existing roots are passed to the runner;
  - missing roots are skipped;
  - all missing roots return no hits plus warnings without invoking `rg`;
- argv construction for ripgrep;
- JSON match parsing into `SearchHit`;
- limit handling, including absent limit returning all fake matches;
- SDK integration by invoking the provider through `ServeSearchWithOptions`;
- binary-level smoke test using a fake rg helper to avoid depending on local ripgrep availability.

This keeps tests deterministic while proving that recall can call the provider through the same SDK/stdiorpc path used by real operators.

## Plan Notes

## Summary

Add a first-party `recall-ripgrep-provider` implemented with the public Go provider SDK. `recall` remains provider-agnostic: it sends a `SearchRequest` over the existing stdio RPC path, and the ripgrep provider translates the provider-owned query language into a safe `rg` invocation.

The main data flow should be:

```text
recall CLI
  -> stdio SearchProvider.Search call
  -> recall-ripgrep-provider
  -> provider SDK path/input decoding
  -> ripgrep query parser
  -> root resolver filters configured roots
  -> rg --json process for existing roots only
  -> SearchResponse with code-match hits and optional warnings
  -> recall renderer
```

The important boundary is that ripgrep-specific concepts stay inside the provider. `recall` does not learn about code search, glob syntax, test files, root validation, ripgrep JSON, or query operators. It only sees normalized `SearchHit` data with useful titles, snippets, URIs, file-based groups, and provider warnings.

Initial query support should be intentionally small but shaped for extension:

```text
foo -in:test
```

Meaning:

- search code for literal text `foo`;
- exclude paths classified as test files or test directories;
- search only configured roots that currently exist;
- missing roots are a no-op, not a provider failure;
- return all matches when `limit` is absent, otherwise stop at the requested provider limit.

This gives us a useful provider now while leaving room to grow the query language behind a parser boundary.

## Implementation details

### Phase 1: Establish the first built-in provider shape

Add a built-in provider package and binary while keeping the SDK as the only stdio integration surface used by provider code.

Recommended shape:

```text
providers/ripgrep/                 # provider implementation
cmd/recall-ripgrep-provider/        # provider binary
```

The provider package should implement:

```go
type Provider struct { ... }

func (p *Provider) Search(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)
```

The binary should do only process setup: parse provider-specific flags, construct the provider, and call:

```go
recallprovider.ServeSearch(ctx, provider)
```

or `ServeSearchWithOptions` when flags need to pass remaining args through cleanly.

This keeps the new provider aligned with the SDK contract and makes it an example of how future first-party providers should be added.

### Phase 2: Define a small query AST for ripgrep search

Put query interpretation behind a parser boundary instead of scattering string checks through the provider.

Initial query language:

- free text becomes the ripgrep search text;
- `-in:test` excludes test files;
- no positive text is an invalid provider query.

The parser should produce a small provider-owned shape, for example:

```go
type Query struct {
    Pattern        string
    ExcludedScopes []Scope
}

type Scope string

const ScopeTest Scope = "test"
```

For the first version, treat the remaining non-operator text as a literal search phrase. That is closer to a user-facing search language than raw regex. The provider can translate it to `rg --fixed-strings`.

The boundary matters because future support for `in:docs`, quoted phrases, regex mode, Boolean terms, or path fields can extend the parser without changing the provider SDK or recall core.

### Phase 3: Translate `-in:test` into ripgrep path exclusions

Implement `in:test` as a named scope, not as hard-coded ad hoc globs at the call site.

For the initial exclusion, map test scope to path patterns such as:

```text
**/*_test.*
**/*.test.*
**/*.spec.*
**/test/**
**/tests/**
**/__tests__/**
```

The provider should translate `-in:test` into `rg --glob '!pattern'` arguments. This keeps the query language independent from ripgrep’s exact glob syntax and lets the scope mapping evolve.

Only `-in:test` needs to work initially. A positive `in:test` can be rejected as unsupported for now or parsed into the AST but not wired until inclusion semantics are designed.

### Phase 4: Resolve roots and skip missing paths

Add a root-resolution boundary before ripgrep execution. Provider roots come from repeatable `--root PATH` flags and default to the current directory when no root is configured.

The provider should stat roots at search time, because configured paths can appear or disappear while the provider binary stays unchanged. Root handling should be:

- existing file or directory roots are searched;
- missing roots are skipped and do not make the provider fail;
- if every configured root is missing, the provider returns an empty `SearchResponse` without invoking `rg`;
- skipped roots should be reported as provider warnings so configuration problems are visible without breaking federated search.

A warning shape such as this is enough:

```text
message: "ripgrep root does not exist: /path/to/repo"
code:    "ripgrep_root_missing"
```

This makes optional or machine-specific roots safe in shared configs while still giving the operator enough diagnostics to fix stale paths.

### Phase 5: Encapsulate ripgrep execution and JSON parsing

Run ripgrep through an argv builder, never a shell command. The runner should own process execution, context cancellation, stdout parsing, and ripgrep exit semantics.

The provider should call ripgrep roughly as:

```text
rg --json --fixed-strings --line-number --column --with-filename --color=never PATTERN EXISTING_ROOT...
```

Provider flags should include:

```text
--root PATH     repeatable, defaults to current directory
--rg PATH       optional ripgrep binary path, defaults to rg
```

Use `rg --json` so the provider can parse structured match events instead of scraping terminal output. Treat ripgrep exit code `1` as “no matches”, not an error. Treat other non-zero exits as provider errors, including stderr diagnostics.

For `SearchRequest.limit`:

- absent or zero means collect all ripgrep matches;
- positive means collect that many hits and then stop the ripgrep process cleanly.

This execution boundary is also the main testing seam: most tests can use a fake rg executable or runner rather than requiring ripgrep to be installed.

### Phase 6: Map ripgrep matches into recall-friendly hits

Return one `SearchHit` per matching line or submatch, with enough structure for recall’s existing renderer to look good without code-specific renderer changes.

Suggested hit shape:

```text
kind:  "code_match"
id:    stable root/path/line/column identity
title: "path/to/file.go:42:7"
snippet: source line text
uris:
  - name: "file", uri: "file:///absolute/path/to/file.go"
group:
  key:   relative file path or file URI
  title: relative file path
  uris:
    - name: "file", uri: "file:///absolute/path/to/file.go"
```

The group should be the file, so `recall --grouped` naturally clusters code matches by source file. The title and snippet should carry line/column and matched context so the default human renderer is already useful.

Warnings from skipped roots should be returned alongside hits. That keeps missing-root diagnostics visible without changing hit normalization or recall rendering.

### Phase 7: Wire distribution, examples, and operator config

Add the provider binary to the build/install workflow alongside existing binaries.

Example config:

```textproto
providers {
  id: "code"
  enabled: true
  weight: 1.0
  timeout_ms: 5000
  default_limit: 50
  stdio {
    command: "recall-ripgrep-provider"
    args: "--root"
    args: "/path/to/repo"
  }
}
```

Direct provider debugging should work with textproto:

```bash
printf 'query: "foo -in:test"\n' |
  recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

Document that missing `--root` paths are skipped as a no-op and surfaced as warnings. This is useful for shared or portable configs where not every workspace exists on every machine.

Add a short docs section that explains the provider’s role, the supported query subset, root behavior, and how `-in:test` maps to test-file exclusions. Keep core recall docs provider-agnostic; provider-specific docs can live with the ripgrep provider.

### Phase 8: Test the seams, not ripgrep itself

Tests should cover the provider boundaries:

- query parsing:
  - `foo`
  - `foo -in:test`
  - missing positive text
  - unsupported scope/operator behavior;
- scope-to-glob translation for `-in:test`;
- root resolution:
  - existing roots are passed to the runner;
  - missing roots are skipped;
  - all missing roots return no hits plus warnings without invoking `rg`;
- argv construction for ripgrep;
- JSON match parsing into `SearchHit`;
- limit handling, including absent limit returning all fake matches;
- SDK integration by invoking the provider through `ServeSearchWithOptions`;
- binary-level smoke test using a fake rg helper to avoid depending on local ripgrep availability.

This keeps tests deterministic while proving that recall can call the provider through the same SDK/stdiorpc path used by real operators.
