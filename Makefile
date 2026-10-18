# aii-plugin-sdk — build and proof targets.
#
# `make test` uses Go and its race-detector C toolchain. The e2e and acceptance targets
# prove the kit against a real host and need TinyGo and a host checkout
# (knobs: TINYGO, AII_OS_DIR, AII_OS_GO — see CONTRIBUTING.md).

GO ?= go
PYTHON ?= python3

.PHONY: test release-test e2e acceptance cli clean

test:
	$(GO) build ./...
	$(GO) vet ./...
	@fmt_out=$$(gofmt -l .); if [ -n "$$fmt_out" ]; then echo "gofmt needed on:"; echo "$$fmt_out"; exit 1; fi
	$(GO) test -race -count=1 ./...

release-test:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) scripts/release/test_verify_sdk_release.py -v

e2e:
	$(GO) test -tags e2e -count=1 -v ./e2e/

acceptance:
	$(GO) test -tags acceptance -count=1 -v ./acceptance/

cli:
	$(GO) build -o .tools/aiisdk ./cmd/aiisdk
	@echo "built .tools/aiisdk"

clean:
	rm -rf .tools examples/memory-skel/memory-skel.wasm
