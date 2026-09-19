package aiiospkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// .
// .
// .
// .
// .
// .
// .
// .
var hostWindowFixtures = []struct {
	Name, Min, Max string
}{
	{"no-window", "", ""},
	{"min-only", "0.1.0", ""},
	{"max-only", "", "9.0.0"},
	{"both", "0.1.0", "9.0.0"},
	{"leading-zeros", "00.01.006", "00.02.000"},
	{"large-max", "1.0.0", "999999999999999999999999999999999999.0.0"},
}

func TestSDKBuiltHostWindowPackagesForTheHostReader(t *testing.T) {
	out := os.Getenv("AIIOS_HOST_WINDOW_FIXTURES_OUT")
	descriptors := []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`)
	expected := map[string]map[string]string{}
	for _, fx := range hostWindowFixtures {
		cfg := testConfig()
		cfg.ID, cfg.Version = "org.example.window."+fx.Name, "0.1.0"
		cfg.AiiosMinVersion, cfg.AiiosMaxExclusiveVersion = fx.Min, fx.Max
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: %v", fx.Name, err)
		}
		files := testInstallFiles(cfg, descriptors, []byte("\x00asm-fake-module"))
		manifest, err := BuildManifest(cfg, []string{"core.echo"}, files)
		if err != nil {
			t.Fatalf("%s: %v", fx.Name, err)
		}
		tree := NewTree(cfg.Root())
		tree.Add("manifest.json", manifest)
		for rel, data := range files {
			tree.Add("install-root/"+rel, data)
		}
		bundle, err := WriteBundle(tree)
		if err != nil {
			t.Fatalf("%s: %v", fx.Name, err)
		}
		// .
		got := manifestInBundle(t, bundle, cfg.Root()+"/manifest.json")
		want := map[string]string{}
		if fx.Min != "" {
			want["aiios_min_version"] = fx.Min
		}
		if fx.Max != "" {
			want["aiios_max_exclusive_version"] = fx.Max
		}
		for _, name := range []string{"aiios_min_version", "aiios_max_exclusive_version"} {
			raw, present := got[name]
			if w, wanted := want[name]; wanted != present || (present && string(raw) != `"`+w+`"`) {
				t.Errorf("%s: %s in the archive is %s, want %q (present %v)", fx.Name, name, raw, w, wanted)
			}
		}
		expected[fx.Name] = want
		if out != "" {
			if err := os.WriteFile(filepath.Join(out, fx.Name+".aiiospkg"), bundle, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out != "" {
		enc, _ := json.MarshalIndent(expected, "", " ")
		if err := os.WriteFile(filepath.Join(out, "expected.json"), append(enc, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// .
// .
func manifestInBundle(t *testing.T, bundle []byte, path string) map[string]json.RawMessage {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name != path {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	t.Fatalf("%s not in the archive", path)
	return nil
}
