package aiiospkg

import (
	"os"
	"path/filepath"
	"testing"
)

// .
// .
// .
// .
func TestReadTreeTakesNoModeFromTheDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "com.example.x-1.0.0")
	if err := os.MkdirAll(filepath.Join(dir, "install-root"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-root", "plugin"), []byte("bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	tree, err := ReadTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Exec["install-root/plugin"] {
		t.Fatal("an executable bit on the staging disk reached the tree")
	}
}
