#!/bin/sh
# Build the twilio-sms guest — the one-line TinyGo build.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
"${TINYGO:-tinygo}" build -o dist/com.aiii.examples.twilio-sms.wasm -target=wasm-unknown -scheduler=none -gc=conservative -no-debug .
echo "built dist/com.aiii.examples.twilio-sms.wasm"
