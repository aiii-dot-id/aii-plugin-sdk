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

// .
// .
// .
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

// .
// .
// .
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

// .
// .
// .
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
	// .
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
		`{"k":1,"k":2}`,
		`[1,2]`,
		`{"n":1e5}`,
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
