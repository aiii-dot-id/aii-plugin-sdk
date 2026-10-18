package aiiospkg

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type whenVectors struct {
	Floor       string          `json:"floor"`
	Settings    []SettingDecl   `json:"settings"`
	Base        json.RawMessage `json:"base"`
	Declaration []struct {
		Name   string `json:"name"`
		When   string `json:"when"`
		Min    string `json:"min"`
		Host   bool   `json:"host"`
		Author bool   `json:"author"`
	} `json:"declaration"`
}

// A model needed only for some values of a setting is accepted exactly
// where the host accepts it: the member's form, the package's own
// settings, and the floor. The host holds the same vectors.
func TestModelWhenSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/model_when.json")
	if err != nil {
		t.Fatal(err)
	}
	var v whenVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Floor != ModelWhenMinHost || len(v.Declaration) < 16 {
		t.Fatalf("vectors: floor %q against %q, %d cases", v.Floor, ModelWhenMinHost, len(v.Declaration))
	}
	base := bytes.TrimSuffix(bytes.TrimSpace(v.Base), []byte("}"))
	for _, c := range v.Declaration {
		t.Run(c.Name, func(t *testing.T) {
			var d ModelDecl
			dec := json.NewDecoder(bytes.NewReader(append(append(append([]byte{}, base...), c.When...), '}')))
			dec.DisallowUnknownFields()
			err := dec.Decode(&d)
			if err == nil {
				err = ValidateModels([]ModelDecl{d})
			}
			if err == nil {
				err = ValidateModelConditions([]ModelDecl{d}, v.Settings, c.Min)
			}
			if (err == nil) != c.Author || c.Author != c.Host {
				t.Fatalf("author acceptance %v, the vectors say author %v and host %v (%v)", err == nil, c.Author, c.Host, err)
			}
		})
	}
}

// The packer writes the condition into the packaged member, canonically,
// and writes nothing for a model that has none; an author's configuration
// is held to it whole, and no accelerator profile may name a conditional
// model.
func TestAConditionalModelIsPackedAndHeldToTheConfiguration(t *testing.T) {
	prof := &AcceleratorProfile{OS: "macos", Arch: "arm64", Backend: "mlx", Operators: []string{"attention"}, RuntimeLibraries: []string{"mlx-0.20"}, Precision: "int8",
		Models: []string{"base.bin"}, MemoryBytes: 1 << 30, SessionLimit: 1, Fallback: "none"}
	model := func(name string, when *ModelWhen) ModelDecl {
		return ModelDecl{Name: name, URL: "https://models.example.test/" + name, SHA256: strings.Repeat("a", 64), Size: 1 << 20, When: when}
	}
	cfg := &AuthorConfig{ID: "com.example.engine", Version: "0.1.0", PluginFamily: "voice_interface", AiiosMinVersion: ModelWhenMinHost,
		Interface:          AuthorInterface{ID: "speech.engine", Version: 1},
		CapabilityEnvelope: []string{},
		Settings:           []SettingDecl{{Key: "tts_language", Type: SettingEnum, Title: "Speaking language", Default: "en", Values: []string{"en", "fr", "de"}}},
		Models:             []ModelDecl{model("base.bin", nil), model("fr-pack.bin", &ModelWhen{Setting: "tts_language", Values: []string{"fr", "de"}})},
		Variants: []AuthorVariant{{VariantID: "macos-arm64-native", Platform: "macos", Arch: "arm64", Topology: "full_identity_host",
			ExecutionRuntime: "native_t3_component", AdmissionProfile: "platform_reserved", VariantCapabilities: []string{}, Artifact: "dist/engine", Accelerator: prof}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a configuration with a conditional model validates: %v", err)
	}
	packed, err := ModelsJSON(cfg.Models)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"base.bin","sha256":"` + strings.Repeat("a", 64) + `","size":1048576,"url":"https://models.example.test/base.bin"},` +
		`{"name":"fr-pack.bin","sha256":"` + strings.Repeat("a", 64) + `","size":1048576,"url":"https://models.example.test/fr-pack.bin","when":{"setting":"tts_language","values":["fr","de"]}}]`
	if string(packed) != want {
		t.Fatalf("the packaged member:\n%s\nwant\n%s", packed, want)
	}

	for name, c := range map[string]struct {
		change  func(*AuthorConfig)
		refusal string
	}{
		"a floor below the member's": {func(c *AuthorConfig) { c.AiiosMinVersion = "0.1.14" }, "requires aiios_min_version >= " + ModelWhenMinHost},
		"no floor":                   {func(c *AuthorConfig) { c.AiiosMinVersion = "" }, "requires aiios_min_version >= " + ModelWhenMinHost},
		"no such setting":            {func(c *AuthorConfig) { c.Settings = nil }, "not a setting this package declares"},
		"a value the setting lacks": {func(c *AuthorConfig) {
			c.Models = []ModelDecl{model("base.bin", nil), model("fr-pack.bin", &ModelWhen{Setting: "tts_language", Values: []string{"nl"}})}
		}, "not a value of setting"},
		"a profile naming a conditional model": {func(c *AuthorConfig) {
			p := *prof
			p.Models = []string{"base.bin", "fr-pack.bin"}
			c.Variants = []AuthorVariant{c.Variants[0]}
			c.Variants[0].Accelerator = &p
		}, "needed only for some values of a setting"},
	} {
		bad := *cfg
		c.change(&bad)
		if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), c.refusal) {
			t.Errorf("%s: want a refusal saying %q, got: %v", name, c.refusal, err)
		}
	}
}
