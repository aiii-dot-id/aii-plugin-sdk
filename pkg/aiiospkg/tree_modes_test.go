package aiiospkg

import (
	"os"
	"path/filepath"
	"testing"
)

// THE SIGNED BYTES DO NOT DEPEND ON THE DISK. A staged tree read back for
// signing carries the modes `package` chose, not the ones the filesystem
// reports: on a mount that marks every file executable the archive — and
// the catalog hash printed for it — must be the same as anywhere else.
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
