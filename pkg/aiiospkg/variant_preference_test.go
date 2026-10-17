package aiiospkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func preferredConfig() *AuthorConfig {
	cfg := testConfig()
	cfg.AiiosMinVersion = VariantPreferenceMinHost
	cfg.Models = []ModelDecl{{Name: "m", URL: "https://example.com/m", SHA256: "0123456789012345678901234567890123456789012345678901234567890123", Size: 1}}
	cfg.Variants = []AuthorVariant{nativeVariant("full", "linux", "x86_64"), nativeVariant("small", "linux", "x86_64")}
	for i := range cfg.Variants {
		zero := int64(0)
		cfg.Variants[i].Accelerator = &AcceleratorProfile{OS: "linux", Arch: "x86_64", Backend: "cpu", Precision: "fp32", Models: []string{"m"}, MemoryBytes: 1, DeviceMemoryBytes: &zero, SessionLimit: 1, Fallback: "none"}
	}
	return cfg
}

func TestVariantSelectionSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/variant_selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Preference []struct {
			Name, Raw, Min string
			OK             bool
		}
		Accelerators []struct {
			Name     string
			Required []string
			OK       bool
		}
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Preference) < 14 || len(vectors.Accelerators) < 8 {
		t.Fatal("thin vectors")
	}
	for _, v := range vectors.Preference {
		t.Run(v.Name, func(t *testing.T) {
			cfg := preferredConfig()
			cfg.AiiosMinVersion = v.Min
			base, _ := json.Marshal(cfg)
			var written map[string]json.RawMessage
			json.Unmarshal(base, &written)
			if v.Raw != "" {
				written["variant_preference"] = json.RawMessage(v.Raw)
			}
			base, _ = json.Marshal(written)
			path := filepath.Join(t.TempDir(), "plugin.json")
			if err := os.WriteFile(path, base, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadAuthorConfig(path)
			if (err == nil) != v.OK {
				t.Fatalf("accepted=%v want=%v: %v", err == nil, v.OK, err)
			}
			if err != nil {
				return
			}
			files := testInstallFiles(got, []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`), []byte("native fixture"))
			emitted, err := BuildManifest(got, []string{"core.echo"}, files)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]json.RawMessage
			json.Unmarshal(emitted, &result)
			if _, present := result["variant_preference"]; present != (v.Raw != "") {
				t.Fatal("presence changed")
			}
		})
	}
	for _, v := range vectors.Accelerators {
		t.Run("accelerator/"+v.Name, func(t *testing.T) {
			cfg := preferredConfig()
			p := cfg.Variants[0].Accelerator
			p.RequiredAccelerators = v.Required
			if err := ValidateAcceleratorProfile("full", p); (err == nil) != v.OK {
				t.Fatalf("accepted=%v want=%v: %v", err == nil, v.OK, err)
			}
		})
	}
}

func TestPreferenceRequiresBoundNativeResources(t *testing.T) {
	for _, mutate := range []func(*AuthorConfig){
		func(c *AuthorConfig) { c.Variants[0].Accelerator = nil },
		func(c *AuthorConfig) { c.Variants[0].Accelerator.DeviceMemoryBytes = nil },
		func(c *AuthorConfig) { n := int64(1); c.Variants[0].Accelerator.DeviceMemoryBytes = &n },
		func(c *AuthorConfig) {
			c.Variants[0].Accelerator.RequiredAccelerators = []string{"cuda"}
			c.VariantPreference = nil
		},
	} {
		cfg := preferredConfig()
		cfg.VariantPreference = []string{"full", "small"}
		mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("unbounded resource declaration admitted")
		}
	}
	order := make([]string, 65)
	if err := validateVariantPreference(order, VariantPreferenceMinHost, map[string]bool{}); err == nil {
		t.Fatal("unbounded order admitted")
	}
}
