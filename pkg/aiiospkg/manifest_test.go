package aiiospkg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func testConfig() *AuthorConfig {
	return &AuthorConfig{
		ID: "com.example.hello", Version: "0.1.0", PluginFamily: "tool_bridge",
		Interface:          AuthorInterface{ID: "com.example.hello.core", Version: 1},
		CapabilityEnvelope: []string{},
		Variants: []AuthorVariant{{
			VariantID: "linux-x86_64-wasm", Platform: "linux", Arch: "x86_64",
			Topology: "full_identity_host", ExecutionRuntime: "wasm_component",
			AdmissionProfile: "wasm_sandbox", VariantCapabilities: []string{},
		}},
	}
}

func testInstallFiles(cfg *AuthorConfig, descriptors, wasm []byte) map[string][]byte {
	files := map[string][]byte{SchemaFileRel(cfg.Interface): descriptors}
	for _, v := range cfg.Variants {
		files[EntrypointRel(v)] = wasm
	}
	return files
}

func TestBuildManifestHonestQuartet(t *testing.T) {
	cfg := testConfig()
	descriptors := []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`)
	wasm := []byte("\x00asm-fake-module")
	files := testInstallFiles(cfg, descriptors, wasm)

	manifest, err := BuildManifest(cfg, []string{"core.echo"}, files)
	if err != nil {
		t.Fatal(err)
	}
	// .
	canon, err := CanonicalizeV1(manifest)
	if err != nil || !bytes.Equal(canon, manifest) {
		t.Fatalf("manifest bytes are not canonical (%v)", err)
	}

	var m struct {
		Kind               string          `json:"kind"`
		ID                 string          `json:"id"`
		Version            string          `json:"version"`
		PackageHash        string          `json:"package_hash"`
		PluginFamily       string          `json:"plugin_family"`
		BBBProtocolVersion int             `json:"bbb_protocol_version"`
		DefaultVariant     string          `json:"default_variant"`
		CapabilityEnvelope []string        `json:"capability_envelope"`
		Interfaces         json.RawMessage `json:"interfaces"`
		Variants           []struct {
			VariantID    string          `json:"variant_id"`
			Entrypoint   string          `json:"entrypoint"`
			ArtifactHash string          `json:"artifact_hash"`
			Implements   json.RawMessage `json:"implements"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	if m.Kind != "plugin" || m.ID != cfg.ID || m.Version != cfg.Version {
		t.Fatalf("identity fields: %+v", m)
	}
	if m.BBBProtocolVersion != 2 {
		t.Fatalf("bbb_protocol_version = %d, want 2", m.BBBProtocolVersion)
	}
	if m.PackageHash != PackageHash(files) {
		t.Fatalf("package_hash %s is not the install-root aggregation %s", m.PackageHash, PackageHash(files))
	}
	if m.DefaultVariant != "linux-x86_64-wasm" {
		t.Fatalf("default_variant fallback = %q", m.DefaultVariant)
	}
	if m.CapabilityEnvelope == nil || len(m.CapabilityEnvelope) != 0 {
		t.Fatalf("capability_envelope must be the empty list, got %v", m.CapabilityEnvelope)
	}

	wasmSum := sha256.Sum256(wasm)
	v := m.Variants[0]
	if v.Entrypoint != "variants/linux-x86_64-wasm/plugin.wasm" {
		t.Fatalf("entrypoint %q", v.Entrypoint)
	}
	if v.ArtifactHash != "sha256:"+hex.EncodeToString(wasmSum[:]) {
		t.Fatalf("artifact_hash %s is not the entrypoint digest", v.ArtifactHash)
	}
	if want := `{"core":["com.example.hello.core@1"]}`; string(v.Implements) != want {
		t.Fatalf("implements = %s, want %s", v.Implements, want)
	}

	schemaSum := sha256.Sum256(descriptors)
	wantIfaces := `{"core":[{"id":"com.example.hello.core","methods":["core.echo"],"schema_hash":"sha256:` +
		hex.EncodeToString(schemaSum[:]) + `","version":1}]}`
	if string(m.Interfaces) != wantIfaces {
		t.Fatalf("interfaces = %s\nwant %s", m.Interfaces, wantIfaces)
	}

	// .
	gotHash, err := ManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &top); err != nil {
		t.Fatal(err)
	}
	delete(top, "package_hash")
	stripped, _ := json.Marshal(top)
	view, err := CanonicalizeV1(stripped)
	if err != nil {
		t.Fatal(err)
	}
	if want := SHA256Prefixed(view); gotHash != want {
		t.Fatalf("ManifestHash = %s, want %s", gotHash, want)
	}
}

func TestBuildManifestRefusesMissingStagedFiles(t *testing.T) {
	cfg := testConfig()
	if _, err := BuildManifest(cfg, []string{"op"}, map[string][]byte{}); err == nil || !strings.Contains(err.Error(), "schema file") {
		t.Fatalf("missing schema file: %v", err)
	}
	files := map[string][]byte{SchemaFileRel(cfg.Interface): []byte("[]")}
	if _, err := BuildManifest(cfg, []string{"op"}, files); err == nil || !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("missing entrypoint: %v", err)
	}
}

func TestDescriptorIDs(t *testing.T) {
	ids, err := DescriptorIDs([]byte(`[{"id":"a.x"},{"id":"b.y"}]`))
	if err != nil || strings.Join(ids, ",") != "a.x,b.y" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	for name, in := range map[string]string{
		"empty_array": `[]`,
		"not_array":   `{"id":"x"}`,
		"missing_id":  `[{"summary":"s"}]`,
	} {
		if _, err := DescriptorIDs([]byte(in)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// .
	// .
	// .
	// .
	// .
	// .
	seventeen := make([]string, 17)
	for i := range seventeen {
		seventeen[i] = `{"id":"op` + string(rune('a'+i)) + `"}`
	}
	ids, err = DescriptorIDs([]byte(`[` + strings.Join(seventeen, ",") + `]`))
	if err != nil || len(ids) != 17 {
		t.Fatalf("seventeen operations across interfaces are describable: %v %v", ids, err)
	}
	if _, err := PartitionMethods([]AuthorInterface{{ID: "one.iface", Version: 1}}, ids); err == nil {
		t.Fatal("seventeen methods under ONE interface must still be refused")
	}
}

// .
// .
// .
// .
// .
// .
// .
// .
func TestTwoInterfacesPartitionTheDescribedOperations(t *testing.T) {
	ifaces := []AuthorInterface{
		{ID: "speech.session", Version: 1, Methods: []string{"speech.session.open", "speech.session.close"}},
		{ID: "speaker.uid", Version: 1, Methods: []string{"speaker.enroll", "speaker.list"}},
	}
	ids := []string{"speaker.enroll", "speaker.list", "speech.session.close", "speech.session.open"}
	got, err := PartitionMethods(ifaces, ids)
	if err != nil {
		t.Fatalf("a clean partition must hold: %v", err)
	}
	if strings.Join(got["speaker.uid"], ",") != "speaker.enroll,speaker.list" || len(got["speech.session"]) != 2 {
		t.Fatalf("each interface keeps its own methods: %v", got)
	}

	for name, bad := range map[string][]AuthorInterface{
		"an operation in no interface": {
			{ID: "speech.session", Version: 1, Methods: []string{"speech.session.open", "speech.session.close"}},
			{ID: "speaker.uid", Version: 1, Methods: []string{"speaker.enroll"}},
		},
		"an operation in two": {
			{ID: "speech.session", Version: 1, Methods: []string{"speech.session.open", "speech.session.close", "speaker.list"}},
			{ID: "speaker.uid", Version: 1, Methods: []string{"speaker.enroll", "speaker.list"}},
		},
		"a method nothing describes": {
			{ID: "speech.session", Version: 1, Methods: []string{"speech.session.open", "speech.session.close"}},
			{ID: "speaker.uid", Version: 1, Methods: []string{"speaker.enroll", "speaker.list", "speaker.imagined"}},
		},
		"an interface naming nothing": {
			{ID: "speech.session", Version: 1, Methods: ids},
			{ID: "speaker.uid", Version: 1},
		},
	} {
		if _, err := PartitionMethods(bad, ids); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}

	// .
	// .
	emitted := []byte(`[{"id":"speaker.enroll","summary":"e","operator_confirms":true},{"id":"speaker.list","summary":"l"},{"id":"speech.session.open","summary":"o"}]`)
	sub, err := DescriptorsSubset(emitted, []string{"speaker.enroll", "speaker.list"})
	if err != nil {
		t.Fatal(err)
	}
	if string(sub) != `[{"id":"speaker.enroll","summary":"e","operator_confirms":true},{"id":"speaker.list","summary":"l"}]` {
		t.Fatalf("the subset preserves the emitted bytes verbatim: %s", sub)
	}
	if _, err := DescriptorsSubset(emitted, []string{"speaker.enroll", "not.described"}); err == nil {
		t.Fatal("a subset naming an operation the emission lacks must be refused")
	}
}

func TestAuthorConfigValidation(t *testing.T) {
	ok := testConfig()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*AuthorConfig)
		want   string
	}{
		{"bad_id", func(c *AuthorConfig) { c.ID = "Com.Example" }, "id grammar"},
		{"empty_version", func(c *AuthorConfig) { c.Version = "" }, "version"},
		{"bad_family", func(c *AuthorConfig) { c.PluginFamily = "gadget" }, "plugin_family"},
		{"bad_interface", func(c *AuthorConfig) { c.Interface.ID = "no_dots" }, "interface"},
		{"nil_envelope", func(c *AuthorConfig) { c.CapabilityEnvelope = nil }, "capability_envelope"},
		{"no_variants", func(c *AuthorConfig) { c.Variants = nil }, "variant"},
		{"bad_pairing", func(c *AuthorConfig) { c.Variants[0].AdmissionProfile = "standard" }, "forbids"},
		{"no_wasm_baseline", func(c *AuthorConfig) {
			c.Variants[0].ExecutionRuntime = "service_process"
			c.Variants[0].AdmissionProfile = "standard"
		}, "WASM baseline"},
		{"bad_predicate_class", func(c *AuthorConfig) {
			c.Requirements = &AuthorRequirements{Required: []string{"gpu:cuda"}}
		}, "requirement grammar"},
		{"unknown_default_variant", func(c *AuthorConfig) { c.DefaultVariant = "nope" }, "default_variant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadAuthorConfigClosedSurface(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "plugin.json", `{
		"id": "com.example.hello", "version": "0.1.0",
		"plugin_family": "tool_bridge",
		"interface": {"id": "com.example.hello.core", "version": 1},
		"capability_envelop": [],
		"variants": []
	}`)
	if _, err := LoadAuthorConfig(dir + "/plugin.json"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("typo'd member accepted: %v", err)
	}
}

// .
// .
// .
// .
func TestBuildManifestCarriesEveryDeclaredInterface(t *testing.T) {
	cfg := testConfig()
	cfg.Interface = AuthorInterface{}
	cfg.Interfaces = []AuthorInterface{
		{ID: "speech.session", Version: 1, Methods: []string{"speech.session.open"}},
		{ID: "speaker.uid", Version: 1, Methods: []string{"speaker.enroll", "speaker.list"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a two-interface config is valid: %v", err)
	}
	methods := []string{"speaker.enroll", "speaker.list", "speech.session.open"}
	install := map[string][]byte{
		"interfaces/speech.session.v1.schema.json": []byte(`[{"id":"speech.session.open"}]`),
		"interfaces/speaker.uid.v1.schema.json":    []byte(`[{"id":"speaker.enroll","operator_confirms":true},{"id":"speaker.list"}]`),
	}
	for _, v := range cfg.Variants {
		install[EntrypointRel(v)] = []byte("artifact-" + v.VariantID)
	}
	raw, err := BuildManifest(cfg, methods, install)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var m struct {
		Interfaces struct {
			Core []struct {
				ID         string   `json:"id"`
				SchemaHash string   `json:"schema_hash"`
				Methods    []string `json:"methods"`
			} `json:"core"`
		} `json:"interfaces"`
		Variants []struct {
			Implements struct {
				Core []string `json:"core"`
			} `json:"implements"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Interfaces.Core) != 2 {
		t.Fatalf("both interfaces are declared: %s", raw)
	}
	first, second := m.Interfaces.Core[0], m.Interfaces.Core[1]
	if first.ID != "speech.session" || len(first.Methods) != 1 || second.ID != "speaker.uid" || len(second.Methods) != 2 {
		t.Fatalf("each keeps its own methods, in declaration order: %+v", m.Interfaces.Core)
	}
	if first.SchemaHash == second.SchemaHash || first.SchemaHash == "" {
		t.Fatalf("different schemas hash differently: %+v", m.Interfaces.Core)
	}
	for _, v := range m.Variants {
		if len(v.Implements.Core) != 2 {
			t.Fatalf("every variant implements both: %+v", v.Implements)
		}
	}
	// .
	one := testConfig()
	install2 := map[string][]byte{SchemaFileRel(one.Interface): []byte(`[{"id":"a.x"}]`)}
	for _, v := range one.Variants {
		install2[EntrypointRel(v)] = []byte("artifact-" + v.VariantID)
	}
	if _, err := BuildManifest(one, []string{"a.x"}, install2); err != nil {
		t.Fatalf("the single-interface path is unchanged: %v", err)
	}
}
