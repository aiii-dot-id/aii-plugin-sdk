package aiiospkg

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .
// .
// .
// .
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
	// .
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
	// .
	invStart := bytes.Index(raw, []byte(`{"files"`))
	if invStart < 0 {
		t.Fatal("no inventory")
	}
	var inv Inventory
	end := bytes.IndexByte(raw[invStart:], 0)
	if err := json.Unmarshal(raw[invStart:invStart+end], &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Files) != 3 || inv.Files[0].Path != "bin/carrier" || inv.Files[0].Mode != "exec" || inv.Files[1].Mode != "file" {
		t.Fatalf("inventory %+v", inv)
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
	// .
	// .
	over := good
	over.InstalledBytes, over.Files, over.Size = RuntimeLimits.MaxInstalledBytes+1, RuntimeLimits.MaxFiles+1, RuntimeLimits.MaxCompressedBytes+1
	if err := ValidateRuntimes([]RuntimeDecl{over}, variants); err != nil {
		t.Fatalf("a declaration past the defaults must stand: %v", err)
	}
	req := DeclaredCeilings(over)
	if len(req) != 3 || req[CeilingInstalledBytes] != over.InstalledBytes || req[CeilingFiles] != int64(over.Files) || req[CeilingCompressedBytes] != over.Size {
		t.Fatalf("declared ceilings = %v", req)
	}
	if len(DeclaredCeilings(good)) != 0 {
		t.Fatal("a declaration within the defaults needs nothing")
	}
	if f := FormatCeilings(req); !strings.HasPrefix(f, CeilingCompressedBytes+" >= ") || !strings.Contains(f, ", "+CeilingFiles+" >= ") {
		t.Fatalf("format = %q", f)
	}
	if raw, err := RuntimesJSON([]RuntimeDecl{good}); err != nil || !strings.Contains(string(raw), `"runtimes"`) {
		t.Fatalf("json: %v %s", err, raw)
	}
}

// .
// .
// .
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
	big := RuntimeArchive{InstalledBytes: 8 << 30, Files: 40000, LargestFileBytes: 1 << 30, Size: 4 << 30, Depth: 30}
	req := big.RequiredCeilings()
	want := map[string]int64{CeilingInstalledBytes: 8 << 30, CeilingFiles: 40000, CeilingFileBytes: 1 << 30, CeilingCompressedBytes: 4 << 30, CeilingDepth: 30}
	if len(req) != len(want) {
		t.Fatalf("required = %v", req)
	}
	for k, v := range want {
		if req[k] != v {
			t.Fatalf("required[%s] = %d, want %d", k, req[k], v)
		}
	}
}
