package aiiospkg

import (
	"strings"
	"testing"
)

// .
// .
// .
func TestNativeDeclarationsAreHeldToTheHostsBounds(t *testing.T) {
	prof := &AcceleratorProfile{OS: "macos", Arch: "arm64", Backend: "mlx", Operators: []string{"attention"}, RuntimeLibraries: []string{"mlx-0.20"}, Precision: "int8", Models: []string{"stt-int8.bin"}, MemoryBytes: 6 << 30, SessionLimit: 1, Fallback: "reported"}
	cfg := &AuthorConfig{ID: "com.example.engine", Version: "0.1.0", PluginFamily: "voice_interface",
		Interface:          AuthorInterface{ID: "speech.engine", Version: 1},
		CapabilityEnvelope: []string{},
		Models:             []ModelDecl{{Name: "stt-int8.bin", URL: "https://models.example.test/stt.bin", SHA256: strings.Repeat("a", 64), Size: 1 << 20}},
		Variants: []AuthorVariant{{VariantID: "macos-arm64-native", Platform: "macos", Arch: "arm64", Topology: "full_identity_host",
			ExecutionRuntime: "native_t3_component", AdmissionProfile: "platform_reserved", VariantCapabilities: []string{}, Artifact: "dist/engine", Accelerator: prof}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a good native config validates: %v", err)
	}
	accel, present, err := AcceleratorJSON(cfg.Variants)
	if err != nil || !present {
		t.Fatalf("accelerator member: %v %v", err, present)
	}
	if !strings.HasPrefix(string(accel), `{"macos-arm64-native":{"arch":"arm64","backend":"mlx","fallback":"reported","memory_bytes":6442450944,"models":["stt-int8.bin"]`) {
		t.Fatalf("canonical member: %s", accel)
	}
	models, err := ModelsJSON(cfg.Models)
	if err != nil || string(models) != `[{"name":"stt-int8.bin","sha256":"`+strings.Repeat("a", 64)+`","size":1048576,"url":"https://models.example.test/stt.bin"}]` {
		t.Fatalf("models member: %v %s", err, models)
	}

	wasm := *cfg
	wasm.Variants = []AuthorVariant{{VariantID: "linux-x86_64-wasm", Platform: "linux", Arch: "x86_64", Topology: "full_identity_host", ExecutionRuntime: "wasm_component", AdmissionProfile: "wasm_sandbox", VariantCapabilities: []string{}, Accelerator: prof}}
	if err := wasm.Validate(); err == nil || !strings.Contains(err.Error(), "only a native_t3_component") {
		t.Fatalf("a wasm variant carries no accelerator: %v", err)
	}
	unknown := *cfg
	up := *prof
	up.Models = []string{"absent.bin"}
	unknown.Variants = []AuthorVariant{cfg.Variants[0]}
	unknown.Variants[0].Accelerator = &up
	if err := unknown.Validate(); err == nil || !strings.Contains(err.Error(), "which models does not declare") {
		t.Fatalf("a profile's models are declared: %v", err)
	}
	for name, bad := range map[string]*AcceleratorProfile{
		"no models":       {OS: "macos", Arch: "arm64", Backend: "mlx", Precision: "int8", Models: nil, MemoryBytes: 1, SessionLimit: 1, Fallback: "none"},
		"silent fallback": {OS: "macos", Arch: "arm64", Backend: "mlx", Precision: "int8", Models: []string{"m"}, MemoryBytes: 1, SessionLimit: 1, Fallback: "silent"},
		"unmeasured":      {OS: "macos", Arch: "arm64", Backend: "mlx", Precision: "int8", Models: []string{"m"}, MemoryBytes: 0, SessionLimit: 1, Fallback: "none"},
	} {
		if err := ValidateAcceleratorProfile("v", bad); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if err := ValidateModels([]ModelDecl{{Name: "m", URL: "http://x/m", SHA256: strings.Repeat("a", 64), Size: 1}}); err == nil {
		t.Fatal("an http model url is refused")
	}
}

// .
// .
func TestAModelPathIsPortableOrRefused(t *testing.T) {
	ok := func(name, path string) ModelDecl {
		return ModelDecl{Name: name, Path: path, URL: "https://x/" + name, SHA256: strings.Repeat("a", 64), Size: 1}
	}
	good := []ModelDecl{ok("stt.config", "stt/config.json"), ok("stt.weights", "stt/model.safetensors"), ok("tts.tok", "tts/speech_tokenizer/tokenizer.json"), ok("vad", "")}
	// .
	// .
	if err := ValidateModels([]ModelDecl{ok("stt-gitattributes", "stt/.gitattributes")}); err != nil {
		t.Fatalf("a leading dot is a portable file name: %v", err)
	}
	if err := ValidateModels(good); err != nil {
		t.Fatalf("directory shapes validate: %v", err)
	}
	if b, err := ModelsJSON(good[:1]); err != nil || string(b) != `[{"name":"stt.config","path":"stt/config.json","sha256":"`+strings.Repeat("a", 64)+`","size":1,"url":"https://x/stt.config"}]` {
		t.Fatalf("the path travels in the member: %v %s", err, b)
	}
	for name, tc := range map[string]struct {
		decls []ModelDecl
		want  string
	}{
		"traversal": {[]ModelDecl{ok("m", "../m")}, "portable file name"},
		"dot only":  {[]ModelDecl{ok("m", "stt/./m")}, "portable file name"},
		"absolute":  {[]ModelDecl{ok("m", "/etc/m")}, "portable file name"},
		"backslash": {[]ModelDecl{ok("m", `a\b`)}, "portable file name"},
		"drive":     {[]ModelDecl{ok("m", "C:/m")}, "portable file name"},
		"stream":    {[]ModelDecl{ok("m", "m:zone")}, "portable file name"},
		"dot end":   {[]ModelDecl{ok("m", "stt./m")}, "portable file name"},
		"device":    {[]ModelDecl{ok("m", "stt/NUL.bin")}, "device name"},
		"partial":   {[]ModelDecl{ok("m", "m.partial")}, "suffix is the host's"},
		"deep":      {[]ModelDecl{ok("m", "a/b/c/d/e/f/g/h/i")}, "deeper than 8"},
		"case":      {[]ModelDecl{ok("m", "STT/config.json"), ok("n", "stt/Config.json")}, "case-insensitive"},
		"directory": {[]ModelDecl{ok("m", "stt"), ok("n", "stt/config.json")}, "is a directory of"},
		"too many":  {make([]ModelDecl, MaxModels+1), "at most"},
	} {
		if err := ValidateModels(tc.decls); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
	big := &AcceleratorProfile{OS: "linux", Arch: "x86_64", Backend: "cuda", Precision: "fp16", Models: make([]string, MaxProfileModels+1), MemoryBytes: 1, SessionLimit: 1, Fallback: "none"}
	for i := range big.Models {
		big.Models[i] = "m"
	}
	if err := ValidateAcceleratorProfile("v", big); err == nil || !strings.Contains(err.Error(), "128 models") {
		t.Fatalf("a profile names at most %d models: %v", MaxProfileModels, err)
	}
}
