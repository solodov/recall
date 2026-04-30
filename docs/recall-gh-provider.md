# Recall GitHub provider

`recall-gh-provider` is a first-party remote-search provider backed by the GitHub CLI. It uses `gh api` search endpoints and exposes GitHub entity families as provider-local selectors.

The provider is intentionally opt-in per query: when recall sends no `selector_hints`, the provider returns no hits and performs no GitHub API calls. Use recall selectors to choose the GitHub surface you want.

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

By default, the provider supports all selectors: `file:content`, `commit:content`, `issue:content`, `pr:content`, and `repo:name`. Use repeatable `--selector` args to restrict which GitHub selectors this configured source can search:

```textproto
transports {
  stdio {
    command: "recall-gh-provider"
    args: "--selector"
    args: "issue:content"
    args: "--selector"
    args: "pr:content"
  }
}
```

## Query syntax

Queries are GitHub search queries passed to GitHub's search API. Use GitHub qualifiers such as `repo:`, `org:`, `user:`, `language:`, `path:`, `is:`, `label:`, and `author:` as appropriate for the selected surface.

```bash
recall -s github:pr:content "repo:example/project parser"
recall -s github:issue:content "org:example is:open label:bug"
recall -s github:file:content "SearchRequest repo:example/project language:go"
recall -s github:commit:content "repo:example/project fix parser"
recall -s github:repo:name "example topic:search"
```

Selectors map to GitHub search endpoints:

- `file:content` searches code results.
- `commit:content` searches commits.
- `issue:content` searches issues.
- `pr:content` searches pull requests.
- `repo:name` searches repositories.

For `issue:content` and `pr:content`, the provider appends the corresponding `type:issue` or `type:pr` qualifier before calling GitHub.

## Direct provider debugging

Build the provider:

```bash
just build
```

Pipe a textproto `SearchRequest` directly to the provider. Include `selector_hints`; without them, the provider is a no-op.

```bash
printf 'query: "repo:example/project parser"\nselector_hints: "pr:content"\nlimit: 10\n' |
  dist/recall-gh-provider /recall.search.v1.SearchProvider/Search
```

List provider capabilities directly:

```bash
printf '' | dist/recall-gh-provider /recall.search.v1.SearchProvider/ListCapabilities
```

The provider mirrors the input format, so textproto input produces a textproto response.
