package aiiosdk

// .
// .
// .
// .

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// .
// .
// .
// .
type manifestInterfaceDecl struct {
	ID         string   `json:"id"`
	Version    int      `json:"version"`
	SchemaHash string   `json:"schema_hash"`
	Methods    []string `json:"methods"`
}

func describedPlugin() *Plugin {
	p := New("com.example.memory")
	p.Handle("focus.set", func(Call) (any, error) { return nil, nil })
	p.Handle("focus.get", func(Call) (any, error) { return nil, nil })
	p.Handle("focus.echo", func(Call) (any, error) { return nil, nil })
	p.Describe("focus.set", Descriptor{
		Summary:      "Remember the current focus",
		Input:        "schemas/focus_set_in.json",
		Output:       "schemas/focus_set_out.json",
		Effects:      EffectsWriteLocal,
		Capabilities: []string{"kv"},
	})
	p.Describe("focus.echo", Descriptor{
		Summary: "Echo the arguments back",
		Effects: EffectsReadInternal,
	})
	return p
}

func TestDescriptorsJSON(t *testing.T) {
	p := describedPlugin()
	raw, err := p.DescriptorsJSON()
	if err != nil {
		t.Fatalf("DescriptorsJSON: %v", err)
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("emitted JSON: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("want every HANDLED operation covered, got %d", len(list))
	}
	// .
	// .
	// .
	var ids []string
	for _, d := range list {
		for _, key := range []string{"id", "summary", "input", "output", "effects", "capabilities"} {
			if _, ok := d[key]; !ok {
				t.Fatalf("descriptor missing %q: %s", key, raw)
			}
		}
		var id string
		if err := json.Unmarshal(d["id"], &id); err != nil {
			t.Fatalf("id: %v", err)
		}
		ids = append(ids, id)
		if string(d["capabilities"]) == "null" {
			t.Fatalf("capabilities must emit [], never null: %s", raw)
		}
	}
	if strings.Join(ids, ",") != "focus.echo,focus.get,focus.set" {
		t.Fatalf("not sorted by id: %v", ids)
	}
	// .
	raw2, _ := p.DescriptorsJSON()
	if string(raw) != string(raw2) {
		t.Fatalf("emission must be deterministic")
	}
}

func TestManifestInterfacesShape(t *testing.T) {
	p := describedPlugin()
	raw, err := p.manifestInterfaces("com.example.memory.focus", 1)
	if err != nil {
		t.Fatalf("manifestInterfaces: %v", err)
	}
	var frag struct {
		Core []manifestInterfaceDecl `json:"core"`
	}
	if err := json.Unmarshal(raw, &frag); err != nil {
		t.Fatalf("fragment: %v", err)
	}
	if len(frag.Core) != 1 {
		t.Fatalf("want one core declaration, got %d", len(frag.Core))
	}
	decl := frag.Core[0]
	if decl.ID != "com.example.memory.focus" || decl.Version != 1 {
		t.Fatalf("decl: %+v", decl)
	}
	if strings.Join(decl.Methods, ",") != "focus.echo,focus.get,focus.set" {
		t.Fatalf("methods must be the sorted handled operations: %v", decl.Methods)
	}
	// .
	// .
	// .
	// .
	schemaBytes, _ := p.DescriptorsJSON()
	sum := sha256.Sum256(schemaBytes)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if decl.SchemaHash != want {
		t.Fatalf("schema_hash:\n got %s\nwant %s", decl.SchemaHash, want)
	}
	// .
	var loose []map[string]json.RawMessage
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 1 {
		t.Fatalf("fragment carries exactly {core}: %s", raw)
	}
	if err := json.Unmarshal(top["core"], &loose); err != nil {
		t.Fatalf("core: %v", err)
	}
	for _, m := range loose {
		if len(m) != 4 {
			t.Fatalf("decl must carry exactly id/version/schema_hash/methods: %s", raw)
		}
		for _, key := range []string{"id", "version", "schema_hash", "methods"} {
			if _, ok := m[key]; !ok {
				t.Fatalf("decl missing %q: %s", key, raw)
			}
		}
	}
}

func TestDescriptorGuards(t *testing.T) {
	t.Run("described but unhandled refuses to emit", func(t *testing.T) {
		p := New("x")
		p.Handle("a", func(Call) (any, error) { return nil, nil })
		p.Describe("ghost", Descriptor{Summary: "no handler"})
		if _, err := p.DescriptorsJSON(); err == nil {
			t.Fatalf("drift must refuse")
		}
		if _, err := p.manifestInterfaces("i", 1); err == nil {
			t.Fatalf("drift must refuse")
		}
	})

	t.Run("no handlers refuses a manifest", func(t *testing.T) {
		if _, err := New("x").manifestInterfaces("i", 1); err == nil {
			t.Fatalf("manifests require interfaces.core with at least one declaration (manifest.go:333-334)")
		}
	})

	t.Run("interface id and version validated", func(t *testing.T) {
		p := New("x")
		p.Handle("a", func(Call) (any, error) { return nil, nil })
		if _, err := p.manifestInterfaces("", 1); err == nil {
			t.Fatalf("empty id must refuse")
		}
		if _, err := p.manifestInterfaces("i", 0); err == nil {
			t.Fatalf("version 0 must refuse")
		}
	})

	t.Run("mismatched descriptor id panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("must panic")
			}
		}()
		New("x").Describe("op", Descriptor{ID: "other"})
	})

	t.Run("duplicate descriptor panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("must panic")
			}
		}()
		p := New("x")
		p.Describe("op", Descriptor{})
		p.Describe("op", Descriptor{})
	})
}
