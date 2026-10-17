package aiiospkg

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A WINDOWS RUNTIME LISTS NO FILE AS "exec" (2026-10-08). Windows keeps no
// executable mark on a file, so the host never matches an "exec" row there
// and refuses the runtime after downloading it whole: a candidate of the
// meaning plugin, packed on a Mac with its worker's bit set, was. Packed
// for Windows, a tree with a file whose executable bit is set is refused
// here, before a byte is written, naming the file and the fix; with the
// bits cleared it packs, every row "file". Packed for any other platform,
// or for none named, the bits are kept as they always were.
func TestAWindowsRuntimeListsNoFileAsExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows records no executable bit on a file (Go reports none there), so no tree on Windows carries the mark this refusal is for, nor bits to keep; what a tree there packs as is TestAWindowsRuntimeWithoutExecutableBitsListsEveryFileAsFile")
	}
	src := t.TempDir()
	for rel, mode := range map[string]os.FileMode{"meaning-worker.exe": 0o755, "onnxruntime.dll": 0o644, "lib/helper.exe": 0o755} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), mode); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	_, err := WriteRuntimeTreeFor(&buf, src, "runtime", "windows", TreeLimits{})
	if err == nil {
		t.Fatal("a Windows runtime with an executable file was packed")
	}
	for _, said := range []string{"lib/helper.exe and 1 more file", "Windows", `"exec"`, "chmod a-x", "pack again", `"file"`} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("the refusal does not say %q: %v", said, err)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("%d bytes were written before the refusal", buf.Len())
	}

	modes := func(platform string) map[string]string {
		t.Helper()
		var out bytes.Buffer
		if _, err := WriteRuntimeTreeFor(&out, src, "runtime", platform, TreeLimits{}); err != nil {
			t.Fatalf("packed for %q: %v", platform, err)
		}
		got := map[string]string{}
		for _, e := range inventoryOf(t, out.Bytes()).Files {
			got[e.Path] = e.Mode
		}
		return got
	}
	for _, platform := range []string{"", "linux", "macos"} {
		if got := modes(platform); got["meaning-worker.exe"] != "exec" || got["onnxruntime.dll"] != "file" {
			t.Fatalf("packed for %q, the bits are kept: %v", platform, got)
		}
	}
	for _, rel := range []string{"meaning-worker.exe", "lib/helper.exe"} {
		if err := os.Chmod(filepath.Join(src, filepath.FromSlash(rel)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, mode := range modes("windows") {
		if mode != "file" {
			t.Fatalf("%s is %q in a Windows runtime", rel, mode)
		}
	}
	if _, err := WriteRuntimeTreeFor(&buf, src, "runtime", "win64", TreeLimits{}); err == nil || !strings.Contains(err.Error(), "win64") {
		t.Fatalf("a platform the manifest does not name: %v", err)
	}
}

// A TREE WITH NO EXECUTABLE BIT PACKS FOR WINDOWS, every row "file" —
// the bits cleared on a machine that keeps them, and every tree on Windows,
// which keeps none. A platform the manifest does not name is refused
// wherever the tree is.
func TestAWindowsRuntimeWithoutExecutableBitsListsEveryFileAsFile(t *testing.T) {
	src := t.TempDir()
	for _, rel := range []string{"meaning-worker.exe", "onnxruntime.dll", "lib/helper.exe"} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if _, err := WriteRuntimeTreeFor(&out, src, "runtime", "windows", TreeLimits{}); err != nil {
		t.Fatalf("a Windows runtime with no executable bit was refused: %v", err)
	}
	files := inventoryOf(t, out.Bytes()).Files
	if len(files) != 3 {
		t.Fatalf("inventory %+v, want the three files", files)
	}
	for _, e := range files {
		if e.Mode != "file" {
			t.Fatalf("%s is %q in a Windows runtime", e.Path, e.Mode)
		}
	}
	if _, err := WriteRuntimeTreeFor(&bytes.Buffer{}, src, "runtime", "win64", TreeLimits{}); err == nil || !strings.Contains(err.Error(), "win64") {
		t.Fatalf("a platform the manifest does not name: %v", err)
	}
}

// inventoryOf reads the inventory an archive leads with: after the root's
// header comes the inventory's, and its bytes follow it.
func inventoryOf(t *testing.T, archive []byte) Inventory {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	hdr := raw[512:1024]
	size := octalFieldOf(t, hdr[124:136])
	var inv Inventory
	if err := json.Unmarshal(raw[1024:1024+size], &inv); err != nil {
		t.Fatal(err)
	}
	return inv
}
