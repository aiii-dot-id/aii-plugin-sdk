package aiiospkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The runtime archive writer: a directory becomes the canonical
// archive led by its inventory, exec bits kept, symlinks refused, the
// declaration's numbers reported; the declaration is held to the
// host's bounds.
func TestWriteRuntimeTreeLeadsWithTheInventory(t *testing.T) {
	src := t.TempDir()
	files := map[string][]byte{"bin/carrier": []byte("#!c\n"), "lib/a.so": bytes.Repeat([]byte("a"), 3000), "python/pyvenv.cfg": []byte("home\n")}
	for rel, c := range files {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if rel == "bin/carrier" {
			mode = 0o755
		}
		if err := os.WriteFile(p, c, mode); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	rep, err := WriteRuntimeTree(&buf, src, "cp1")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 3 || rep.InstalledBytes != 4+3000+5 || rep.Size != int64(buf.Len()) || len(rep.SHA256) != 64 || len(rep.InventorySHA256) != 64 {
		t.Fatalf("report %+v", rep)
	}
	// Read the members back: the root, then the inventory, then the rest.
	zr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for off := 0; off+512 <= len(raw); {
		hdr := raw[off : off+512]
		if bytes.Equal(hdr, make([]byte, 512)) {
			break
		}
		name := strings.TrimRight(string(hdr[:100]), "\x00")
		size := octalFieldOf(t, hdr[124:136])
		names = append(names, name)
		off += 512 + int((size+511)/512)*512
	}
	if len(names) < 2 || names[0] != "cp1/" || names[1] != "cp1/"+InventoryFile {
		t.Fatalf("members = %v: the root and then the inventory must lead", names)
	}
	// The inventory names every file with its mode.
	invStart := bytes.Index(raw, []byte(`{"files"`))
	if invStart < 0 {
		t.Fatal("no inventory")
	}
	var inv Inventory
	end := bytes.IndexByte(raw[invStart:], 0)
	if err := json.Unmarshal(raw[invStart:invStart+end], &inv); err != nil {
		t.Fatal(err)
	}
	// The carrier's bit is kept where the file system records one; Windows
	// records none, so there the carrier is a "file" like the rest.
	carrier := "exec"
	if runtime.GOOS == "windows" {
		carrier = "file"
	}
	if len(inv.Files) != 3 || inv.Files[0].Path != "bin/carrier" || inv.Files[0].Mode != carrier || inv.Files[1].Mode != "file" {
		t.Fatalf("inventory %+v, want bin/carrier %q", inv, carrier)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(src, "lib", "escape")); err == nil {
		if _, err := WriteRuntimeTree(&bytes.Buffer{}, src, "cp1"); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("a symlink must be refused: %v", err)
		}
	}
}

func octalFieldOf(t *testing.T, field []byte) int64 {
	t.Helper()
	s := strings.TrimRight(strings.TrimSpace(string(field)), "\x00")
	s = strings.TrimSpace(s)
	var n int64
	for _, c := range s {
		if c < '0' || c > '7' {
			break
		}
		n = n*8 + int64(c-'0')
	}
	return n
}

func TestValidateRuntimesHoldsTheHostsBounds(t *testing.T) {
	variants := []AuthorVariant{{VariantID: "win"}}
	good := RuntimeDecl{VariantID: "win", URL: "https://x/y.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1, InstalledBytes: 2, Files: 3, InventorySHA256: strings.Repeat("b", 64)}
	if err := ValidateRuntimes([]RuntimeDecl{good}, variants); err != nil {
		t.Fatalf("good: %v", err)
	}
	bad := []struct {
		name string
		fn   func(*RuntimeDecl)
	}{
		{"unknown variant", func(d *RuntimeDecl) { d.VariantID = "mac" }},
		{"http", func(d *RuntimeDecl) { d.URL = "http://x" }},
		{"digest", func(d *RuntimeDecl) { d.SHA256 = "zz" }},
		{"size", func(d *RuntimeDecl) { d.Size = 0 }},
	}
	for _, c := range bad {
		d := good
		c.fn(&d)
		if err := ValidateRuntimes([]RuntimeDecl{d}, variants); err == nil {
			t.Fatalf("%s must be refused", c.name)
		}
	}
	if err := ValidateRuntimes([]RuntimeDecl{good, good}, variants); err == nil {
		t.Fatal("a variant declared twice must be refused")
	}
	// Past the host's defaults is the operator's to admit, not the
	// package's to refuse: the declaration stands and names its needs.
	// Only files and depth have a default; byte extents need no ceiling raised.
	over := good
	largest, depth := int64(600<<20), RuntimeLimits.MaxDepth+1
	over.InstalledBytes, over.Files, over.Size = 2<<30, RuntimeLimits.MaxFiles+1, 1<<30
	over.LargestFileBytes, over.Depth = &largest, &depth
	if err := ValidateRuntimes([]RuntimeDecl{over}, variants); err != nil {
		t.Fatalf("a declaration past the defaults must stand: %v", err)
	}
	req := DeclaredCeilings(over)
	if len(req) != 2 || req[CeilingFiles] != int64(over.Files) || req[CeilingDepth] != int64(depth) {
		t.Fatalf("declared ceilings = %v, want %s and %s alone", req, CeilingFiles, CeilingDepth)
	}
	if len(DeclaredCeilings(good)) != 0 {
		t.Fatal("a declaration within the defaults needs nothing")
	}
	if f := FormatCeilings(req); !strings.HasPrefix(f, CeilingDepth+" >= ") || !strings.Contains(f, ", "+CeilingFiles+" >= ") {
		t.Fatalf("format = %q", f)
	}
	if raw, err := RuntimesJSON([]RuntimeDecl{good}); err != nil || !strings.Contains(string(raw), `"runtimes"`) {
		t.Fatalf("json: %v %s", err, raw)
	}
}

// A budget is the author's declared requirement: a tree past it is refused
// naming the flag; a tree within it is packed, and the report names the
// operator ceilings it needs beyond the host's defaults.
func TestWriteRuntimeTreeWithinABudget(t *testing.T) {
	src := t.TempDir()
	for rel, c := range map[string][]byte{"bin/carrier": []byte("#!c\n"), "lib/a.so": bytes.Repeat([]byte("a"), 3000), "python/site/pkg/mod.py": []byte("x = 1\n")} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, c, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		budget TreeLimits
		flag   string
	}{
		"files":      {TreeLimits{MaxFiles: 2}, "-max-files"},
		"installed":  {TreeLimits{MaxInstalledBytes: 100}, "-max-installed-bytes"},
		"file":       {TreeLimits{MaxFileBytes: 1000}, "-max-file-bytes"},
		"compressed": {TreeLimits{MaxCompressedBytes: 10}, "-max-compressed-bytes"},
		"depth":      {TreeLimits{MaxDepth: 2}, "-max-depth"},
	} {
		_, err := WriteRuntimeTreeWithin(&bytes.Buffer{}, src, "cp1", tc.budget)
		if err == nil || !strings.Contains(err.Error(), tc.flag) {
			t.Fatalf("%s: want a refusal naming %s, got %v", name, tc.flag, err)
		}
	}
	var buf bytes.Buffer
	rep, err := WriteRuntimeTreeWithin(&buf, src, "cp1", TreeLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 3 || rep.LargestFileBytes != 3000 || rep.Depth != 4 || len(rep.RequiredCeilings()) != 0 {
		t.Fatalf("report %+v needs %v", rep, rep.RequiredCeilings())
	}
	// No byte ceiling has a default: an archive asks the operator only
	// for the files and depth it needs.
	big := RuntimeArchive{InstalledBytes: 8 << 30, Files: 40000, LargestFileBytes: 1 << 30, Size: 4 << 30, Depth: 30}
	req := big.RequiredCeilings()
	want := map[string]int64{CeilingFiles: 40000, CeilingDepth: 30}
	if len(req) != len(want) {
		t.Fatalf("required = %v", req)
	}
	for k, v := range want {
		if req[k] != v {
			t.Fatalf("required[%s] = %d, want %d", k, req[k], v)
		}
	}
}

// A source rewritten, same length, after the walk hashed it for the
// inventory and before its bytes are copied is refused, naming it: the
// archive's contents must be what its inventory says. The rewrite lands on
// the output's first write, between the two; rewriting the same bytes is
// the control, packed with its inventory matching its payload.
func TestWriteRuntimeTreeRefusesASourceChangedWhilePacking(t *testing.T) {
	for _, rewrite := range []string{"AAAA", "BBBB"} {
		src := t.TempDir()
		lib := filepath.Join(src, "lib.so")
		if err := os.WriteFile(lib, []byte("AAAA"), 0o644); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		rep, err := WriteRuntimeTree(&rewriteOnFirstWrite{w: &buf, path: lib, bytes: []byte(rewrite)}, src, "runtime")
		if rewrite == "AAAA" {
			if err != nil {
				t.Fatalf("the control was refused: %v", err)
			}
			if !payloadMatchesInventory(t, buf.Bytes()) {
				t.Fatal("the control's payload contradicts its inventory")
			}
			continue
		}
		if err == nil {
			t.Fatalf("packed (report %+v); payload matches inventory: %v", rep, payloadMatchesInventory(t, buf.Bytes()))
		}
		if rep != nil || !strings.Contains(err.Error(), "lib.so changed while packing") {
			t.Fatalf("report %+v, error %v: want no report and a refusal naming lib.so", rep, err)
		}
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

// payloadMatchesInventory reads the archive with archive/tar, a third
// opinion, and reports whether every file it carries hashes to its
// inventory row.
func payloadMatchesInventory(t *testing.T, archive []byte) bool {
	t.Helper()
	rows := map[string]string{}
	for _, f := range inventoryOf(t, archive).Files {
		rows[f.Path] = f.SHA256
	}
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return true
		}
		if err != nil {
			t.Fatal(err)
		}
		rel := strings.TrimPrefix(h.Name, "runtime/")
		if h.Typeflag != tar.TypeReg || rel == InventoryFile {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if sum := sha256.Sum256(b); rows[rel] != "sha256:"+hex.EncodeToString(sum[:]) {
			return false
		}
	}
}
