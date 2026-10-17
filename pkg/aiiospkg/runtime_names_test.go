package aiiospkg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// The names a frozen Python runtime carries pack; what the grammar
// refuses is refused by name at pack time (the host's component rule,
// mirrored).
func TestWriteRuntimeTreeAdmitsAPythonRuntimesNames(t *testing.T) {
	src := t.TempDir()
	for _, rel := range []string{
		"lib/python3.12/site-packages/pkg/__init__.py", "lib/python3.12/__pycache__/os.cpython-312.pyc",
		"lib/libstdc++.so.6", "lib/PIL/.dylibs/libtiff.6.dylib", "lib/numpy-2.1.0.dist-info/RECORD", "bin/python3",
		// The names the runtime trees carry and read at import.
		"python/lib/python3.11/site-packages/scipy/io/tests/data/Transparent Busy.ani",
		"python/lib/python3.11/site-packages/setuptools/launcher manifest.xml",
		"python/lib/python3.11/site-packages/setuptools/script (dev).tmpl",
		"python/lib/python3.11/site-packages/setuptools/_vendor/jaraco/text/Lorem ipsum.txt",
		"vendor/pocket/source/docs/API Reference/Reference/tts_model.md",
		"vendor/pocket/source/docs/CLI Commands/serve.md",
	} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := WriteRuntimeTree(&buf, src, "cp1"); err != nil {
		t.Fatalf("the names a frozen Python runtime carries must pack: %v", err)
	}
	for _, bad := range []string{"lib/ leading.txt", "lib/trailing ", "lib/nul.txt", "lib/x.", "lib/café.py", "lib/a\tb.txt", "lib/a{b}.txt"} {
		t.Run(bad, func(t *testing.T) {
			src := t.TempDir()
			p := filepath.Join(src, filepath.FromSlash(bad))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			writeNameVerbatim(t, p)
			var b bytes.Buffer
			if _, err := WriteRuntimeTree(&b, src, "cp1"); err == nil || !strings.Contains(err.Error(), "not admitted") {
				t.Fatalf("%q must be refused by name: %v", bad, err)
			}
		})
	}
}

// errorInvalidName is Windows' ERROR_INVALID_NAME: the file system refuses
// the name itself.
const errorInvalidName = syscall.Errno(123)

// writeNameVerbatim creates the file p under exactly its name. On Windows
// an ordinary path is rewritten before it reaches the file system — a
// trailing space or dot dropped, "nul.txt" sent to the NUL device — so the
// tree would not hold the name under test; the extended-length form
// (\\?\) passes the name through as written, which is how a tree a tool
// extracts verbatim holds it, and is the case the walker meets there. It
// is removed the same way, before the temporary directory is. A name the
// file system itself cannot hold is one no tree there carries.
func writeNameVerbatim(t *testing.T, p string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		p = `\\?\` + p
		t.Cleanup(func() { os.Remove(p) })
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, errorInvalidName) {
			t.Skipf("the file system refuses the name itself, so no tree on Windows carries it: %v", err)
		}
		t.Fatal(err)
	}
}
