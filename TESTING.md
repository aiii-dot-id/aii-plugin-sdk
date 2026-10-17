# Testing

The repository carries the unit tests, synthetic fixtures and host integration
checks for this SDK. No account credentials or live model/provider requests are
needed for the commands below. Tests use temporary directories, local loopback
servers and subprocesses. Dependency downloads may be needed on the first run.

## Unit suite

Use Go 1.25 or newer and a C toolchain supported by the Go race detector:

```sh
go build ./...
go vet ./...
go test -race -count=1 -timeout=15m ./...
```

`make test` also checks `gofmt`. CI runs the same checks on Linux, macOS and
Windows. Set `GOMAXPROCS=4` and `GOFLAGS=-p=4` to bound build concurrency.

## Host integration and authoring acceptance

Use a matching AII OS source checkout containing its tests and shared fixtures,
its required Go toolchain, and TinyGo 0.42.0. The host and SDK must carry identical
shared vector files; the e2e suite verifies all twelve copies byte for byte.

```sh
export AII_OS_DIR=/path/to/aii-os
export AII_OS_GO=/path/to/host-go/bin
export TINYGO=/path/to/tinygo

go test -race -tags e2e -count=1 -timeout=15m ./e2e/
go test -race -tags acceptance -count=1 -timeout=15m ./acceptance/
```

The e2e suite builds the actual host worker and verifier without source patches.
It checks guest framing, signing, tamper refusals, packaged variant selection and
shared vectors. Acceptance clones the **committed SDK HEAD** into a temporary
directory, builds the CLI and examples, and exercises the full authoring path.
Commit the source being tested before running acceptance. `ACCEPT_SRC_REPO` can
select a separate committed SDK checkout explicitly.

These lanes skip when their host or TinyGo prerequisites are absent. A skipped
lane is incomplete coverage, not a passing qualification. Capture JSON results
with `-json` to distinguish passed, failed and skipped tests.

## Scope and fixtures

The example service tests simulate responses, including malformed data, partial
outcomes, retries and refusals; they do not qualify live Gmail, calendar, SMS or
other external services. Audio fixtures use synthetic samples and do not require
microphones, speakers or model downloads. Unit coverage does not replace native
platform install, update or signing checks.

Filesystem tests report platform-specific skips where permissions or links are
unsupported. Some Windows directory-mounted volumes and junction layouts can be
refused by path containment checks; use an ordinary directory for authoring and
record the environment rather than interpreting an unavailable check as success.
