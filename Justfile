set positional-arguments
set script-interpreter := ["bash", "-euo", "pipefail"]

goimports := require("goimports")
protoc := require("protoc")
protoc_gen_go := require("protoc-gen-go")

generate-proto:
    "{{protoc}}" --go_out=. --go_opt=paths=source_relative proto/recall/config/v1/config.proto proto/recall/rpc/v1/rpc.proto proto/recall/search/v1/search.proto
    "{{goimports}}" -w proto/recall/config/v1/config.pb.go proto/recall/rpc/v1/rpc.pb.go proto/recall/search/v1/search.pb.go

build: generate-proto
    mkdir -p dist
    go build ./...
    go build -o dist/recall ./cmd/recall
    go build -o dist/recall-example-provider ./cmd/recall-example-provider

[script]
lint *paths:
    add_path() {
      local candidate="$1"

      if [[ -d "$candidate" ]]; then
        paths+=("$candidate")
      elif [[ -f "$candidate" && "$candidate" == *.go ]]; then
        paths+=("$candidate")
      elif [[ "$candidate" == . || "$candidate" == "./." ]]; then
        paths+=(".")
      fi
    }

    paths=()
    if (($# == 0)); then
      set -- .
    fi

    for path in "$@"; do
      add_path "$path"
    done

    if ((${#paths[@]} == 0)); then
      exit 0
    fi

    "{{goimports}}" -w "${paths[@]}"

[script]
test *paths:
    add_package() {
      local candidate="$1"
      local package_path

      if [[ "$candidate" == . || "$candidate" == "./." ]]; then
        package_path="./..."
      elif [[ "$candidate" == *"..." ]]; then
        if [[ "$candidate" == ./* ]]; then
          package_path="$candidate"
        else
          package_path="./${candidate#./}"
        fi
      elif [[ -d "$candidate" ]]; then
        candidate="${candidate%/}"
        package_path="./${candidate#./}/..."
      elif [[ -f "$candidate" && "$candidate" == *.go ]]; then
        package_path="./$(dirname "${candidate#./}")"
      elif [[ -f "$candidate" ]]; then
        return
      else
        package_path="$candidate"
      fi

      if [[ "$package_path" == "./." ]]; then
        package_path="."
      fi

      if [[ -z "${seen[$package_path]+x}" ]]; then
        packages+=("$package_path")
        seen["$package_path"]=1
      fi
    }

    declare -A seen=()
    packages=()

    for path in "$@"; do
      add_package "$path"
    done

    if ((${#packages[@]} == 0)); then
      go test ./...
      exit 0
    fi

    go test "${packages[@]}"

run *args: build
    exec dist/recall "$@"

run-bin binary *args:
    binary="$1"; shift; exec go run "./cmd/${binary}" "$@"

install: build
    cp dist/recall $(go env GOPATH)/bin
    cp dist/recall-example-provider $(go env GOPATH)/bin
