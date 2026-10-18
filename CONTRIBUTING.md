# Contributing

`make test` runs the local checks: `go build`, `go vet`, `gofmt`, and the unit
suite under the race detector. It needs Go 1.25 or newer and a C toolchain
supported by the race detector; dependencies may be downloaded on the first run.

Two further targets prove the kit against a real AII OS host, and need one:

- `make e2e` builds a guest with TinyGo and proves a dev-signed package
  against the host's `aii plugin verify`.
- `make acceptance` runs the whole authoring path from a fresh clone.

Both take the host checkout from `AII_OS_DIR` (default: a sibling
`../aii-os`), its Go **bin directory** from `AII_OS_GO` (for example,
`/opt/go-current/bin`; default: the directory containing `go` on PATH) and
TinyGo from `TINYGO` (default: `tinygo` on PATH), and skip with a clear
message when one is missing. A skip is not a pass.

The platform-attachment proof needs an already built host verifier, not
production keys. It generates a throwaway root and signed status, verifies
WASM and native T3 packages, and proves actual signature/layout rejection:

```sh
AIISDK_PLATFORM_HOST_PROOF=1 AII_OS_BIN=/path/to/host/bin \
  go test -race -count=1 ./cmd/aiisdk -run '^TestPlatformAttachmentWithRealHost$' -v
```

Without the opt-in it reports a skip; the hermetic command-adapter tests
remain separate and never claim cryptographic admission.

`make release-test` additionally runs the checksum verifier's Python 3.11+
parser and tool-boundary tests without keys, signing tools or a network. The
retained signed-catalog proof is separate from these synthetic boundary tests.

Every change lands with its tests. The guest library stays free of
third-party imports and of reflection, because TinyGo's wasm-unknown
target cannot run it; the packager's one dependency is `cloudflare/circl`.

See [TESTING.md](TESTING.md) for prerequisites, reproducible commands and test boundaries.
