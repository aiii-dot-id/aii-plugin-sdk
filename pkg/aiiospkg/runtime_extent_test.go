package aiiospkg

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type extentVectors struct {
	Floor       string          `json:"floor"`
	Base        json.RawMessage `json:"base"`
	Declaration []struct {
		Name, Extent, Min string
		Host, Author      bool
	} `json:"declaration"`
	Preflight []struct {
		Name    string
		Extent  bool
		Limits  map[string]int64
		Refused map[string]int64
	} `json:"preflight"`
}

func loadExtentVectors(t *testing.T) extentVectors {
	t.Helper()
	raw, err := os.ReadFile("../../vectors/runtime_extent.json")
	if err != nil {
		t.Fatal(err)
	}
	var v extentVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Floor != RuntimeExtentMinHost || len(v.Declaration) < 20 || len(v.Preflight) < 7 {
		t.Fatalf("vectors: floor %q, %d declaration and %d preflight cases", v.Floor, len(v.Declaration), len(v.Preflight))
	}
	return v
}

// decodeDecl reads one declaration as the host does: closed, numbers typed.
func decodeDecl(raw []byte) (RuntimeDecl, error) {
	var d RuntimeDecl
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&d)
	return d, err
}

// A declared extent is read exactly when the package's floor reads it;
// a package at that floor declares it; an extent no archive can have is
// refused. The host holds the same vectors.
func TestRuntimeExtentSharedVectors(t *testing.T) {
	v := loadExtentVectors(t)
	base := bytes.TrimSuffix(bytes.TrimSpace(v.Base), []byte("}"))
	variants := []AuthorVariant{{VariantID: "v"}}
	for _, c := range v.Declaration {
		t.Run(c.Name, func(t *testing.T) {
			raw := append(append(append([]byte{}, base...), c.Extent...), '}')
			d, err := decodeDecl(raw)
			host := err == nil
			if host {
				host = ValidateRuntimes([]RuntimeDecl{d}, variants) == nil && ValidateRuntimeExtent([]RuntimeDecl{d}, c.Min) == nil
			}
			if host != c.Host {
				t.Fatalf("host acceptance %v, want %v (decode: %v)", host, c.Host, err)
			}
			author := host && requireRuntimeExtent([]RuntimeDecl{d}, c.Min) == nil
			if author != c.Author {
				t.Fatalf("authoring acceptance %v, want %v", author, c.Author)
			}
			if host {
				out, err := RuntimesJSON([]RuntimeDecl{d})
				if err != nil {
					t.Fatal(err)
				}
				var back struct{ Runtimes []json.RawMessage }
				if err := json.Unmarshal(out, &back); err != nil || len(back.Runtimes) != 1 {
					t.Fatalf("rendered member: %v %s", err, out)
				}
				again, err := decodeDecl(back.Runtimes[0])
				if err != nil || !reflect.DeepEqual(again, d) {
					t.Fatalf("the rendered declaration does not read back: %v", err)
				}
			}
		})
	}
}

// The host's pre-download refusal, from the declaration alone: a byte
// ceiling the operator has not set refuses nothing; one they set
// refuses a runtime past it before the download — max_file_bytes only
// where the extent is declared; without it, only extraction can.
func TestRuntimeExtentPreflightVectors(t *testing.T) {
	v := loadExtentVectors(t)
	base, err := decodeDecl(v.Base)
	if err != nil {
		t.Fatal(err)
	}
	largest, depth := int64(547383096), 4
	for _, c := range v.Preflight {
		t.Run(c.Name, func(t *testing.T) {
			d := base
			if c.Extent {
				d.LargestFileBytes, d.Depth = &largest, &depth
			}
			limits := TreeLimits{
				MaxInstalledBytes: c.Limits["max_installed_bytes"], MaxFiles: int(c.Limits["max_files"]),
				MaxFileBytes: c.Limits["max_file_bytes"], MaxCompressedBytes: c.Limits["max_compressed_bytes"],
				MaxDepth: int(c.Limits["max_depth"]),
			}
			got := ExceededCeilings(d, limits)
			if !reflect.DeepEqual(got, c.Refused) {
				t.Fatalf("refused %v, want %v", got, c.Refused)
			}
			if len(c.Limits) == 0 && !reflect.DeepEqual(DeclaredCeilings(d), c.Refused) {
				t.Fatalf("DeclaredCeilings %v, want %v", DeclaredCeilings(d), c.Refused)
			}
		})
	}
}

// What runtime-pack measures is what the declaration carries: an archive
// written here declares an extent that validates against its own report.
func TestRuntimePackExtentDeclares(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/lib/deep", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"engine": 10, "lib/a.so": 300, "lib/deep/b.so": 20} {
		if err := os.WriteFile(dir+"/"+name, bytes.Repeat([]byte{1}, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	rep, err := WriteRuntimeTree(&buf, dir, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if rep.LargestFileBytes != 300 || rep.Depth != 3 {
		t.Fatalf("measured largest %d depth %d", rep.LargestFileBytes, rep.Depth)
	}
	d := RuntimeDecl{VariantID: "v", URL: "https://example.com/r.tar.gz", SHA256: rep.SHA256, Size: rep.Size,
		InstalledBytes: rep.InstalledBytes, Files: rep.Files, InventorySHA256: rep.InventorySHA256,
		LargestFileBytes: &rep.LargestFileBytes, Depth: &rep.Depth}
	if err := ValidateRuntimes([]RuntimeDecl{d}, []AuthorVariant{{VariantID: "v"}}); err != nil {
		t.Fatalf("runtime-pack's own numbers must declare: %v", err)
	}
	if err := ValidateRuntimeExtent([]RuntimeDecl{d}, RuntimeExtentMinHost); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(DeclaredCeilings(d), rep.RequiredCeilings()) {
		t.Fatalf("the declaration's ceilings %v differ from the archive's %v", DeclaredCeilings(d), rep.RequiredCeilings())
	}
}
