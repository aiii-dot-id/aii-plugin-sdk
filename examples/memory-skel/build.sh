#!/bin/sh
set -eu
cd "$(dirname "$0")"

TINYGO="${TINYGO:-}"
if [ -z "$TINYGO" ]; then
  if command -v tinygo >/dev/null 2>&1; then TINYGO=tinygo
  else
    echo "build.sh: tinygo not found (set TINYGO=/path/to/tinygo)" >&2
    exit 1
  fi
fi

GOFLAGS=-buildvcs=false "$TINYGO" build -o memory-skel.wasm \
  -target=wasm-unknown -scheduler=none -gc=conservative -no-debug .
ls -l memory-skel.wasm
