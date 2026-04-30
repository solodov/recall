# Recall GitHub provider

`recall-gh-provider` is a first-party remote-search provider backed by the GitHub CLI. It uses `gh api` search endpoints and maps GitHub entity families onto recall result kinds.

The provider is intentionally opt-in per query: when recall sends no `kind_hints`, the provider returns no hits and performs no GitHub API calls. Use recall `--kind/-k` to choose the GitHub entity family you want.

## Registry entry

Configure it with a stdio transport:

```textproto
providers {
  id: "github"
  enabled: true
  weight: 1.0
  timeout_ms: 8000
  default_limit: 30
  transports {
    stdio {
      command: "recall-gh-provider"
    }
  }
}
```

By default, the provider supports all domains: `code`, `commit`, `issue`, `pr`, and `repo`. Use repeatable `--domain` args to restrict which GitHub domains this configured source can search:

```textproto
transports {
  stdio {
    command: "recall-gh-provider"
    args: "--domain"
    args: "issue"
    args: "--domain"
    args: "pr"
  }
}
```

## Query syntax

Queries are GitHub search queries passed to GitHub's search API. Use GitHub qualifiers such as `repo:`, `org:`, `user:`, `language:`, `path:`, `is:`, `label:`, and `author:` as appropriate for the selected kind.

```bash
recall -s github -k pr "repo:example/project parser"
recall -s github -k issue "org:example is:open label:bug"
recall -s github -k code "SearchRequest repo:example/project language:go"
recall -s github -k commit "repo:example/project fix parser"
recall -s github -k repo "example topic:search"
```

Kinds map to GitHub search domains:

- `code` searches code results.
- `commit` searches commits.
- `issue` searches issues.
- `pr` searches pull requests.
- `repo` searches repositories.

For `issue` and `pr`, the provider appends the corresponding `type:issue` or `type:pr` qualifier before calling GitHub.

## Direct provider debugging

Build the provider:

```bash
just build
```

Pipe a textproto `SearchRequest` directly to the provider. Include `kind_hints`; without them, the provider is a no-op.

```bash
printf 'query: "repo:example/project parser"\nkind_hints: "pr"\nlimit: 10\n' |
  dist/recall-gh-provider /recall.search.v1.SearchProvider/Search
```

The provider mirrors the input format, so textproto input produces a textproto `SearchResponse`.
