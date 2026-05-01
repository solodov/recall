---
id: 20260501-structured-search-result-format
title: Structured search results with provider-suggested field formatting
status: implementing
created: 2026-05-01
updated: 2026-05-01
currentPhase: 2
externalRef: 
origin: recall-jira provider design discussion
---

# Structured search results with provider-suggested field formatting

## Outcome

Replace the current hit-shaped search response with a structured result model: providers return ordered, typed fields plus a declarative render format, while `recall` remains provider-agnostic and owns generic rendering, ranking, grouping, timestamp localization, open-target handling, and JSON output.

The intended system shape:

- Providers return **machine identity** separately from **rendered content**.
- `id` remains stable provider-local identity and is not necessarily shown.
- Rendered labels, snippets, timestamps, line numbers, ticket keys, statuses, and summaries are normal typed fields.
- Providers suggest presentation through `format.title_fields` and `format.detail_fields`.
- `recall` resolves those field keys generically; it does not add Jira/Slack/GitHub-specific renderer branches.
- Open targets identify where to open; display positions such as Slack message timestamp, ripgrep line number, or Jira updated time are fields selected by the result format.

This is a breaking change in `recall.search.v1`. There is no compatibility phase, no old-shape decoding, no transitional adapters, and no responsibility to migrate external providers in this repo. External providers will fail against the new contract until their owners update them.

The protobuf contract should be self-documenting enough for provider authors to implement from `proto/recall/search/v1/search.proto` alone. Proto comments are part of the public contract and should define selector semantics, result identity, field key rules, format behavior, timestamp expectations, open-target boundaries, validation constraints, and concrete examples.

The important boundaries:

- **Provider contract**: stable ids, selectors, typed fields, open targets, optional group, optional score, optional render format, and thorough proto comments as the implementer-facing source of truth.
- **Core normalization/ranking**: validate structured data, preserve JSON, filter by selector, rank by provider-local order and provider weight.
- **Renderer**: turn fields and format hints into generic human output, including local-time timestamps and compact grouped rows.
- **Openers**: use targets only for opening, not for display layout decisions.
- **In-repo providers**: example, ripgrep, and GitHub providers migrate in the same change so this repo remains internally consistent and buildable.
- **External providers**: out of scope for implementation; their contract is the documented proto.

## Phases

- [x] 1. Establish the structured result proto as the public contract
- [ ] 2. Localize structured-result handling in core normalization
- [ ] 3. Render from fields and declarative format hints
- [ ] 4. Migrate every in-repo provider and provider-facing binary
- [ ] 5. Define the external provider boundary in docs only
- [ ] 6. Refresh docs, SDK examples, and debugging snippets
- [ ] 7. Validate by contract layer, in-repo providers, then full repo

## Phase Details

### Phase 1: Establish the structured result proto as the public contract

Replace the current hit-shaped response with a structured result model in `proto/recall/search/v1/search.proto`, then regenerate Go bindings through the existing `Justfile` flow.

The proto should be written as the primary public contract, not just a schema. Every service, message, field, and nested type should explain:

- whether the value is provider-owned or recall-owned;
- whether it is required, optional, or advisory;
- how recall validates it;
- how recall renders or preserves it;
- what provider authors should not encode there;
- representative examples for common provider families.

Target shape:

```proto
message SearchResponse {
  repeated Result results = 1;
  repeated Warning warnings = 2;

  message Result {
    string id = 1;
    string selector = 2;
    repeated Field fields = 3;
    repeated OpenTarget targets = 4;
    SearchGroup group = 5;
    optional double score = 6;
    Format format = 7;

    message Field {
      string key = 1;

      oneof value {
        string text = 2;
        int64 integer = 3;
        google.protobuf.Timestamp timestamp = 4;
      }
    }

    message Format {
      repeated string title_fields = 1;
      repeated string detail_fields = 2;
    }
  }

  message Warning {
    string message = 1;
    optional string code = 2;
  }
}
```

Keep `OpenTarget`, `UriTarget`, `FileTarget`, `SearchGroup`, `SearchSurface`, `SearchRequest`, and `ListCapabilities` as top-level protocol concepts unless implementation exposes a clear reason to nest them.

Document the structured result model directly in the proto:

- `Result.id` is stable provider-local machine identity, not necessarily display text.
- `Result.selector` is a full provider-local `object:match` selector and should match a listed `SearchSurface` when capabilities are available.
- `Field.key` is stable snake_case machine identity such as `ticket`, `summary`, `timestamp`, `line`, `column`, `snippet`, `status`, or `updated_at`.
- Field keys are unique within one result.
- Text fields are provider-normalized text; recall may collapse whitespace for human output.
- Integer fields are numeric facts such as line, column, count, priority rank, or sequence number.
- Timestamp fields are UTC instants from providers; recall renders human output in the operator’s local timezone without showing a timezone suffix.
- `Format.title_fields` and `Format.detail_fields` are presentation hints, not schema declarations.
- Fields not referenced by `Format` remain preserved in JSON.

Remove `UriTarget.timestamp`. URI targets identify what to open. If a source needs timestamp data to open correctly, encode it in the URI/permalink. If a timestamp should be displayed, return it as a normal `timestamp` field and select it through `format.title_fields` or `format.detail_fields`.

Document open-target boundaries clearly:

- `FileTarget.line` and `FileTarget.column` are file open-position metadata.
- Slack-style message timestamps, Jira updated time, GitHub authored time, and similar display facts belong in fields.
- Open targets should not carry display-only data.
- Group targets open the group; result targets open the individual result.

Contract tests should lock the stable shape:

- `SearchRequest` still has `query`, optional `limit`, and `selector_hints`.
- `SearchResponse` contains `results` and nested `Warning`.
- `SearchResponse.Result` contains `id`, `selector`, `fields`, `targets`, `group`, `score`, and `format`.
- `SearchResponse.Result.Field.Value` supports `text`, `integer`, and `timestamp`.
- `UriTarget` does not contain display timestamp fields.

### Phase 2: Localize structured-result handling in core normalization

Replace `SearchHit` usage in recall core with the new result model. Do not add compatibility adapters for old `SearchHit.title`, `snippet`, `kind`, or `occurred_at`; old providers are simply no longer compatible with this contract.

Introduce a small internal abstraction around result fields so normalization, rendering, ranking, and filtering can ask common questions without each reimplementing generated-proto lookups. This keeps the generated nested Go names contained and makes renderer behavior easier to test.

Core behavior stays stable:

- `internal/searchclient` reads and writes the new protobuf messages directly.
- `internal/normalize` validates result ids, selectors, fields, targets, groups, warnings, and timestamps.
- Ranking continues to use provider-local result order and provider weight.
- Field contents do not affect ranking.
- Selector filtering continues to inspect each result’s provider-local `selector`.
- JSON output preserves structured fields and provider format hints.

Validation should reject behavior-affecting malformed data:

- empty `id`
- empty `selector`
- duplicate field keys within one result
- field with empty `key`
- field with no `value` kind
- invalid timestamp values
- non-finite score
- malformed URI target
- file target with `column` but no `line`

### Phase 3: Render from fields and declarative format hints

Refactor `internal/render` so human output is derived from result fields and `Result.Format`, not legacy title/snippet/timestamp fields.

Renderer ownership:

- Providers choose stable field keys and suggest field order.
- `recall` owns separators, styles, labels, timestamp localization, OSC 8 links, grouping, and fallbacks.
- No source-specific renderer branches should be added for Jira, Slack, GitHub, or similar providers.

Format semantics:

- `format.title_fields` controls the first-line content.
- `format.detail_fields` controls ordered detail rows.
- Fields not listed in either format list are still preserved in JSON.
- Unknown format keys are skipped during human rendering.
- Duplicate keys in format lists are ignored after the first occurrence.
- If `title_fields` is empty or all referenced fields are missing, fallback uses the first available field as the title.
- If `detail_fields` is empty, fallback renders remaining fields as details.
- If `format` is absent, render the first field as the title line and remaining fields as details.

Do not add provider-controlled labels initially. `Field.key` remains the stable machine key, and recall humanizes keys for detail rows, such as `updated_at` → `updated at`.

Timestamp rendering assumes providers supply UTC instants. Human output localizes to the operator’s timezone and omits timezone suffixes. JSON preserves protobuf timestamps.

Renderer tests should cover:

- Jira-style `ticket + summary` title with ordered detail rows.
- Slack-style `timestamp + snippet` title.
- Ripgrep-style `line + snippet` grouped output.
- Fallback rendering when `format` is absent.
- Missing title/detail keys.
- Duplicate format keys.
- JSON preservation of fields not shown in human output.

### Phase 4: Migrate every in-repo provider and provider-facing binary

Update all in-repo providers to return the new response shape directly. This is core scope: the repository should not contain first-party providers, examples, or command binaries that still emit the old hit-shaped contract.

This should not mechanically wrap old `title` and `snippet`; each provider should expose fields that match its source semantics.

Provider mapping guidance:

- Do not put the recall provider id into result ids.
- Keep `id` stable and provider-local.
- Put user-visible identifiers into fields when they should render.
- Put snippets into a `snippet` field.
- Put source-domain times into named timestamp fields such as `updated_at`, `created_at`, `timestamp`, or `authored_at`.
- Use `format.title_fields` and `format.detail_fields` to express preferred generic presentation.
- Encode open-required time information in the URI/permalink, not in target metadata.

In-repo coverage must include:

- `examples/exampleprovider`: fixture fields and formats for notes/events, with SDK tests exercising `Search` and `ListCapabilities`.
- `cmd/recall-example-provider`: direct textproto examples and integration coverage pass with structured responses.
- `providers/ripgrep`: `path`, `line`, `column`, and `snippet` fields with compact grouped line output.
- `cmd/recall-ripgrep-provider`: binary smoke tests and direct provider snippets use structured fields.
- `providers/gh`: issue, PR, repo, commit, and code fields with source-appropriate title and detail formats.
- `cmd/recall-gh-provider`: selector configuration remains intact while emitted results use fields.
- `provider` SDK package tests: public API examples demonstrate `Result`, `Field`, and `Format`.
- repo examples and docs that pipe `SearchRequest` directly into providers.

Provider tests should assert meaningful field keys and formats, not just that old title/snippet strings were preserved somewhere. First-party providers should remain living examples for external implementers.

### Phase 5: Define the external provider boundary in docs only

External and sibling providers are not implementation scope for this repo. There is no compatibility mode to keep them running, and no coordinated migration work here. Their owners are responsible for updating to the documented proto contract.

The recall repo should only provide:

- a thoroughly documented protobuf contract;
- public SDK examples that compile against the new shape;
- first-party in-repo providers as reference implementations;
- clear docs that old hit-shaped providers are incompatible.

Do not add temporary adapters, legacy request/response translation, old field names, or mixed old/new validation paths to support external providers.

Validation should avoid relying on the operator’s personal config if it includes external providers that have not migrated. Use in-repo configs and first-party providers for acceptance.

### Phase 6: Refresh docs, SDK examples, and debugging snippets

Update docs so provider authors understand the new result model and the boundary between identity, fields, format, and targets.

Docs should cover:

- `README.md` provider SDK example.
- `docs/recall-compatible-search.md`.
- provider-specific docs such as `docs/recall-gh-provider.md` and `docs/recall-ripgrep-provider.md`.
- example config and direct textproto snippets.

Docs should defer to the proto for detailed contract rules instead of duplicating every rule, but they should clearly point implementers to `proto/recall/search/v1/search.proto` as the authoritative reference.

Docs should make these distinctions explicit:

- `id` is stable provider-local machine identity.
- rendered identifiers, timestamps, line numbers, snippets, and statuses are fields.
- `format.title_fields` and `format.detail_fields` select fields for human output.
- fields not rendered in human output remain available in JSON.
- open targets are for opening only and should not carry display-only data.
- provider examples should show complete `Result` payloads with fields and format hints.
- external providers must update themselves to the new breaking contract.

### Phase 7: Validate by contract layer, in-repo providers, then full repo

Validation should include build, lint, focused tests, and full tests. Build is required because generated proto and API drift can fail compilation before narrower tests expose the issue.

Use the repo’s Justfile workflow:

```bash
just build
just lint proto/recall/search/v1 internal/normalize internal/render internal/searchclient
just test proto/recall/search/v1 internal/normalize internal/render internal/searchclient
just test providers examples provider cmd
just test
```

Then validate representative direct provider textproto calls against in-repo providers only. Do not require the operator’s external providers or personal config to work as part of this repo change.

Final acceptance criteria:

- `recall` renders Jira-style detail rows without Jira-specific renderer code.
- Slack-style timestamp rows are produced from `timestamp` fields selected by format, not target metadata.
- Ripgrep grouped line output remains compact.
- Human timestamps are localized to the operator timezone without showing timezone suffixes.
- JSON output preserves structured result fields and format hints.
- Proto comments are complete enough for provider authors to implement without reverse-engineering recall internals.
- All in-repo providers, provider binaries, SDK examples, and direct textproto examples use the new structured response.
- The repo builds and tests against the new proto without old-contract compatibility shims.

## Plan Notes

## Summary

Replace the current hit-shaped search response with a structured result model: providers return ordered, typed fields plus a declarative render format, while `recall` remains provider-agnostic and owns generic rendering, ranking, grouping, timestamp localization, open-target handling, and JSON output.

The intended system shape:

- Providers return **machine identity** separately from **rendered content**.
- `id` remains stable provider-local identity and is not necessarily shown.
- Rendered labels, snippets, timestamps, line numbers, ticket keys, statuses, and summaries are normal typed fields.
- Providers suggest presentation through `format.title_fields` and `format.detail_fields`.
- `recall` resolves those field keys generically; it does not add Jira/Slack/GitHub-specific renderer branches.
- Open targets identify where to open; display positions such as Slack message timestamp, ripgrep line number, or Jira updated time are fields selected by the result format.

This is a breaking change in `recall.search.v1`. There is no compatibility phase, no old-shape decoding, no transitional adapters, and no responsibility to migrate external providers in this repo. External providers will fail against the new contract until their owners update them.

The protobuf contract should be self-documenting enough for provider authors to implement from `proto/recall/search/v1/search.proto` alone. Proto comments are part of the public contract and should define selector semantics, result identity, field key rules, format behavior, timestamp expectations, open-target boundaries, validation constraints, and concrete examples.

The important boundaries:

- **Provider contract**: stable ids, selectors, typed fields, open targets, optional group, optional score, optional render format, and thorough proto comments as the implementer-facing source of truth.
- **Core normalization/ranking**: validate structured data, preserve JSON, filter by selector, rank by provider-local order and provider weight.
- **Renderer**: turn fields and format hints into generic human output, including local-time timestamps and compact grouped rows.
- **Openers**: use targets only for opening, not for display layout decisions.
- **In-repo providers**: example, ripgrep, and GitHub providers migrate in the same change so this repo remains internally consistent and buildable.
- **External providers**: out of scope for implementation; their contract is the documented proto.

## Implementation details

### Phase 1: Establish the structured result proto as the public contract

Replace the current hit-shaped response with a structured result model in `proto/recall/search/v1/search.proto`, then regenerate Go bindings through the existing `Justfile` flow.

The proto should be written as the primary public contract, not just a schema. Every service, message, field, and nested type should explain:

- whether the value is provider-owned or recall-owned;
- whether it is required, optional, or advisory;
- how recall validates it;
- how recall renders or preserves it;
- what provider authors should not encode there;
- representative examples for common provider families.

Target shape:

```proto
message SearchResponse {
  repeated Result results = 1;
  repeated Warning warnings = 2;

  message Result {
    string id = 1;
    string selector = 2;
    repeated Field fields = 3;
    repeated OpenTarget targets = 4;
    SearchGroup group = 5;
    optional double score = 6;
    Format format = 7;

    message Field {
      string key = 1;

      oneof value {
        string text = 2;
        int64 integer = 3;
        google.protobuf.Timestamp timestamp = 4;
      }
    }

    message Format {
      repeated string title_fields = 1;
      repeated string detail_fields = 2;
    }
  }

  message Warning {
    string message = 1;
    optional string code = 2;
  }
}
```

Keep `OpenTarget`, `UriTarget`, `FileTarget`, `SearchGroup`, `SearchSurface`, `SearchRequest`, and `ListCapabilities` as top-level protocol concepts unless implementation exposes a clear reason to nest them.

Document the structured result model directly in the proto:

- `Result.id` is stable provider-local machine identity, not necessarily display text.
- `Result.selector` is a full provider-local `object:match` selector and should match a listed `SearchSurface` when capabilities are available.
- `Field.key` is stable snake_case machine identity such as `ticket`, `summary`, `timestamp`, `line`, `column`, `snippet`, `status`, or `updated_at`.
- Field keys are unique within one result.
- Text fields are provider-normalized text; recall may collapse whitespace for human output.
- Integer fields are numeric facts such as line, column, count, priority rank, or sequence number.
- Timestamp fields are UTC instants from providers; recall renders human output in the operator’s local timezone without showing a timezone suffix.
- `Format.title_fields` and `Format.detail_fields` are presentation hints, not schema declarations.
- Fields not referenced by `Format` remain preserved in JSON.

Remove `UriTarget.timestamp`. URI targets identify what to open. If a source needs timestamp data to open correctly, encode it in the URI/permalink. If a timestamp should be displayed, return it as a normal `timestamp` field and select it through `format.title_fields` or `format.detail_fields`.

Document open-target boundaries clearly:

- `FileTarget.line` and `FileTarget.column` are file open-position metadata.
- Slack-style message timestamps, Jira updated time, GitHub authored time, and similar display facts belong in fields.
- Open targets should not carry display-only data.
- Group targets open the group; result targets open the individual result.

Contract tests should lock the stable shape:

- `SearchRequest` still has `query`, optional `limit`, and `selector_hints`.
- `SearchResponse` contains `results` and nested `Warning`.
- `SearchResponse.Result` contains `id`, `selector`, `fields`, `targets`, `group`, `score`, and `format`.
- `SearchResponse.Result.Field.Value` supports `text`, `integer`, and `timestamp`.
- `UriTarget` does not contain display timestamp fields.

### Phase 2: Localize structured-result handling in core normalization

Replace `SearchHit` usage in recall core with the new result model. Do not add compatibility adapters for old `SearchHit.title`, `snippet`, `kind`, or `occurred_at`; old providers are simply no longer compatible with this contract.

Introduce a small internal abstraction around result fields so normalization, rendering, ranking, and filtering can ask common questions without each reimplementing generated-proto lookups. This keeps the generated nested Go names contained and makes renderer behavior easier to test.

Core behavior stays stable:

- `internal/searchclient` reads and writes the new protobuf messages directly.
- `internal/normalize` validates result ids, selectors, fields, targets, groups, warnings, and timestamps.
- Ranking continues to use provider-local result order and provider weight.
- Field contents do not affect ranking.
- Selector filtering continues to inspect each result’s provider-local `selector`.
- JSON output preserves structured fields and provider format hints.

Validation should reject behavior-affecting malformed data:

- empty `id`
- empty `selector`
- duplicate field keys within one result
- field with empty `key`
- field with no `value` kind
- invalid timestamp values
- non-finite score
- malformed URI target
- file target with `column` but no `line`

### Phase 3: Render from fields and declarative format hints

Refactor `internal/render` so human output is derived from result fields and `Result.Format`, not legacy title/snippet/timestamp fields.

Renderer ownership:

- Providers choose stable field keys and suggest field order.
- `recall` owns separators, styles, labels, timestamp localization, OSC 8 links, grouping, and fallbacks.
- No source-specific renderer branches should be added for Jira, Slack, GitHub, or similar providers.

Format semantics:

- `format.title_fields` controls the first-line content.
- `format.detail_fields` controls ordered detail rows.
- Fields not listed in either format list are still preserved in JSON.
- Unknown format keys are skipped during human rendering.
- Duplicate keys in format lists are ignored after the first occurrence.
- If `title_fields` is empty or all referenced fields are missing, fallback uses the first available field as the title.
- If `detail_fields` is empty, fallback renders remaining fields as details.
- If `format` is absent, render the first field as the title line and remaining fields as details.

Do not add provider-controlled labels initially. `Field.key` remains the stable machine key, and recall humanizes keys for detail rows, such as `updated_at` → `updated at`.

Timestamp rendering assumes providers supply UTC instants. Human output localizes to the operator’s timezone and omits timezone suffixes. JSON preserves protobuf timestamps.

Renderer tests should cover:

- Jira-style `ticket + summary` title with ordered detail rows.
- Slack-style `timestamp + snippet` title.
- Ripgrep-style `line + snippet` grouped output.
- Fallback rendering when `format` is absent.
- Missing title/detail keys.
- Duplicate format keys.
- JSON preservation of fields not shown in human output.

### Phase 4: Migrate every in-repo provider and provider-facing binary

Update all in-repo providers to return the new response shape directly. This is core scope: the repository should not contain first-party providers, examples, or command binaries that still emit the old hit-shaped contract.

This should not mechanically wrap old `title` and `snippet`; each provider should expose fields that match its source semantics.

Provider mapping guidance:

- Do not put the recall provider id into result ids.
- Keep `id` stable and provider-local.
- Put user-visible identifiers into fields when they should render.
- Put snippets into a `snippet` field.
- Put source-domain times into named timestamp fields such as `updated_at`, `created_at`, `timestamp`, or `authored_at`.
- Use `format.title_fields` and `format.detail_fields` to express preferred generic presentation.
- Encode open-required time information in the URI/permalink, not in target metadata.

In-repo coverage must include:

- `examples/exampleprovider`: fixture fields and formats for notes/events, with SDK tests exercising `Search` and `ListCapabilities`.
- `cmd/recall-example-provider`: direct textproto examples and integration coverage pass with structured responses.
- `providers/ripgrep`: `path`, `line`, `column`, and `snippet` fields with compact grouped line output.
- `cmd/recall-ripgrep-provider`: binary smoke tests and direct provider snippets use structured fields.
- `providers/gh`: issue, PR, repo, commit, and code fields with source-appropriate title and detail formats.
- `cmd/recall-gh-provider`: selector configuration remains intact while emitted results use fields.
- `provider` SDK package tests: public API examples demonstrate `Result`, `Field`, and `Format`.
- repo examples and docs that pipe `SearchRequest` directly into providers.

Provider tests should assert meaningful field keys and formats, not just that old title/snippet strings were preserved somewhere. First-party providers should remain living examples for external implementers.

### Phase 5: Define the external provider boundary in docs only

External and sibling providers are not implementation scope for this repo. There is no compatibility mode to keep them running, and no coordinated migration work here. Their owners are responsible for updating to the documented proto contract.

The recall repo should only provide:

- a thoroughly documented protobuf contract;
- public SDK examples that compile against the new shape;
- first-party in-repo providers as reference implementations;
- clear docs that old hit-shaped providers are incompatible.

Do not add temporary adapters, legacy request/response translation, old field names, or mixed old/new validation paths to support external providers.

Validation should avoid relying on the operator’s personal config if it includes external providers that have not migrated. Use in-repo configs and first-party providers for acceptance.

### Phase 6: Refresh docs, SDK examples, and debugging snippets

Update docs so provider authors understand the new result model and the boundary between identity, fields, format, and targets.

Docs should cover:

- `README.md` provider SDK example.
- `docs/recall-compatible-search.md`.
- provider-specific docs such as `docs/recall-gh-provider.md` and `docs/recall-ripgrep-provider.md`.
- example config and direct textproto snippets.

Docs should defer to the proto for detailed contract rules instead of duplicating every rule, but they should clearly point implementers to `proto/recall/search/v1/search.proto` as the authoritative reference.

Docs should make these distinctions explicit:

- `id` is stable provider-local machine identity.
- rendered identifiers, timestamps, line numbers, snippets, and statuses are fields.
- `format.title_fields` and `format.detail_fields` select fields for human output.
- fields not rendered in human output remain available in JSON.
- open targets are for opening only and should not carry display-only data.
- provider examples should show complete `Result` payloads with fields and format hints.
- external providers must update themselves to the new breaking contract.

### Phase 7: Validate by contract layer, in-repo providers, then full repo

Validation should include build, lint, focused tests, and full tests. Build is required because generated proto and API drift can fail compilation before narrower tests expose the issue.

Use the repo’s Justfile workflow:

```bash
just build
just lint proto/recall/search/v1 internal/normalize internal/render internal/searchclient
just test proto/recall/search/v1 internal/normalize internal/render internal/searchclient
just test providers examples provider cmd
just test
```

Then validate representative direct provider textproto calls against in-repo providers only. Do not require the operator’s external providers or personal config to work as part of this repo change.

Final acceptance criteria:

- `recall` renders Jira-style detail rows without Jira-specific renderer code.
- Slack-style timestamp rows are produced from `timestamp` fields selected by format, not target metadata.
- Ripgrep grouped line output remains compact.
- Human timestamps are localized to the operator timezone without showing timezone suffixes.
- JSON output preserves structured result fields and format hints.
- Proto comments are complete enough for provider authors to implement without reverse-engineering recall internals.
- All in-repo providers, provider binaries, SDK examples, and direct textproto examples use the new structured response.
- The repo builds and tests against the new proto without old-contract compatibility shims.
