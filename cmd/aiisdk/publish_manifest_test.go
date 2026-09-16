package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// .
// .
// .
type handMember struct {
	name string
	typ  byte
	data []byte
}

func handArchive(t *testing.T, members []handMember, trailing []byte) []byte {
	t.Helper()
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	for _, m := range members {
		h := &tar.Header{Name: m.name, Typeflag: m.typ, Mode: 0o644, Size: int64(len(m.data))}
		if m.typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if m.typ == tar.TypeSymlink {
			h.Linkname = "elsewhere"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	tb.Write(trailing)
	var gb bytes.Buffer
	zw := gzip.NewWriter(&gb)
	if _, err := zw.Write(tb.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return gb.Bytes()
}

// .
func manifestOf(t *testing.T, bundle []byte) []byte {
	t.Helper()
	z, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	r := tar.NewReader(z)
	for {
		h, err := r.Next()
		if err != nil {
			t.Fatal("no manifest.json in the fixture bundle")
		}
		if strings.HasSuffix(h.Name, "/manifest.json") {
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
}

// .
// .
// .
// .
func TestReadCatalogManifestRefusesWhatTheHostWould(t *testing.T) {
	dir := t.TempDir()
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "1.0.0", Variants: []aiiospkg.AuthorVariant{
		{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
	}}
	bundle := writeCatalogFixture(t, cfg, filepath.Join(dir, "p.aiiospkg"))
	manifest := manifestOf(t, bundle)
	root := "com.x.eng-1.0.0"
	dirM := func(name string) handMember { return handMember{name: name + "/", typ: tar.TypeDir} }
	fileM := func(name string, data []byte) handMember { return handMember{name: name, typ: tar.TypeReg, data: data} }
	good := []handMember{dirM(root), fileM(root+"/manifest.json", manifest), dirM(root + "/install-root"), fileM(root+"/install-root/a", []byte("x"))}

	if _, err := readCatalogManifest(handArchive(t, good, nil)); err != nil {
		t.Fatalf("the control archive must be accepted: %v", err)
	}

	many := []handMember{dirM(root), fileM(root+"/manifest.json", manifest)}
	for i := 0; i < 1023; i++ {
		many = append(many, fileM(root+"/install-root-"+strings.Repeat("f", i%7)+string(rune('a'+i%26))+strings.Repeat("g", i/26), []byte("x")))
	}
	cases := []struct {
		name     string
		members  []handMember
		trailing []byte
		want     string
	}{
		{"a regular file before the root", []handMember{fileM("stray", []byte("x")), dirM(root), fileM(root+"/manifest.json", manifest)}, nil, "sole root directory"},
		{"a member outside the root", append(append([]handMember{}, good...), fileM("other/x", []byte("x"))), nil, "foreign package member"},
		{"a duplicate member", append(append([]handMember{}, good...), fileM(root+"/install-root/a", []byte("x"))), nil, "duplicate"},
		{"too many members", many, nil, "excessive"},
		{"a symlink", append(append([]handMember{}, good...), handMember{name: root + "/install-root/link", typ: tar.TypeSymlink}), nil, "not a regular file or directory"},
		{"a manifest over 1 MiB", []handMember{dirM(root), fileM(root+"/manifest.json", bytes.Repeat([]byte(" "), 1<<20+1))}, nil, "1..1048576"},
		{"a manifest that is not a plugin", []handMember{dirM(root), fileM(root+"/manifest.json", []byte(`{"kind":"other"}`))}, nil, "plugin kind"},
		{"bytes after the end blocks", good, bytes.Repeat([]byte{0}, 1024), "excess tar bytes"},
		{"a root that is not the manifest's id-version", []handMember{dirM("com.x.eng-2.0.0"), fileM("com.x.eng-2.0.0/manifest.json", manifest)}, nil, "does not name its manifest"},
		{"no manifest", []handMember{dirM(root), fileM(root+"/install-root/a", []byte("x"))}, nil, "no manifest.json"},
		{"an empty archive", nil, nil, "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readCatalogManifest(handArchive(t, c.members, c.trailing))
			if err == nil {
				t.Fatalf("%s: accepted", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%s: refused for another reason: %v", c.name, err)
			}
		})
	}
}

// .
// .
func TestAStaleAuthorRefusalNamesTheDifference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.aiiospkg")
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "1.0.0", Variants: []aiiospkg.AuthorVariant{
		{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
	}}
	writeCatalogFixture(t, cfg, path)
	cfg.Variants[0].Arch = "arm64"
	_, err := buildCatalogEntry(cfg, path, dir, "https://h/p.aiiospkg", "T3", "")
	if err == nil || !strings.Contains(err.Error(), "linux/x86_64") || !strings.Contains(err.Error(), "linux/arm64") {
		t.Fatalf("the refusal does not show both tuples: %v", err)
	}
	cfg.Variants[0].Arch = "x86_64"
	cfg.Variants = append(cfg.Variants, aiiospkg.AuthorVariant{VariantID: "extra", Platform: "macos", Arch: "arm64", ExecutionRuntime: "native_t3_component"})
	_, err = buildCatalogEntry(cfg, path, dir, "https://h/p.aiiospkg", "T3", "")
	if err == nil || !strings.Contains(err.Error(), "extra") || !strings.Contains(err.Error(), "the package carries") {
		t.Fatalf("the refusal does not show the sets: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a package"), 0o644); err != nil {
		t.Fatal(err)
	}
}
