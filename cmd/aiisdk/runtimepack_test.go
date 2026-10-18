package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func TestParseBytesReadsCountsAndBinarySuffixes(t *testing.T) {
	for raw, want := range map[string]int64{"1024": 1024, "2K": 2 << 10, "512M": 512 << 20, "8G": 8 << 30, "8GiB": 8 << 30, "1g": 1 << 30, " 7 ": 7} {
		got, err := parseBytes(raw)
		if err != nil || got != want {
			t.Fatalf("%q = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "0", "-1", "1.5G", "G", "8T", "x"} {
		if _, err := parseBytes(raw); err == nil {
			t.Fatalf("%q must be refused", raw)
		}
	}
}

// runtime-pack -platform windows refuses a tree with an executable file,
// naming it, and leaves no archive behind; without the bit it packs.
func TestRuntimePackForWindowsRefusesAnExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows records no executable bit on a file (Go reports none there), so no tree on Windows carries the mark this refusal is for; that a tree there packs is TestRuntimePackForWindowsPacksATreeWithNoExecutableBit")
	}
	src := t.TempDir()
	worker := filepath.Join(src, "meaning-worker.exe")
	if err := os.WriteFile(worker, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if rc := cmdRuntimePack([]string{"-dir", src, "-o", out, "-platform", "windows"}); rc == 0 {
		t.Fatal("runtime-pack -platform windows packed an executable file")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a refused archive was left behind: %v", err)
	}
	if err := os.Chmod(worker, 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := cmdRuntimePack([]string{"-dir", src, "-o", out, "-platform", "windows"}); rc != 0 {
		t.Fatalf("runtime-pack -platform windows refused a tree without executable bits: %d", rc)
	}
}

// runtime-pack -platform windows packs a tree with no executable bit — the
// bit cleared, or any tree on Windows — and writes the archive.
func TestRuntimePackForWindowsPacksATreeWithNoExecutableBit(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "meaning-worker.exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if rc := cmdRuntimePack([]string{"-dir", src, "-o", out, "-platform", "windows"}); rc != 0 {
		t.Fatalf("runtime-pack -platform windows refused a tree with no executable bit: %d", rc)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("no archive was written: %v", err)
	}
}

// A source rewritten, same length, after the packer hashed it for the
// inventory and before it copies its bytes fails runtime-pack, and the
// failure path removes the archive: none is left whose contents contradict
// its inventory.
func TestRuntimePackLeavesNoArchiveWhenASourceChangesWhilePacking(t *testing.T) {
	src := t.TempDir()
	lib := filepath.Join(src, "lib.so")
	if err := os.WriteFile(lib, []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	pack := writeRuntimeTreeFor
	t.Cleanup(func() { writeRuntimeTreeFor = pack })
	writeRuntimeTreeFor = func(w io.Writer, dir, root, platform string, budget aiiospkg.TreeLimits) (*aiiospkg.RuntimeArchive, error) {
		return pack(&rewriteOnFirstWrite{w: w, path: lib, bytes: []byte("BBBB")}, dir, root, platform, budget)
	}
	out := filepath.Join(t.TempDir(), "runtime.tar.gz")
	if rc := cmdRuntimePack([]string{"-dir", src, "-o", out}); rc == 0 {
		t.Fatal("runtime-pack packed a source that changed between its inventory and its archive")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a refused archive was left behind: %v", err)
	}
}

// rewriteOnFirstWrite rewrites path when the archive's first byte is
// written: after the walk hashed it, before its bytes are copied.
type rewriteOnFirstWrite struct {
	w     io.Writer
	path  string
	bytes []byte
	done  bool
}

func (r *rewriteOnFirstWrite) Write(p []byte) (int, error) {
	if !r.done {
		r.done = true
		if err := os.WriteFile(r.path, r.bytes, 0o644); err != nil {
			return 0, err
		}
	}
	return r.w.Write(p)
}
