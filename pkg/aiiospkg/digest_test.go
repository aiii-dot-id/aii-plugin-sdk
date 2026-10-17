package aiiospkg

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTreeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) error {
	t.Helper()
	return os.Symlink(target, link)
}

// TestPackageHashFormula restates PACKAGE_DIGEST.md §3.3 inline —
// update(relpath + NUL) then update(lowercase-hex(sha256(content)) +
// LF) in sorted path order — and compares against the implementation.
func TestPackageHashFormula(t *testing.T) {
	files := map[string][]byte{
		"b.txt":       []byte("bravo"),
		"a.txt":       []byte("alpha"),
		"nested/c.md": []byte("charlie"),
	}
	agg := sha256.New()
	for _, p := range []string{"a.txt", "b.txt", "nested/c.md"} {
		sum := sha256.Sum256(files[p])
		agg.Write([]byte(p))
		agg.Write([]byte{0})
		agg.Write([]byte(hex.EncodeToString(sum[:])))
		agg.Write([]byte{'\n'})
	}
	want := "sha256:" + hex.EncodeToString(agg.Sum(nil))
	if got := PackageHash(files); got != want {
		t.Fatalf("PackageHash = %s, want %s", got, want)
	}
}

// TestPackageHashOrderTheorem pins the separator-interleaving order —
// "a-c" < "a.d" < "a/b" bytewise — the same theorem the runtime's
// digest order rests on (aii-os packagefmt/digest.go:45-57).
func TestPackageHashOrderTheorem(t *testing.T) {
	files := map[string][]byte{
		"a/b": []byte("3"),
		"a-c": []byte("1"),
		"a.d": []byte("2"),
	}
	agg := sha256.New()
	for _, p := range []string{"a-c", "a.d", "a/b"} {
		sum := sha256.Sum256(files[p])
		agg.Write([]byte(p))
		agg.Write([]byte{0})
		agg.Write([]byte(hex.EncodeToString(sum[:])))
		agg.Write([]byte{'\n'})
	}
	want := "sha256:" + hex.EncodeToString(agg.Sum(nil))
	if got := PackageHash(files); got != want {
		t.Fatalf("order theorem broken: PackageHash = %s, want %s", got, want)
	}
}

// TestManifestHashDropsOnlyTopLevelPackageHash pins §3.5: the view
// omits the TOP-LEVEL package_hash member (nested ones stay), and the
// hash is over canonical bytes.
func TestManifestHashDropsOnlyTopLevelPackageHash(t *testing.T) {
	manifest := []byte(`{
		"kind": "plugin",
		"package_hash": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"nested": {"package_hash": "kept"},
		"id": "x.y"
	}`)
	got, err := ManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	view := []byte(`{"id":"x.y","kind":"plugin","nested":{"package_hash":"kept"}}`)
	if want := SHA256Prefixed(view); got != want {
		t.Fatalf("ManifestHash = %s, want %s (over view %s)", got, want, view)
	}
	// Absent package_hash: the view is the manifest itself.
	got2, err := ManifestHash([]byte(`{"id":"x.y"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := SHA256Prefixed([]byte(`{"id":"x.y"}`)); got2 != want {
		t.Fatalf("ManifestHash without package_hash = %s, want %s", got2, want)
	}
}

func TestManifestHashRejectsNonCanonicalizable(t *testing.T) {
	for _, raw := range []string{
		`{"k":1,"k":2}`, // duplicate member
		`[1,2]`,         // not an object
		`{"n":1e5}`,     // non-canonical number
	} {
		if _, err := ManifestHash([]byte(raw)); err == nil {
			t.Fatalf("ManifestHash(%s) accepted", raw)
		}
	}
}

func TestReadTreeRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "install-root/real.txt", "content")
	if err := symlinkOrSkip(t, dir+"/install-root/real.txt", dir+"/install-root/link.txt"); err != nil {
		t.Skipf("no symlink support: %v", err)
	}
	if _, err := ReadTree(dir); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("ReadTree accepted a symlink: %v", err)
	}
}
