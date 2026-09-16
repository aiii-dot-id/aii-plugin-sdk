#!/bin/sh
# Build the native skeleton for the three desktop platforms — the three
# builds ride in ONE signed package, one variant each; this builds the
# artifacts the package declares.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/native-skel-linux-x86_64 .
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/native-skel-macos-arm64 .
GOFLAGS=-buildvcs=false CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/native-skel-windows-x86_64.exe .
echo "built dist/native-skel-{linux-x86_64,macos-arm64,windows-x86_64.exe}"
