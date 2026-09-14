package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// .
// .
func TestGoModTemplateWiresAReleaseOrACheckout(t *testing.T) {
	rel := goModTemplate("com.example.x", "", "v0.3.1")
	if !strings.Contains(rel, "require github.com/aiii-dot-id/aii-plugin-sdk v0.3.1\n") || strings.Contains(rel, "replace ") {
		t.Fatalf("release form:\n%s", rel)
	}
	local := goModTemplate("com.example.x", "/kit", "")
	if !strings.Contains(local, "require github.com/aiii-dot-id/aii-plugin-sdk v0.0.0\n") || !strings.Contains(local, "replace github.com/aiii-dot-id/aii-plugin-sdk => /kit\n") {
		t.Fatalf("checkout form:\n%s", local)
	}
	if !strings.Contains(rel, "\ngo 1.25\n") {
		t.Fatalf("the scaffold's floor is the kit's:\n%s", rel)
	}
}

// .
// .
// .
func TestOnlyATaggedReleaseCountsAsInstalled(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.1.0": true, "v1.2.3": true, "v0.2.0-rc1": true,
		"v0.0.0-20260906112259-52d11f1b376b": false, "v0.1.1-0.20260906112259-52d11f1b376b": false,
		"v0.0.0-20260906112259-52d11f1b376b+dirty": false, "v0.1.0+dirty": false, "(devel)": false, "": false,
	} {
		if got := isReleaseVersion(v); got != want {
			t.Errorf("isReleaseVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

// .
// .
// .
func TestTheBuildGuardLeavesTheKitsOwnExamplesAlone(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	before := fileDigests(t, "../../go.mod", "../../go.sum")
	if err := ensureModules("../../examples/logging"); err != nil {
		t.Fatalf("an example inside the kit needs nothing: %v", err)
	}
	if err := ensureModules(t.TempDir()); err != nil {
		t.Fatalf("a directory with no module is the toolchain's to refuse, not the guard's: %v", err)
	}
	if after := fileDigests(t, "../../go.mod", "../../go.sum"); after != before {
		t.Fatal("the guard touched the kit's own module files")
	}
}

func fileDigests(t *testing.T, paths ...string) string {
	t.Helper()
	var out strings.Builder
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "%s:%x\n", p, sha256.Sum256(raw))
	}
	return out.String()
}

// .
// .
// .
func TestGoModReplaceQuotesAPathThatNeedsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list; skipped in -short")
	}
	kit := filepath.Join(t.TempDir(), "kit dir")
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kit, "go.mod"), []byte("module github.com/aiii-dot-id/aii-plugin-sdk\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := goModTemplate("com.example.spaced", kit, "")
	if !strings.Contains(mod, "replace github.com/aiii-dot-id/aii-plugin-sdk => "+strconv.Quote(kit)+"\n") {
		t.Fatalf("a path with a space was written bare:\n%s", mod)
	}
	if bare := goModTemplate("com.example.plain", "/kit", ""); !strings.Contains(bare, "=> /kit\n") {
		t.Fatalf("a plain path was quoted for no reason:\n%s", bare)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Replace.Dir}}", "github.com/aiii-dot-id/aii-plugin-sdk")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go could not read the scaffold's go.mod: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != kit {
		t.Fatalf("go resolved the replacement to %q, want %q", got, kit)
	}
}
