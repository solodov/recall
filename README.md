# recall

`recall` is a federated personal-search CLI. It loads an operator-owned provider registry, asks each enabled provider to search the same query, normalizes results, blends provider-local ranks, and renders a single response with clickable terminal open targets when available.

Providers implement the versioned protobuf service in `proto/recall/search/v1/search.proto`:

```proto
service SearchProvider {
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc ListCapabilities(ListCapabilitiesRequest) returns (ListCapabilitiesResponse);
}

message SearchRequest {
  string query = 1;
  optional uint32 limit = 2;
  repeated string selector_hints = 3;
}
```

Recall-level selector routing and output format are orchestration or rendering controls. Providers receive `query`, optional `limit`, and advisory provider-local `selector_hints`; recall still applies authoritative selector filtering after provider responses. The selector taxonomy is documented in the proto comments.

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

For the built-in search contract, the paths are:

```text
/recall.search.v1.SearchProvider/Search
/recall.search.v1.SearchProvider/ListCapabilities
```

Providers auto-detect whether stdin is protobuf binary or textproto, then mirror the same format on stdout. `recall` uses protobuf binary for normal provider calls; humans can pipe textproto directly for debugging.

## Build a Go provider

Go provider binaries can use the public SDK package:

```go
package main

import (
	"context"
	"fmt"
	"os"

	recallprovider "github.com/solodov/recall/provider"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

type Provider struct{}

func (Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{{Selector: "note:content", Title: "Notes"}}}, nil
}

func (Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	hits := []*searchv1.SearchHit{{Id: "example:1", Selector: "note:content", Title: request.GetQuery()}}
	if limit, ok := recallprovider.RequestedLimit(request); ok && len(hits) > limit {
		hits = hits[:limit]
	}
	return &searchv1.SearchResponse{Hits: hits}, nil
}

func main() {
	if err := recallprovider.ServeSearch(context.Background(), Provider{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

The SDK handles the stdio RPC path, stdin format auto-detection, mirrored stdout encoding, and dispatch to `Search` or `ListCapabilities`.

## First-party providers

- `recall-example-provider` demonstrates the provider contract with a deterministic fixture.
- `recall-ripgrep-provider` searches local code with ripgrep; see `docs/recall-ripgrep-provider.md`.
- `recall-gh-provider` searches GitHub through `gh`; see `docs/recall-gh-provider.md`.

## Run the example

The example script builds `recall` and the example provider, writes a temporary config that points at the built provider, and runs a search:

```bash
examples/run-example.sh
examples/run-example.sh rollout
examples/run-example.sh --format json rollout
```

The script defaults to the query `rollout` when no query is supplied.

## Pipe textproto directly to a provider

Build the binaries first:

```bash
just build
```

This produces `dist/recall`, `dist/recall-open`, `dist/recall-example-provider`, `dist/recall-ripgrep-provider`, and `dist/recall-gh-provider`.

Then call the example provider directly with a textproto `SearchRequest`:

```bash
printf 'query: "rollout"\n' |
  dist/recall-example-provider /recall.search.v1.SearchProvider/Search
```

Omitting `limit` asks the provider to return every reasonable match. Add `limit` when you want a cap:

```bash
printf 'query: "rollout"\nlimit: 10\n' |
  dist/recall-example-provider /recall.search.v1.SearchProvider/Search
```

Because stdin is textproto, stdout is a textproto `SearchResponse`.

## Provider registry

The default registry path is `$XDG_CONFIG_HOME/recall/config.txtpb`, falling back to `$HOME/.config/recall/config.txtpb`. You can pass a registry explicitly with `--config PATH`.

A provider entry lists one or more transports in preference order:

```textproto
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 10
  transports {
    stdio {
      command: "recall-example-provider"
    }
  }
}
```

Service and method are protocol-owned, so they are not config fields. For stdio providers, `recall` appends the selected `SearchProvider` RPC path at call time.

`openers` are optional local commands used by `recall-open` for OSC 8 `recall://` terminal links.

## Development

Use the Justfile wrappers:

```bash
just build
just test
just lint
```
