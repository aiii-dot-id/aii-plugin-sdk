package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// WHAT `aiisdk test` RUNS IS WHAT THE PACKAGE CARRIES. It finds a
// variant's bytes as a host does — the staged manifest's entrypoint for
// that variant, held to the hash the manifest binds — and never the
// module 'aiisdk build' left in dist/. The
// end-to-end proof is e2e TestAiisdkTestRunsTheBytesThePackageCarries.
func TestThePackagedArtifactIsTheStagedMemberTheManifestBinds(t *testing.T) {
	dir := t.TempDir()
	cfg := &aiiospkg.AuthorConfig{ID: "twin", Version: "0.1.0"}
	stage := stageDir(dir, cfg)
	member := "install-root/variants/any-wasm/plugin.wasm"
	carried := []byte("\x00asm the variant's own bytes")
	sum := sha256.Sum256(carried)
	bound := "sha256:" + hex.EncodeToString(sum[:])
	write := func(rel string, b []byte) {
		t.Helper()
		p := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", []byte(`{"variants":[{"variant_id":"any-wasm","entrypoint":"variants/any-wasm/plugin.wasm","artifact_hash":"`+bound+`"}]}`))
	write(member, carried)
	// The module the build left in dist/ is another module, and is not it.
	if err := os.WriteFile(wasmPath(dir, cfg), []byte("\x00asm the built module"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := packagedArtifact(dir, cfg, "any-wasm")
	if err != nil {
		t.Fatal(err)
	}
	if got.member != member || got.hash != bound || got.size != len(carried) {
		t.Fatalf("packaged %+v, want %s %s %d bytes", got, member, bound, len(carried))
	}
	if b, err := os.ReadFile(got.path); err != nil || string(b) != string(carried) {
		t.Fatalf("the path named holds %q (%v), not the packaged bytes", b, err)
	}

	if _, err := packagedArtifact(dir, cfg, "other-wasm"); err == nil || !strings.Contains(err.Error(), "declares no variant other-wasm") {
		t.Fatalf("a variant the manifest does not declare must be refused: %v", err)
	}
	// Staged bytes that are not the ones the manifest binds are not run.
	write(member, []byte("\x00asm changed after packaging"))
	if _, err := packagedArtifact(dir, cfg, "any-wasm"); err == nil || !strings.Contains(err.Error(), "the manifest binds") {
		t.Fatalf("staged bytes the manifest does not bind must be refused: %v", err)
	}
}
