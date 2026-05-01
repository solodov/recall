---
id: 20260501-structured-search-result-format
title: Structured search results with provider-suggested field formatting
status: active
created: 2026-05-01
updated: 2026-05-01
currentPhase: 1
externalRef: 
origin: recall-jira provider design discussion
---

# Structured search results with provider-suggested field formatting

## Outcome

Replace the current hit-shaped search response with a structured result model that lets providers return ordered, typed fields and a declarative rendering suggestion. This keeps `recall` provider-agnostic while supporting richer sources such as Jira, Slack, GitHub, and file search without source-specific renderers.

The core principle is:

- `id` is a stable provider-local machine identity, not necessarily a rendered label.
- Provider data is returned as typed fields with stable keys.
- Providers suggest which field keys appear on the title line and which appear as detail rows.
- `recall` owns generic styling, separators, timestamp localization, grouping, open targets, ranking, and JSON output.

This intentionally does not preserve the old `SearchHit.title`, `snippet`, or `occurred_at` shape. All known providers should be updated in the same rollout. If external provider compatibility becomes required before implementation starts, move this contract to `recall.search.v2` instead of changing `recall.search.v1` in place.

## Target protobuf shape

```proto
syntax = "proto3";

package recall.search.v1;

option go_package = "github.com/solodov/recall/proto/recall/search/v1;searchv1";

import "google/protobuf/timestamp.proto";

service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc ListCapabilities(ListCapabilitiesRequest) returns (ListCapabilitiesResponse);
}

message SearchRequest {
  string query = 1;
  optional uint32 limit = 2;
  repeated string selector_hints = 3;
}

message SearchResponse {
  repeated Result results = 1;
  repeated Warning warnings = 2;

  message Result {
    // Stable provider-local machine identity. It should not include the recall
    // provider id unless that prefix is native to the source.
    string id = 1;

    // Provider-local selector in object:match form.
    string selector = 2;

    // Ordered structured data returned for this result. Format references these
    // fields by key when suggesting title-line and detail rendering.
    repeated Field fields = 3;

    // Openable targets for this result, in provider-preferred order.
    repeated Target targets = 4;

    // Optional provider-preferred grouping identity.
    Group group = 5;

    // Optional provider-native score for diagnostics. Scores are not comparable
    // across providers and are not the primary blending signal.
    optional double score = 6;

    // Provider-suggested generic rendering layout.
    Format format = 7;

    message Field {
      // Stable machine key, such as "ticket", "summary", "priority", "status",
      // "updated_at", "assignee", "timestamp", or "snippet".
      string key = 1;

      // Optional display label. Empty means recall derives one from key.
      string label = 2;

      Value value = 3;

      message Value {
        oneof kind {
          string text = 1;
          int64 integer = 2;
          google.protobuf.Timestamp timestamp = 3;
        }
      }
    }

    message Target {
      oneof target {
        Uri uri = 1;
        File file = 2;
      }

      message Uri {
        string uri = 1;
      }

      message File {
        string path = 1;
        optional uint32 line = 2;
        optional uint32 column = 3;
      }
    }

    message Group {
      string key = 1;
      string title = 2;
      repeated Target targets = 3;
    }

    message Format {
      // Ordered field keys rendered on the first line.
      repeated string title_fields = 1;

      // Ordered field keys rendered below the first line as label/value rows.
      repeated string detail_fields = 2;
    }
  }

  message Warning {
    string message = 1;
    optional string code = 2;
  }
}

message ListCapabilitiesRequest {}

message ListCapabilitiesResponse {
  repeated Surface surfaces = 1;

  message Surface {
    string selector = 1;
    string title = 2;
    string description = 3;
  }
}
```

## Rendering contract

Human rendering should use the result format when present:

1. Build a field lookup by `Field.key`.
2. Render `format.title_fields` on the first line using recall-owned separators and terminal links.
3. Render `format.detail_fields` in order below the title line as `label: value` rows.
4. Derive a label from `key` when `Field.label` is empty: replace `_` with spaces and keep it lower-case.
5. Render timestamp values in the operator's local timezone.
6. Ignore missing format keys rather than failing the whole response; response normalization should report invalid duplicate field keys or invalid timestamps.
7. If no format is supplied, use a safe fallback: first non-empty field as the title line and remaining fields as detail rows.

Suggested first-line separator rule:

- two title fields: `first: second`
- more than two title fields: `first: second · third · fourth`
- one title field: `first`

Open links should wrap the rendered first line when a primary target exists. Detail fields are display data only unless future field-level targets are added.

## Examples

### Jira

Jira uses the ticket key as both the stable id and a rendered field. The summary remains a field rather than being duplicated into an old top-level title.

```textproto
results {
  id: "FD-101"
  selector: "ticket:content"

  fields { key: "ticket" value { text: "FD-101" } }
  fields { key: "summary" value { text: "Fix parser" } }
  fields { key: "priority" value { text: "High" } }
  fields { key: "status" value { text: "In Progress" } }
  fields { key: "updated_at" label: "last updated" value { timestamp: { seconds: 1777651200 } } }
  fields { key: "assignee" value { text: "Peter Solodov" } }
  fields { key: "snippet" value { text: "Matched description context..." } }

  targets { uri { uri: "https://fairewholesale.atlassian.net/browse/FD-101" } }
  group { key: "FD" title: "FD" }

  format {
    title_fields: "ticket"
    title_fields: "summary"
    detail_fields: "priority"
    detail_fields: "status"
    detail_fields: "updated_at"
    detail_fields: "assignee"
    detail_fields: "snippet"
  }
}
```

Expected human shape:

```text
FD-101: Fix parser
  priority: High
  status: In Progress
  last updated: 2026-05-01 09:00
  assignee: Peter Solodov
  snippet: Matched description context...
```

### Slack

Slack keeps a stable machine id separate from the rendered timestamp. The timestamp and snippet are just fields selected for the title line.

```textproto
results {
  id: "C123/1776377309.929809"
  selector: "message:content"

  fields { key: "timestamp" value { timestamp: { seconds: 1776377309 nanos: 929809000 } } }
  fields { key: "snippet" value { text: "Launch plan update" } }
  fields { key: "author" value { text: "alice" } }

  targets { uri { uri: "https://workspace.slack.com/archives/C123/p1776377309929809?thread_ts=1776377000.123456" } }
  group { key: "C123" title: "#eng" targets { uri { uri: "https://workspace.slack.com/archives/C123" } } }

  format {
    title_fields: "timestamp"
    title_fields: "snippet"
    detail_fields: "author"
  }
}
```

Expected human shape:

```text
2026-04-16 12:35: Launch plan update
  author: alice
```

### Ripgrep

File search can model line number and matched text as fields. The file target still carries precise open location.

```textproto
results {
  id: "/repo/main.go:42:1"
  selector: "file:content"

  fields { key: "line" value { integer: 42 } }
  fields { key: "snippet" value { text: "func Search(...)" } }

  targets { file { path: "/repo/main.go" line: 42 column: 1 } }
  group { key: "/repo/main.go" title: "main.go" targets { file { path: "/repo/main.go" } } }

  format {
    title_fields: "line"
    title_fields: "snippet"
  }
}
```

Expected grouped shape can remain compact:

```text
   42: func Search(...)
```

## Phases

- [ ] 1. Replace the search proto and regenerate Go bindings
- [ ] 2. Update core normalization, ranking, and transport clients
- [ ] 3. Implement generic field/format rendering
- [ ] 4. Migrate first-party recall providers
- [ ] 5. Coordinate sibling provider migrations
- [ ] 6. Refresh docs, examples, and direct-provider debugging snippets
- [ ] 7. Validate the full provider ecosystem

## Phase Details

### Phase 1: Replace the search proto and regenerate Go bindings

Update `proto/recall/search/v1/search.proto` to the target response shape above and regenerate `search.pb.go` with the existing `Justfile` flow.

This is a source-breaking contract change. Do not keep compatibility shims in the proto. Use the generated nested Go names such as `SearchResponse_Result` and `SearchResponse_Result_Field`; readable proto is more important than short generated identifiers.

Update proto contract tests so they lock:

- `SearchRequest` still has `query`, optional `limit`, and `selector_hints`.
- `SearchResponse` contains `results` and nested `Warning`.
- `SearchResponse.Result` contains `id`, `selector`, `fields`, `targets`, `group`, `score`, and `format`.
- `SearchResponse.Result.Field.Value` supports `text`, `integer`, and `timestamp`.

### Phase 2: Update core normalization, ranking, and transport clients

Replace `SearchHit` usage across recall core with `SearchResponse_Result`.

Important updates:

- `internal/searchclient` should read and write the new response message without special conversion.
- `internal/normalize` should validate result ids, selectors, field keys, field values, targets, groups, warnings, and timestamps.
- Ranking should continue to use provider-local result order and provider weight; field contents must not influence ranking.
- Selector filtering should continue to inspect each result's provider-local `selector`.
- JSON output should preserve structured fields and formats instead of flattening them back into legacy title/snippet fields.

Validation should reject or warn on behavior-affecting malformed data:

- empty `id`
- empty `selector`
- duplicate field keys within one result
- field with empty `key`
- field with no `value` kind
- invalid timestamp values
- file target with `column` but no `line`

### Phase 3: Implement generic field/format rendering

Refactor `internal/render` around result fields rather than top-level title/snippet/timestamp fields.

Renderer behavior:

- Grouping still uses `Result.Group`.
- Primary open target still wraps the first line in an OSC 8 `recall://open` link.
- `Format.title_fields` controls the first-line content.
- `Format.detail_fields` controls ordered detail rows.
- Missing format keys are skipped so partial provider data degrades gracefully.
- If no format is supplied, render the first field as the title line and remaining fields as details.
- If a grouped file result has a file target with line number and a `line` field in title fields, keep the compact line-oriented layout.
- If a grouped Slack-style result has a timestamp title field, render the timestamp as the line prefix through the same generic title-line rule rather than through `Uri.timestamp`.

Add focused render tests for:

- Jira-style `ticket + summary` title with ordered detail rows.
- Slack-style `timestamp + snippet` title.
- Ripgrep-style `line + snippet` grouped output.
- Fallback rendering when `format` is absent.
- Missing title/detail keys.

### Phase 4: Migrate first-party recall providers

Update in-repo providers to return the new response shape:

- `examples/exampleprovider`: fixture fields and formats for notes/events.
- `providers/ripgrep`: `line`, `column`, `path`, and `snippet` fields with compact line title format.
- `providers/gh`: issue/PR/repo/commit/code fields with source-appropriate title formats.
- provider tests and direct textproto examples.

Provider mapping guidance:

- Do not put the recall provider id into result ids.
- Put user-visible identifiers into fields only when they should render, even if they duplicate `id` for sources like Jira.
- Put snippets into a `snippet` field.
- Put source-domain times into named timestamp fields such as `updated_at`, `created_at`, `timestamp`, or `authored_at`.
- If a target needs time information to open correctly, encode it in the URI itself rather than in a target timestamp field.

### Phase 5: Coordinate sibling provider migrations

Update known external/sibling providers in the same rollout or immediately after the recall core change lands:

- `../recall-jira`: emit `ticket`, `summary`, `priority`, `status`, `updated_at`, `assignee`, and `snippet` fields. Use `id` equal to the Jira ticket key and title fields `ticket`, `summary`.
- `../recall-slack`: emit stable id such as `channel_id/message_ts`, title fields `timestamp`, `snippet`, and detail fields such as `author` when available. Remove dependence on `UriTarget.timestamp` for row rendering.
- `recall-notion-provider`: map page title/snippet/updated fields and choose a title format.
- Shiny's recall provider: map workflow title/state/todo/due/scheduled/tags into fields and format them explicitly.
- Any org-roam provider in local config should be migrated or temporarily disabled during the rollout.

Because this is a breaking contract, do not leave mixed old/new providers enabled in the operator config during validation.

### Phase 6: Refresh docs, examples, and direct-provider debugging snippets

Update documentation to describe the new field/format result model:

- `README.md` provider SDK example.
- `docs/recall-compatible-search.md`.
- provider-specific docs such as `docs/recall-gh-provider.md` and `docs/recall-ripgrep-provider.md`.
- example config and textproto snippets.

Docs should explain the distinction between `id` and rendered fields:

- `id` is stable provider-local machine identity.
- rendered identifiers, timestamps, line numbers, and snippets are normal fields selected by `format.title_fields`.

### Phase 7: Validate the full provider ecosystem

Run recall validation after each migration layer:

```bash
just test ./proto/recall/search/v1 ./internal/normalize ./internal/render ./internal/searchclient
just test ./providers/... ./examples/...
just test
```

Then validate direct provider textproto calls for representative providers and a federated `recall` run with the operator config after every enabled provider has been migrated.

The final acceptance criteria:

- `recall` renders Jira-style detail rows without Jira-specific renderer code.
- Slack timestamp rows are produced from title fields, not target-specific timestamp behavior.
- Ripgrep grouped line output remains compact.
- JSON output preserves structured result fields and format hints.
- All enabled providers compile and pass tests against the new proto.
