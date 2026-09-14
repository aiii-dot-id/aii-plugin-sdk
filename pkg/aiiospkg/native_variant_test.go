package aiiospkg

import (
	"strings"
	"testing"
)

// .
// .
// .
func nativeVariant(id, platform, arch string) AuthorVariant {
	return AuthorVariant{
		VariantID: id, Platform: platform, Arch: arch,
		Topology: "full_identity_host", ExecutionRuntime: "native_t3_component",
		AdmissionProfile: "platform_reserved", VariantCapabilities: []string{},
	}
}

// .
// .
// .
// .
func TestANativeOnlyT3PackageNeedsNoWASMBaseline(t *testing.T) {
	cfg := testConfig()
	cfg.Variants = []AuthorVariant{
		nativeVariant("darwin-arm64-native", "macos", "arm64"),
		nativeVariant("linux-x86_64-native", "linux", "x86_64"),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a native-only T3 package was refused: %v", err)
	}
}

// .
func TestTheMobileBundleNeedsNoWASMBaselineEither(t *testing.T) {
	cfg := testConfig()
	cfg.Variants = []AuthorVariant{{
		VariantID: "ios-arm64-inprocess", Platform: "ios", Arch: "arm64",
		Topology: "mobile_app_host", ExecutionRuntime: "inprocess_component",
		AdmissionProfile: "certified_native", VariantCapabilities: []string{},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a mobile in-process package was refused: %v", err)
	}
}

// .
// .
func TestAPackageWithANonT3VariantStillOwesTheBaseline(t *testing.T) {
	cfg := testConfig()
	cfg.Variants = []AuthorVariant{
		nativeVariant("darwin-arm64-native", "macos", "arm64"),
		{
			VariantID: "linux-x86_64-service", Platform: "linux", Arch: "x86_64",
			Topology: "full_identity_host", ExecutionRuntime: "service_process",
			AdmissionProfile: "standard", VariantCapabilities: []string{},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("a package with a non-T3 variant was let off the wasm baseline")
	}
	if !strings.Contains(err.Error(), "WASM baseline") {
		t.Fatalf("the refusal does not name the rule: %v", err)
	}
}

// .
// .
// .
func TestEntrypointIsNamedForWhatItIs(t *testing.T) {
	cases := map[string]AuthorVariant{
		"variants/v/plugin.wasm": {VariantID: "v", ExecutionRuntime: "wasm_component", Platform: "linux"},
		"variants/w/plugin.exe":  {VariantID: "w", ExecutionRuntime: "native_t3_component", Platform: "windows"},
		"variants/x/plugin":      {VariantID: "x", ExecutionRuntime: "native_t3_component", Platform: "macos"},
		"variants/y/plugin":      {VariantID: "y", ExecutionRuntime: "service_process", Platform: "linux"},
	}
	for want, v := range cases {
		if got := EntrypointRel(v); got != want {
			t.Errorf("%s variant on %s: entrypoint %q, want %q", v.ExecutionRuntime, v.Platform, got, want)
		}
	}
}

// .
// .
func TestOnlyWASMVariantsCanFallBackToTheBuiltModule(t *testing.T) {
	wasm := AuthorVariant{ExecutionRuntime: "wasm_component"}
	aot := AuthorVariant{ExecutionRuntime: "wasm_aot_component"}
	native := nativeVariant("n", "macos", "arm64")
	inproc := AuthorVariant{ExecutionRuntime: "inprocess_component"}
	svc := AuthorVariant{ExecutionRuntime: "service_process"}

	if wasm.RequiresOwnArtifact() || aot.RequiresOwnArtifact() {
		t.Error("a wasm variant should be able to use the built module")
	}
	for _, v := range []AuthorVariant{native, inproc, svc} {
		if !v.RequiresOwnArtifact() {
			t.Errorf("%s would be handed a wasm module", v.ExecutionRuntime)
		}
	}
}

// .
// .
func TestEachVariantGetsItsOwnEntrypointPath(t *testing.T) {
	cfg := testConfig()
	cfg.Variants = []AuthorVariant{
		nativeVariant("darwin-arm64-native", "macos", "arm64"),
		nativeVariant("linux-x86_64-native", "linux", "x86_64"),
	}
	seen := map[string]bool{}
	for _, v := range cfg.Variants {
		e := EntrypointRel(v)
		if seen[e] {
			t.Fatalf("two variants share the entrypoint path %s — they cannot carry different bytes", e)
		}
		seen[e] = true
	}
}
