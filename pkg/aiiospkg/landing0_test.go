package aiiospkg

import (
	"encoding/json"
	"strings"
	"testing"
)

func i64(n int64) *int64 { return &n }

// .
// .
// .
func TestAProfileMayDeclareItsReservationAndItsAllowance(t *testing.T) {
	good := func() *AcceleratorProfile {
		return &AcceleratorProfile{OS: "linux", Arch: "x86_64", Backend: "cpu", Precision: "fp16",
			Models: []string{"m"}, MemoryBytes: 8589934592, SessionLimit: 2, Fallback: "none"}
	}
	p := good()
	if err := ValidateAcceleratorProfile("v1", p); err != nil {
		t.Fatalf("a profile without the optional declarations must pass: %v", err)
	}
	p.StartupMS, p.DeviceMemoryBytes = i64(180000), i64(4294967296)
	if err := ValidateAcceleratorProfile("v1", p); err != nil {
		t.Fatalf("a declared allowance and device reservation must pass: %v", err)
	}
	// .
	p.DeviceMemoryBytes = i64(0)
	if err := ValidateAcceleratorProfile("v1", p); err != nil {
		t.Fatalf("an explicit zero device reservation must pass: %v", err)
	}
	for _, tc := range []struct {
		name  string
		spoil func(*AcceleratorProfile)
		want  string
	}{
		{"an allowance of zero", func(p *AcceleratorProfile) { p.StartupMS = i64(0) }, "startup_ms"},
		{"a negative allowance", func(p *AcceleratorProfile) { p.StartupMS = i64(-1) }, "startup_ms"},
		{"an allowance beyond the bound", func(p *AcceleratorProfile) { p.StartupMS = i64(MaxStartupMS + 1) }, "startup_ms"},
		{"a negative device reservation", func(p *AcceleratorProfile) { p.DeviceMemoryBytes = i64(-1) }, "device_memory_bytes"},
		{"an undeclared reservation", func(p *AcceleratorProfile) { p.MemoryBytes = 0 }, "declared"},
	} {
		spoiled := good()
		tc.spoil(spoiled)
		err := ValidateAcceleratorProfile("v1", spoiled)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want a refusal naming %q)", tc.name, err, tc.want)
		}
	}

	// .
	// .
	raw, ok, err := AcceleratorJSON([]AuthorVariant{{VariantID: "v1", Accelerator: good()}})
	if err != nil || !ok {
		t.Fatalf("emit: %v", err)
	}
	if strings.Contains(string(raw), "device_memory_bytes") || strings.Contains(string(raw), "startup_ms") {
		t.Fatalf("an undeclared field was emitted: %s", raw)
	}
	full := good()
	full.StartupMS, full.DeviceMemoryBytes = i64(180000), i64(0)
	raw, _, err = AcceleratorJSON([]AuthorVariant{{VariantID: "v1", Accelerator: full}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["v1"]["startup_ms"] != float64(180000) {
		t.Fatalf("the declared allowance was not emitted: %s", raw)
	}
	if v, present := out["v1"]["device_memory_bytes"]; !present || v != float64(0) {
		t.Fatalf("an explicit zero must survive the packager: %s", raw)
	}
}

// .
// .
func TestTheHostWindowIsAuthoredCheckedAndEmitted(t *testing.T) {
	cfg := testConfig()
	cfg.AiiosMinVersion, cfg.AiiosMaxExclusiveVersion = "0.1.6", "0.3.0"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a window the host admits must pass here: %v", err)
	}
	files := testInstallFiles(cfg, []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`), []byte("\x00asm-fake-module"))
	manifest, err := BuildManifest(cfg, []string{"core.echo"}, files)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	if m["aiios_min_version"] != "0.1.6" || m["aiios_max_exclusive_version"] != "0.3.0" {
		t.Fatalf("the window did not reach the manifest: %s", manifest)
	}

	// .
	plain := testConfig()
	manifest, err = BuildManifest(plain, []string{"core.echo"}, testInstallFiles(plain, []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`), []byte("\x00asm-fake-module")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "aiios_min_version") {
		t.Fatalf("an undeclared window was emitted: %s", manifest)
	}

	for _, tc := range []struct{ name, min, max, want string }{
		{"a minimum that is not a version", "one.two.three", "", "aiios_min_version"},
		{"a maximum that is not a version", "", "0.2", "aiios_max_exclusive_version"},
		{"a window with no versions in it", "0.3.0", "0.3.0", "empty"},
		{"a window that runs backwards", "0.4.0", "0.2.0", "empty"},
	} {
		spoiled := testConfig()
		spoiled.AiiosMinVersion, spoiled.AiiosMaxExclusiveVersion = tc.min, tc.max
		err := spoiled.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want a refusal naming %q)", tc.name, err, tc.want)
		}
	}
}
