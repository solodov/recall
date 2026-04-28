#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

just --justfile "${repo_root}/Justfile" --working-directory "${repo_root}" build

config_dir="$(mktemp -d "${TMPDIR:-/tmp}/recall-example.XXXXXX")"
trap 'rm -rf "${config_dir}"' EXIT
config_path="${config_dir}/config.txtpb"

cat >"${config_path}" <<EOF
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 10
  stdio {
    command: "${repo_root}/dist/recall-example-provider"
  }
}
EOF

if (($# == 0)); then
  set -- deploy
fi

exec "${repo_root}/dist/recall" --config "${config_path}" "$@"
