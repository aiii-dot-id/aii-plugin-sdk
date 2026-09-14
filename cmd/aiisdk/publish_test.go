package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func TestBuildCatalogEntryPortableWasm(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.mem-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("PACKAGE-BYTES")
	if err := os.WriteFile(pkg, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	e, err := buildCatalogEntry(cfg, "", dir, "https://h/mem.aiiospkg", "T2", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "com.x.mem" || e.Version != "1.0.0" || e.Tier != "T2" || e.Summary != "Mem" {
		t.Fatalf("entry meta wrong: %+v", e)
	}
	if len(e.Packages) != 1 || e.Packages[0].Platform != "*" || e.Packages[0].Arch != "*" {
		t.Fatalf("a WASM plugin publishes one portable package: %+v", e.Packages)
	}
	sum := sha256.Sum256(body)
	if e.Packages[0].SHA256 != "sha256:"+hex.EncodeToString(sum[:]) || e.Packages[0].Size != int64(len(body)) {
		t.Fatalf("hash/size wrong: %+v", e.Packages[0])
	}
	if e.Packages[0].URL != "https://h/mem.aiiospkg" {
		t.Fatalf("url: %s", e.Packages[0].URL)
	}
}

func TestBuildCatalogEntryNativePerPlatform(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "p.aiiospkg")
	if err := os.WriteFile(pkg, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "2.0.0", Description: "Engine",
		Variants: []aiiospkg.AuthorVariant{
			{Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
			{Platform: "macos", Arch: "arm64", ExecutionRuntime: "native_t3_component"},
			{Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
		}}
	e, err := buildCatalogEntry(cfg, pkg, dir, "https://h/p.aiiospkg", "T3", "override")
	if err != nil {
		t.Fatal(err)
	}
	if e.Summary != "override" {
		t.Fatalf("explicit summary wins: %q", e.Summary)
	}
	if len(e.Packages) != 2 {
		t.Fatalf("a native plugin publishes one package per distinct platform: %+v", e.Packages)
	}
	if e.Packages[0].URL != e.Packages[1].URL || e.Packages[0].SHA256 != e.Packages[1].SHA256 {
		t.Fatalf("both platforms point at the same archive: %+v", e.Packages)
	}
}

// .
// .
// .
func TestBuildCatalogEntryRefusesAnIncoherentTier(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.mem-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, []byte("PACKAGE-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	wasm := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	if _, err := buildCatalogEntry(wasm, "", dir, "https://h/mem.aiiospkg", "T3", ""); err == nil {
		t.Fatal("a WASM package must not publish as T3")
	}
	native := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"}}}
	if _, err := buildCatalogEntry(native, "", dir, "https://h/mem.aiiospkg", "T1", ""); err == nil {
		t.Fatal("a package with a native variant must not publish below T3")
	}
	if _, err := buildCatalogEntry(native, "", dir, "https://h/mem.aiiospkg", "T3", ""); err != nil {
		t.Fatalf("a native package at T3: %v", err)
	}
}
