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

The initial query language is intentionally small:

```text
foo type:go -in:test
```

- Free text becomes a literal ripgrep search pattern.
- `type:foo` forwards `foo` as a ripgrep file type, equivalent to `rg --type foo`; repeat it to pass multiple types.
- `-in:test` excludes test files and conventional test directories.
- Positive `in:test` and negative `type:` filters are not supported yet.

`-in:test` maps to these ripgrep glob exclusions:

```text
!**/*_test.*
!**/*.test.*
!**/*.spec.*
!**/test/**
!**/tests/**
!**/__tests__/**
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

The provider mirrors the input format, so textproto input produces a textproto `SearchResponse`.
