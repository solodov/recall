# Recall ripgrep provider

`recall-ripgrep-provider` is a first-party code-search provider backed by ripgrep. It implements `recall.search.v1.SearchProvider` through the public Go provider SDK, so `recall` treats it like any other configured provider.

## Registry entry

Configure it as a stdio provider. Use one or more `--root` arguments to choose files or directories to search:

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

If no `--root` is configured, the provider searches its current working directory. Missing configured roots are skipped as a no-op and returned as warnings with code `ripgrep_root_missing`, which keeps shared configs safe across machines. The provider passes `--no-follow` so interactive ripgrep config such as `--follow` does not turn broken symlinks into search warnings; add symlink targets as explicit `--root` values when you want recall to search them.

Missing paths encountered during traversal are downgraded to warnings instead of failing the whole provider.

Ripgrep hits include typed file targets with line and column metadata. Human output wraps result titles in OSC 8 `recall://` links that `recall-open` can dispatch to a configured editor.

## Query syntax

The provider searches both file paths and file contents by default:

```text
router type:go in:internal -in:test
```

- Free text searches file contents and also matches file paths by substring.
- Recall `--kind/-k path` returns only file-name/path matches.
- Recall `--kind/-k content` returns only content matches.
- `type:foo` forwards `foo` as a ripgrep file type, equivalent to `rg --type foo`; repeat it to pass multiple types.
- `in:regex` keeps only root-relative file paths matching the regex.
- `-in:regex` excludes root-relative file paths matching the regex.
- Negative `type:` filters are not supported yet.

A path-only query can omit free text when it has an inclusion filter:

```text
in:router
```

From `recall`, add `-k path` before the query to request only path hits:

```bash
recall -s code -k path in:router
```

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

Add advisory kind hints to debug provider-side narrowing directly:

```bash
printf 'query: "foo type:go -in:test"\nkind_hints: "content"\n' |
  dist/recall-ripgrep-provider --root /path/to/repo /recall.search.v1.SearchProvider/Search
```

The provider mirrors the input format, so textproto input produces a textproto `SearchResponse`.
