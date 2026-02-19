#!/usr/bin/env bash
set -euo pipefail

ACTION_DIR="${GITHUB_ACTION_PATH:-""}"
if [[ -z "$ACTION_DIR" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  ACTION_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

BIN_DIR="${RUNNER_TEMP:-/tmp}/evolver-bin"
mkdir -p "$BIN_DIR"

pushd "$ACTION_DIR" >/dev/null
go build -trimpath -ldflags "-s -w" -o "$BIN_DIR/evolver" ./cmd/evolver
popd >/dev/null

chmod +x "$BIN_DIR/evolver"

if [[ -n "${GITHUB_PATH:-}" ]]; then
  echo "$BIN_DIR" >> "$GITHUB_PATH"
else
  export PATH="$BIN_DIR:$PATH"
  echo "evolver installed to $BIN_DIR (PATH updated for this shell)"
fi
