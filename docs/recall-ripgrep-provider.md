# Recall ripgrep provider

`recall-ripgrep-provider` is a first-party code-search provider backed by ripgrep. It implements `recall.search.v1.SearchProvider` through the public Go provider SDK, so `recall` treats it like any other configured provider.

## Registry entry

Configure it with a stdio transport. Use one or more `--root` arguments to choose files or directories to search:

```textproto
providers {
  id: "code"
  enabled: true
  weight: 1.0
  timeout_ms: 5000
  default_limit: 50
  transports {
    stdio {
      command: "recall-ripgrep-provider"
      args: "--root"
      args: "/path/to/repo"
    }
  }
}
```

If no `--root` is configured, the provider searches its current working directory. Missing configured roots are skipped as a no-op and returned as warnings with code `ripgrep_root_missing`, which keeps shared configs safe across machines. The provider passes `--no-follow` so interactive ripgrep config such as `--follow` does not turn broken symlinks into search warnings; add symlink targets as explicit `--root` values when you want recall to search them.

Missing paths encountered during traversal are downgraded to warnings instead of failing the whole provider.

Ripgrep results use the structured contract in `proto/recall/search/v1/search.proto`. Display paths, line numbers, columns, and snippets are typed fields selected by `format`; file targets carry the absolute path plus optional line/column only so `recall-open` can dispatch to an editor.

## Query syntax

The provider searches both file paths and file contents by default:

```text
router type:go in:internal -in:test
```

- Free text searches file contents and also matches file paths by substring.
- Selector `file:name` returns only file-name/path matches.
- Selector `file:content` returns only content matches.
- `type:foo` forwards `foo` as a ripgrep file type, equivalent to `rg --type foo`; repeat it to pass multiple types.
- `in:regex` keeps only root-relative file paths matching the regex.
- `-in:regex` excludes root-relative file paths matching the regex.
- Negative `type:` filters are not supported yet.

A path-only query can omit free text when it has an inclusion filter:

```text
in:router
```

From `recall`, select `code:file:name` to request only path results:

```bash
recall -s code:file:name in:router
```

## Structured result fields

The provider emits fields and format hints instead of legacy title/snippet data:

- `file:name` results expose `name`, `path`, and `directory`; title uses `name`, and the group is the parent directory.
- `file:content` results expose `path`, `line`, `column`, and `snippet`; grouped human output uses `line` plus `snippet` as the row title.

The same line/column values may also appear in `FileTarget` as open-position metadata. The fields are what recall renders; the target metadata is what editors use when opening a result.

## Direct provider debugging

Build the provider:

```bash
just build
```

Pipe a textproto `SearchRequest` directly to the provider:

```bash
printf 'query: "foo type:go -in:test"\n' |
  dist/recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

Add `limit` to cap matches:

```bash
printf 'query: "foo type:go -in:test"\nlimit: 20\n' |
  dist/recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

Add advisory selector hints to debug provider-side narrowing directly:

```bash
printf 'query: "foo type:go -in:test"\nselector_hints: "file:content"\n' |
  dist/recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

A matching content response is structured like this:

```textproto
results {
  id: "file_content:/path/to/repo/main.go:4:1"
  selector: "file:content"
  fields { key: "path" text: "main.go" }
  fields { key: "line" integer: 4 }
  fields { key: "column" integer: 1 }
  fields { key: "snippet" text: "foo()" }
  targets { file { path: "/path/to/repo/main.go" line: 4 column: 1 } }
  group {
    key: "main.go"
    title: "main.go"
    targets { file { path: "/path/to/repo/main.go" } }
  }
  format {
    title_fields: "line"
    title_fields: "snippet"
    detail_fields: "line"
    detail_fields: "snippet"
  }
}
```

List provider capabilities directly:

```bash
printf '' | dist/recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/ListCapabilities
```

The provider mirrors the input format, so textproto input produces a textproto response.
