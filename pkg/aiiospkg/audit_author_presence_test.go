// .
// .
// .
// .

package aiiospkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditAuthorWindowDoesNotEraseMalformedPresence(t *testing.T) {
	base, err := json.Marshal(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"aiios_min_version", "aiios_max_exclusive_version"} {
		for _, raw := range []string{`null`, `""`, `"1.0.0-beta.1"`, `"1.0.0+build"`} {
			t.Run(key+"="+raw, func(t *testing.T) {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(base, &m); err != nil {
					t.Fatal(err)
				}
				m[key] = json.RawMessage(raw)
				b, _ := json.Marshal(m)
				path := filepath.Join(t.TempDir(), "plugin.json")
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err := LoadAuthorConfig(path)
				if err != nil {
					return
				}
				files := testInstallFiles(cfg, []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`), []byte("\x00asm-fake-module"))
				manifest, err := BuildManifest(cfg, []string{"core.echo"}, files)
				if err != nil {
					return
				}
				var emitted map[string]json.RawMessage
				if err := json.Unmarshal(manifest, &emitted); err != nil {
					t.Fatal(err)
				}
				value, present := emitted[key]
				t.Errorf("explicit malformed %s=%s accepted by LoadAuthorConfig and BuildManifest; emitted present=%v value=%q (absent means unconstrained)", key, raw, present, string(value))
			})
		}
	}
}

func TestAuditAuthorWindowPreservesValidAndOmittedPresence(t *testing.T) {
	for _, bounds := range []map[string]string{{}, {"aiios_min_version": "00.01.006"}, {"aiios_max_exclusive_version": "999999999999999999999.0.0"}, {"aiios_min_version": "0.1.0", "aiios_max_exclusive_version": "9.0.0"}} {
		base, _ := json.Marshal(testConfig())
		var m map[string]json.RawMessage
		if err := json.Unmarshal(base, &m); err != nil {
			t.Fatal(err)
		}
		delete(m, "aiios_min_version")
		delete(m, "aiios_max_exclusive_version")
		for key, value := range bounds {
			b, _ := json.Marshal(value)
			m[key] = b
		}
		data, _ := json.Marshal(m)
		path := filepath.Join(t.TempDir(), "plugin.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadAuthorConfig(path)
		if err != nil {
			t.Fatalf("valid author bounds %v refused: %v", bounds, err)
		}
		files := testInstallFiles(cfg, []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`), []byte("\x00asm-fake-module"))
		manifest, err := BuildManifest(cfg, []string{"core.echo"}, files)
		if err != nil {
			t.Fatal(err)
		}
		var emitted map[string]json.RawMessage
		if err := json.Unmarshal(manifest, &emitted); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"aiios_min_version", "aiios_max_exclusive_version"} {
			value, present := emitted[key]
			want, named := bounds[key]
			var got string
			if present {
				if err := json.Unmarshal(value, &got); err != nil {
					t.Fatal(err)
				}
			}
			if present != named || got != want {
				t.Errorf("%s got%q present%v want%q present%v", key, got, present, want, named)
			}
		}
	}
}
