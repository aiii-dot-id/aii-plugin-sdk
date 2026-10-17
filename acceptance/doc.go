// Package acceptance is the capstone proof: from a fresh clone of this
// kit, an author reaches a plugin a host VERIFIES and RUNS, using only
// the kit and the host's binaries.
//
// It is build-tagged (acceptance) so plain `go test ./...` stays
// hermetic: it needs a clone, TinyGo, and a host checkout with its Go.
// Run it with `make acceptance`; it skips, naming what is missing, and
// never fakes a pass.
//
// The proofs it carries: the T0 path (init, build, package, verify, run,
// a clean exit); the T1 path (a dev chain minted, the release signed,
// verified against the pinned dev root, then revoked and refused);
// every example built and tested under its grants; and the native
// matrix packaged and refused below T3. The signing proof is also the
// cross-implementation proof: the envelopes this kit produces verify
// against the host's own verifiers.
package acceptance
