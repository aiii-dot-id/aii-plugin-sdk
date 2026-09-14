# aii-plugin-sdk

The Go kit for writing AII OS plugins: a guest library, a packager, and
one command-line tool.

A plugin is a set of Go handlers compiled to a WASM module and shipped
as a `.aiiospkg` bundle. Any AII OS host verifies the bundle and runs the
module in a sandbox. What the plugin may do is declared in its package
and granted by the operator, never assumed.

## Requirements

- Go 1.25 or newer.
- [TinyGo](https://tinygo.org) 0.42 or newer, to compile the WASM module.
- To verify and run a plugin locally, the host's `aii` and
  `aii-plugin-worker` on your PATH (or in the directory `AII_OS_BIN`
  names). Writing, building and packaging need neither.

## Quick start

    go install github.com/aiii-dot-id/aii-plugin-sdk/cmd/aiisdk@latest
    aiisdk init com.example.hello && cd com.example.hello
    aiisdk build && aiisdk package    # dist/com.example.hello-0.1.0.aiiospkg, unsigned (T0)
    aiisdk test                       # verify and run it on the host binaries; drive tests/*.json
    aiisdk devcert && aiisdk sign     # sign it with a local dev chain (T1)

From a checkout instead of a release: `go build -o ~/bin/aiisdk
./cmd/aiisdk`, then pass `-sdk <path to the checkout>` to `aiisdk init`
(or set `AII_SDK_DIR`); the scaffold is wired to it with a `replace`.

Then read [].

## Trust tiers

| Tier | Package | Signed by |
|---|---|---|
| T0 | WASM, unsigned | nobody: development only |
| T1 | WASM, publisher-signed, the publisher certified by a root the host pins | the publisher: a local dev chain from `aiisdk devcert`, or an AIII certifier for release |
| T2 | T1 plus a reviewer's attestation of the capabilities; credentials require it | the AIII reviewer |
| T3 | platform-signed; the only tier that runs native code | AIII only |

The kit produces T0 and T1 packages. T2 and T3 are the platform's acts.

## Commands

| | |
|---|---|
| `aiisdk init <id>` | scaffold a plugin |
| `aiisdk build` | compile the WASM module |
| `aiisdk package` | write the canonical `.aiiospkg` (T0) |
| `aiisdk test` | build, package, verify and run it on the host binaries |
| `aiisdk devcert` | mint a local dev signing chain, once per machine |
| `aiisdk sign` | sign the package (T1) |
| `aiisdk revoke` | revoke a signed release in the dev chain |
| `aiisdk publish` | print the package's catalog entry |

`aiisdk <command> -h` documents each.

## Layout

- `pkg/aiiosdk` — the guest library. Start at its package documentation.
- `pkg/aiiospkg` — the packager and the dev signing chain.
- `cmd/aiisdk` — the command-line tool.
- `examples/` — ten plugins, each with a README: the bare guest, two
  memories, a GitHub connector, a document plugin, an SMS webhook
  adapter, an event logger, an MCP bridge, and two native skeletons.
- `vectors/` — conformance vectors shared with the host.
- `e2e/`, `acceptance/` — proofs against a host checkout, for
 maintainers.

`make test` runs the unit suite and needs only Go.

## License

Apache-2.0. See [LICENSE](LICENSE).
