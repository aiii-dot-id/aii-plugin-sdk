package aiiospkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .
// .
// .
// .
// .
// .
// .
type hostWindowVectors struct {
	Grammar []struct {
		Name, Min, Max string
		OK             bool   `json:"ok"`
		RefusesNaming  string `json:"refuses_naming"`
	} `json:"grammar"`
	Order []struct {
		Name, A, B string
		Cmp        int `json:"cmp"`
	} `json:"order"`
	Presence []struct {
		Name          string
		MinRaw        string `json:"min_raw"`
		MaxRaw        string `json:"max_raw"`
		OK            bool   `json:"ok"`
		RefusesNaming string `json:"refuses_naming"`
	} `json:"presence"`
}

func loadHostWindowVectors(t *testing.T) hostWindowVectors {
	t.Helper()
	raw, err := os.ReadFile("../../vectors/host_window.json")
	if err != nil {
		t.Fatal(err)
	}
	var file hostWindowVectors
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Grammar) < 20 || len(file.Order) < 8 || len(file.Presence) < 8 {
		t.Fatalf("the vectors are thin: grammar %d, order %d, presence %d", len(file.Grammar), len(file.Order), len(file.Presence))
	}
	return file
}

func TestHostWindowVectors(t *testing.T) {
	file := loadHostWindowVectors(t)
	t.Run("grammar", func(t *testing.T) {
		for _, c := range file.Grammar {
			cfg := testConfig()
			cfg.AiiosMinVersion, cfg.AiiosMaxExclusiveVersion = c.Min, c.Max
			err := cfg.Validate()
			switch {
			case c.OK && err != nil:
				t.Errorf("%s: refused: %v", c.Name, err)
			case !c.OK && err == nil:
				t.Errorf("%s: accepted min=%q max=%q, want a refusal naming %q", c.Name, c.Min, c.Max, c.RefusesNaming)
			case !c.OK && c.RefusesNaming != "" && !strings.Contains(err.Error(), c.RefusesNaming):
				t.Errorf("%s: refusal %q does not name %q", c.Name, err, c.RefusesNaming)
			}
		}
	})
	t.Run("order", func(t *testing.T) {
		for _, c := range file.Order {
			if got := CompareHostVersion(c.A, c.B); got != c.Cmp {
				t.Errorf("%s: compare(%q, %q) = %d, want %d", c.Name, c.A, c.B, got, c.Cmp)
			}
		}
	})
	// .
	// .
	// .
	// .
	// .
	t.Run("presence: the writer never emits what the reader refuses", func(t *testing.T) {
		descriptors := []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`)
		base, err := json.Marshal(testConfig())
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range file.Presence {
			extra := ""
			if c.MinRaw != "" {
				extra += `"aiios_min_version":` + c.MinRaw + `,`
			}
			if c.MaxRaw != "" {
				extra += `"aiios_max_exclusive_version":` + c.MaxRaw + `,`
			}
			// .
			// .
			// .
			// .
			// .
			text := strings.TrimSuffix(string(base), "}")
			if extra != "" {
				text += "," + strings.TrimSuffix(extra, ",")
			}
			text += "}"
			path := filepath.Join(t.TempDir(), "plugin.json")
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			// .
			// .
			// .
			// .
			// .
			// .
			// .
			cfg, err := LoadAuthorConfig(path)
			var manifest []byte
			if err == nil {
				manifest, err = BuildManifest(cfg, []string{"core.echo"}, testInstallFiles(cfg, descriptors, []byte("\x00asm-fake-module")))
			}
			if !c.OK {
				switch {
				case err == nil:
					t.Errorf("%s: min=%s max=%s was accepted by the writer; the host reader refuses it", c.Name, c.MinRaw, c.MaxRaw)
				case c.RefusesNaming != "" && !strings.Contains(err.Error(), c.RefusesNaming):
					t.Errorf("%s: refusal %q does not name %q", c.Name, err, c.RefusesNaming)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: a valid window was refused: %v", c.Name, err)
				continue
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(manifest, &m); err != nil {
				t.Fatal(err)
			}
			for name, written := range map[string]string{"aiios_min_version": c.MinRaw, "aiios_max_exclusive_version": c.MaxRaw} {
				raw, present := m[name]
				switch {
				case written == "" && present:
					t.Errorf("%s: %s was omitted and the manifest carries %s", c.Name, name, raw)
				case written != "" && string(raw) != written:
					t.Errorf("%s: %s was written %s and emitted %q (absent means unconstrained)", c.Name, name, written, string(raw))
				}
			}
		}
	})
}
