// Package e2e proves a kit-built guest against the host's binaries: the
// worker runs the guest, and the verifier proves a dev-signed package to
// T1. Nothing here imports the host; the contract is bytes on stdio.
//
// The suite is opt-in so `go test ./...` stays hermetic:
//
//	go test -tags e2e -count=1 ./e2e/
//
// or `make e2e`. Without TinyGo or a host checkout it skips, naming what
// is missing; it never fakes a pass. One test runs without the tag:
// TestEveryVectorIsDeclared reads only this repository.
package e2e
