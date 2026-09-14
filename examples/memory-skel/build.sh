#!/bin/sh
# Build memory-skel.wasm — the one-line guest build.
#
# TINYGO overrides the compiler path (default: tinygo on PATH). TinyGo
# must drive a host Go it accepts: 0.42 takes Go 1.25 through 1.27.
#
# Flags, each earned:
#   -target=wasm-unknown   the bare core-wasm target: no WASI — the
#                          worker's import wall admits aiii:bbb/bbb
#                          and nothing else
#   -scheduler=none        one plugin, one thread, host-driven entry
#   -gc=conservative       long-lived plugins must not leak; the
#                          collector is non-moving so pinned pointers
#                          handed to the host stay valid
#   -no-debug              strip DWARF; the artifact ships
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
