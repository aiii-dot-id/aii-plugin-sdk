package aiiospkg

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type embeddingsVectors struct {
	Floor         string         `json:"floor"`
	ProbeBoundMin float64        `json:"probe_bound_min"`
	Base          map[string]any `json:"base"`
	Declaration   []struct {
		Name   string         `json:"name"`
		Set    map[string]any `json:"set"`
		Raw    string         `json:"raw"`
		RawHex string         `json:"raw_hex"`
		Host   bool           `json:"host"`
		Author bool           `json:"author"`
	} `json:"declaration"`
}

// An embeddings declaration is accepted here exactly where the host
// accepts it: its bounds, its probes, and how strictly it is read (a
// member's name in another case, a member twice, a null in a vector and
// content after the declaration are all refused there). The host holds
// the same file, byte for byte.
func TestEmbeddingsSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/embeddings.json")
	if err != nil {
		t.Fatal(err)
	}
	var v embeddingsVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Floor != EmbeddingsMinHost || v.ProbeBoundMin != MinProbeBound || len(v.Declaration) < 30 {
		t.Fatalf("vectors: floor %q against %q, least probe bound %v against %v, %d cases", v.Floor, EmbeddingsMinHost, v.ProbeBoundMin, MinProbeBound, len(v.Declaration))
	}
	for _, c := range v.Declaration {
		t.Run(c.Name, func(t *testing.T) {
			text := c.Raw
			if c.RawHex != "" {
				// Bytes a JSON string cannot carry, such as one that is not UTF-8.
				b, err := hex.DecodeString(c.RawHex)
				if err != nil {
					t.Fatal(err)
				}
				text = string(b)
			}
			if text == "" {
				decl := map[string]any{}
				for k, val := range v.Base {
					decl[k] = val
				}
				for k, val := range c.Set {
					decl[k] = val
				}
				made, err := json.Marshal(decl)
				if err != nil {
					t.Fatal(err)
				}
				text = string(made)
			}
			_, err := ParseEmbeddings([]byte(text))
			if (err == nil) != c.Author || c.Author != c.Host {
				t.Fatalf("author acceptance %v, the vectors say author %v and host %v (%v)", err == nil, c.Author, c.Host, err)
			}
		})
	}
}

func embeddingsConfig() *AuthorConfig {
	return &AuthorConfig{ID: "com.example.meaning", Version: "0.1.0", PluginFamily: EmbeddingsFamily, AiiosMinVersion: EmbeddingsMinHost,
		Interface:          AuthorInterface{ID: EmbeddingsInterfaceID, Version: EmbeddingsInterfaceVersion},
		CapabilityEnvelope: []string{}, EmbeddingsFile: "embeddings.json",
		Models: []ModelDecl{{Name: "m.onnx", URL: "https://models.example.test/m.onnx", SHA256: strings.Repeat("a", 64), Size: 1 << 20}},
		Variants: []AuthorVariant{{VariantID: "linux-x86_64-cpu", Platform: "linux", Arch: "x86_64", Topology: "full_identity_host",
			ExecutionRuntime: "native_t3_component", AdmissionProfile: "platform_reserved", VariantCapabilities: []string{}}},
	}
}

// plugin.json is held to what the host admits as a source of vectors, in
// both directions: a package the packer writes is one the host admits, and
// the refusal comes at the author's desk, in words about the thing that
// is wrong.
func TestASourceOfVectorsIsHeldToWhatTheHostAdmits(t *testing.T) {
	if err := embeddingsConfig().validateEmbeddingsSource(); err != nil {
		t.Fatalf("a configuration the host admits is refused: %v", err)
	}
	for name, tc := range map[string]struct {
		turn func(*AuthorConfig)
		says string
	}{
		"another family":         {func(c *AuthorConfig) { c.PluginFamily = "tool_bridge" }, "plugin_family must be " + EmbeddingsFamily},
		"another version":        {func(c *AuthorConfig) { c.Interface.Version = 2 }, "this kit writes @1"},
		"no floor":               {func(c *AuthorConfig) { c.AiiosMinVersion = "" }, "requires aiios_min_version >= " + EmbeddingsMinHost},
		"a floor below":          {func(c *AuthorConfig) { c.AiiosMinVersion = "0.1.14" }, "requires aiios_min_version >= " + EmbeddingsMinHost},
		"a capability":           {func(c *AuthorConfig) { c.CapabilityEnvelope = []string{"fs.private"} }, "capability_envelope must be []"},
		"a variant's capability": {func(c *AuthorConfig) { c.Variants[0].VariantCapabilities = []string{"net.local"} }, "variant_capabilities must be []"},
		"settings":               {func(c *AuthorConfig) { c.Settings = []SettingDecl{{Key: "threads", Type: "number", Title: "Threads"}} }, "declares no settings"},
		"a schedule":             {func(c *AuthorConfig) { c.Schedule = []ScheduleDecl{{Operation: "embed", EverySeconds: 600}} }, "declares no schedule"},
		"a webhook":              {func(c *AuthorConfig) { c.Webhooks = []WebhookDecl{{Path: "in", Operation: "embed"}} }, "declares no webhooks"},
		"a subscription": {func(c *AuthorConfig) {
			c.Subscriptions = []SubscriptionDecl{{Topic: "turn.completed", Operation: "embed"}}
		}, "declares no subscriptions"},
		"a variant that is not native": {func(c *AuthorConfig) { c.Variants[0].ExecutionRuntime = "wasm_component" }, "must be native"},
		"no declaration file":          {func(c *AuthorConfig) { c.EmbeddingsFile = "" }, "embeddings_file must name its declaration"},
		"a second interface": {func(c *AuthorConfig) {
			c.Interfaces, c.Interface = []AuthorInterface{{ID: EmbeddingsInterfaceID, Version: 1, Methods: []string{"embed"}}, {ID: "x.y", Version: 1, Methods: []string{"z"}}}, AuthorInterface{}
		}, "only interface"},
	} {
		c := embeddingsConfig()
		tc.turn(c)
		if err := c.validateEmbeddingsSource(); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: want a refusal saying %q, got %v", name, tc.says, err)
		}
	}
	stray := embeddingsConfig()
	stray.PluginFamily, stray.Interface = "tool_bridge", AuthorInterface{ID: "x.y", Version: 1}
	if err := stray.validateEmbeddingsSource(); err == nil || !strings.Contains(err.Error(), "no "+EmbeddingsInterfaceID+" interface is declared") {
		t.Errorf("the file without the interface: %v", err)
	}

	// What only package time knows: the operations the plugin describes,
	// and the declaration against the package's models.
	decl := &EmbeddingsDecl{Model: "m.onnx"}
	if err := ValidateEmbeddingsPackage(embeddingsConfig(), decl, []string{MethodEmbed}); err != nil {
		t.Fatalf("embed alone, with a declared model: %v", err)
	}
	if err := ValidateEmbeddingsPackage(embeddingsConfig(), decl, []string{MethodEmbed, "status"}); err == nil || !strings.Contains(err.Error(), "describes one operation") {
		t.Errorf("two operations: %v", err)
	}
	if err := ValidateEmbeddingsPackage(embeddingsConfig(), &EmbeddingsDecl{Model: "other.onnx"}, []string{MethodEmbed}); err == nil || !strings.Contains(err.Error(), "does not declare") {
		t.Errorf("an undeclared model: %v", err)
	}
	conditional := embeddingsConfig()
	conditional.Models[0].When = &ModelWhen{Setting: "language", Values: []string{"fr"}}
	if err := ValidateEmbeddingsPackage(conditional, decl, []string{MethodEmbed}); err == nil || !strings.Contains(err.Error(), "always present") {
		t.Errorf("a conditional model: %v", err)
	}
}

// A SOURCE'S OPERATION MAY NOT ASK FOR A CONFIRMATION. The host drives
// embed and refuses a package of the interface whose descriptor declares
// operator_confirms; the kit packed one all the same, so an author learned
// of it at activation, on somebody's machine. The descriptor the plugin
// emits is held here: one that does not ask is accepted, one that asks is
// refused naming the operation, and so is anything that is not an
// emission.
func TestASourcesDescriptorMayNotAskForAConfirmation(t *testing.T) {
	if err := ValidateEmbeddingsDescriptors([]byte(`[{"id":"embed","summary":"one vector for one text","effects":"read.internal"}]`)); err != nil {
		t.Fatalf("a descriptor that asks for nothing is refused: %v", err)
	}
	if err := ValidateEmbeddingsDescriptors([]byte(`[{"id":"embed","operator_confirms":false}]`)); err != nil {
		t.Fatalf("a descriptor that says it does not ask is refused: %v", err)
	}
	err := ValidateEmbeddingsDescriptors([]byte(`[{"id":"embed","summary":"one vector for one text","operator_confirms":true}]`))
	if err == nil || !strings.Contains(err.Error(), "embed may not declare operator_confirms") {
		t.Fatalf("a descriptor that asks for a confirmation must be refused by name: %v", err)
	}
	if err := ValidateEmbeddingsDescriptors([]byte(`{"id":"embed"}`)); err == nil {
		t.Fatal("something that is not an emission was accepted")
	}
}
