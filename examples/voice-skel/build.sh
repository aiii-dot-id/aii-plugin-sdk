#!/bin/sh
# Build the speech-engine skeleton for the three desktop platforms — a
# voice interface is native per platform by construction; this builds
# the artifacts the package declares.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/voice-skel-linux-x86_64 .
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/voice-skel-macos-arm64 .
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/voice-skel-windows-x86_64.exe .
echo "built dist/voice-skel-{linux-x86_64,macos-arm64,windows-x86_64.exe}"
