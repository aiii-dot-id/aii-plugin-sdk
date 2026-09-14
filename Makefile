# aii-plugin-sdk — build and proof targets.
#
# `make test` is hermetic: Go alone. `make e2e` and `make acceptance`
# prove the kit against a real host and need TinyGo and a host checkout
# (knobs: TINYGO, AII_OS_DIR, AII_OS_GO).

GO ?= go

.PHONY: test e2e acceptance cli clean

test:
	$(GO) vet ./...
	@fmt_out=$$(gofmt -l .); if [ -n "$$fmt_out" ]; then echo "gofmt needed on:"; echo "$$fmt_out"; exit 1; fi
	$(GO) test -race -count=1 ./...

e2e:
	$(GO) test -tags e2e -count=1 -v ./e2e/

acceptance:
	$(GO) test -tags acceptance -count=1 -v ./acceptance/

cli:
	$(GO) build -o .tools/aiisdk ./cmd/aiisdk
	@echo "built .tools/aiisdk"

clean:
	rm -rf .tools examples/memory-skel/memory-skel.wasm
