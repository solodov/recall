# recall

Federated personal search across local and remote sources.

`recall` gives scattered information one command-line front door. Code, GitHub, notes, tickets, messages, calendars, and future sources can each keep their own search semantics while exposing searchable surfaces through one provider model.

See [docs/why.md](docs/why.md) for the motivation: making personal and work information discoverable instead of relying on memory, aliases, and one-off search habits.

## What it feels like

List the sources and surfaces you have available:

```bash
recall -ls
```

Search everything enabled in your registry:

```bash
recall "checkout parser"
```

Search local code through the ripgrep provider:

```bash
recall -s code:file:content "checkout type:go"
```

Use provider-owned query syntax for the small stuff, like excluding tests:

```bash
recall -s code:file:content "checkout type:go -in:test"
```

Search only file names/paths:

```bash
recall -s code:file:name in:router
```

Search GitHub through the `gh`-backed provider:

```bash
recall -s github:pr:content "repo:example/project parser"
recall -s github:issue:content "org:example is:open label:bug"
recall -s github:file:content "SearchRequest repo:example/project language:go"
```

Ask for JSON when another tool or agent needs structured output:

```bash
recall -f json "rollout"
```

Human output is grouped by source and provider-native context. When providers return open targets, terminals that support OSC 8 links can open files, URLs, pull requests, messages, or provider config locations through `recall-open`.

## First-party providers

- `recall-example-provider` demonstrates the provider contract with deterministic fixture data.
- `recall-ripgrep-provider` searches local paths and file contents with ripgrep; see [docs/recall-ripgrep-provider.md](docs/recall-ripgrep-provider.md).
- `recall-gh-provider` searches GitHub code, commits, issues, pull requests, and repositories through `gh`; see [docs/recall-gh-provider.md](docs/recall-gh-provider.md).

## How it works

A recall source is a provider implementing `recall.search.v1.SearchProvider` from [proto/recall/search/v1/search.proto](proto/recall/search/v1/search.proto). Providers advertise local search surfaces such as `file:content`, `file:name`, or `pr:content`; recall prefixes them with the configured source id, such as `code:file:content` or `github:pr:content`.

`recall` owns orchestration and presentation:

- load the operator registry;
- select providers and surfaces;
- fan out the query;
- validate structured responses;
- blend provider-local ranks;
- render human or JSON output;
- turn open targets into clickable terminal links.

Providers own source-specific behavior:

- authentication and indexing;
- query syntax;
- source-native result ordering;
- field mapping;
- open targets and grouping.

This keeps powerful provider-specific search features available without making the CLI a pile of disconnected aliases.

## Run the example

The example script builds `recall` and the example provider, writes a temporary config, and runs a search:

```bash
examples/run-example.sh
examples/run-example.sh rollout
examples/run-example.sh --format json rollout
```

## Configure sources

The default registry path is `$XDG_CONFIG_HOME/recall/config.txtpb`, falling back to `$HOME/.config/recall/config.txtpb`. Example registry snippets live in:

- [examples/config.txtpb](examples/config.txtpb)
- [examples/ripgrep.config.txtpb](examples/ripgrep.config.txtpb)
- [examples/gh.config.txtpb](examples/gh.config.txtpb)

Provider entries declare availability and transports only. Service and method are protocol-owned, so recall appends the selected RPC path at call time.

## Build providers

Go providers can use the public SDK in [provider](provider). The provider-facing contract and structured result model are documented in:

- [proto/recall/search/v1/search.proto](proto/recall/search/v1/search.proto)
- [docs/recall-compatible-search.md](docs/recall-compatible-search.md)

## Development

Use the Justfile wrappers:

```bash
just build
just test
just lint
```
