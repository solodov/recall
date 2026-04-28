# recall

`recall` is a federated personal-search CLI. It loads an operator-owned provider registry, asks each enabled provider to search the same query, normalizes results, blends provider-local ranks, and renders a single response.

Providers implement the versioned protobuf service in `proto/recall/search/v1/search.proto`:

```proto
service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;
  optional uint32 limit = 2;
}
```

Recall-level flags such as `--source`, `--kind`, and `--grouped` are orchestration or rendering controls. Providers receive only `query` and optional `limit`; when `limit` is absent, providers should return every reasonable match.

## Stdio provider protocol

A stdio provider is a one-shot process. For each RPC call, `recall` runs:

```bash
provider [provider args] /recall.search.v1.SearchProvider/Search
```

The request is read from stdin and the response is written to stdout. Stderr is reserved for diagnostics.

The final argument is the RPC path:

```text
/<protobuf service>/<method>
```

For the built-in search contract, that path is:

```text
/recall.search.v1.SearchProvider/Search
```

Providers auto-detect whether stdin is protobuf binary or textproto, then mirror the same format on stdout. `recall` uses protobuf binary for normal provider calls; humans can pipe textproto directly for debugging.

## Run the example

The example script builds `recall` and the example provider, writes a temporary config that points at the built provider, and runs a search:

```bash
examples/run-example.sh
examples/run-example.sh deploy
examples/run-example.sh --format json deploy
```

The script defaults to the query `deploy` when no query is supplied.

## Pipe textproto directly to a provider

Build the binaries first:

```bash
just build
```

Then call the example provider directly with a textproto `SearchRequest`:

```bash
printf 'query: "deploy"\n' |
  dist/recall-example-provider /recall.search.v1.SearchProvider/Search
```

Omitting `limit` asks the provider to return every reasonable match. Add `limit` when you want a cap:

```bash
printf 'query: "deploy"\nlimit: 10\n' |
  dist/recall-example-provider /recall.search.v1.SearchProvider/Search
```

Because stdin is textproto, stdout is a textproto `SearchResponse`.

## Provider registry

The default registry path is `$XDG_CONFIG_HOME/recall/config.txtpb`, falling back to `$HOME/.config/recall/config.txtpb`. You can pass a registry explicitly with `--config PATH`.

A stdio provider entry declares process execution only:

```textproto
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 10
  stdio {
    command: "recall-example-provider"
  }
}
```

Service and method are protocol-owned, so they are not config fields. For stdio providers, `recall` appends `/recall.search.v1.SearchProvider/Search` at call time.

## Development

Use the Justfile wrappers:

```bash
just build
just test
just lint
```
