package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// nativeSkelTree is examples/native-skel as a module of its own, its
// carrier for this machine built as build.sh builds it, with plugin.json
// rewritten by edit when one is given. It skips where the skeleton declares
// no native variant for this machine.
func nativeSkelTree(t *testing.T, edit func(doc map[string]any, variant map[string]any)) string {
	t.Helper()
	dir := exampleModule(t, "native-skel", "main.go", "plugin.json")
	cfg, err := aiiospkg.LoadAuthorConfig(filepath.Join(dir, aiiospkg.AuthorFileName))
	if err != nil {
		t.Fatal(err)
	}
	variant := nativeVariant(cfg)
	if variant == nil {
		t.Skip("the native skeleton declares no native variant for this machine")
	}
	if edit != nil {
		raw, err := os.ReadFile(filepath.Join(dir, aiiospkg.AuthorFileName))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for _, v := range doc["variants"].([]any) {
			if v := v.(map[string]any); v["variant_id"] == variant.VariantID {
				edit(doc, v)
			}
		}
		if raw, err = json.MarshalIndent(doc, "", "  "); err != nil {
			t.Fatal(err)
		}
		writeAt(t, dir, aiiospkg.AuthorFileName, raw)
	}
	gobin := os.Getenv("GO")
	if gobin == "" {
		gobin = "go"
	}
	carrier := filepath.Join(dir, filepath.FromSlash(variant.Artifact))
	if err := os.Remove(carrier); err != nil {
		t.Fatal(err)
	}
	build := exec.Command(gobin, "build", "-o", carrier, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-buildvcs=false", "GOPROXY=off")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the native skeleton: %v\n%s", err, b)
	}
	return dir
}

// aiisdkTestHere runs the public command, `aiisdk test`, in dir and returns
// its exit status and the report it wrote.
func aiisdkTestHere(t *testing.T, dir string, args ...string) (int, map[string]checkEntry) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qual.json")
	var code int
	inDir(t, dir, func() { code = cmdTest(append([]string{"-report", path}, args...)) })
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env qualEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	byName := map[string]checkEntry{}
	for _, e := range env.Checks {
		byName[e.Name] = e
	}
	return code, byName
}

// THE DOCUMENTED PLAIN NATIVE PLUGIN QUALIFIES. examples/native-skel
// declares no accelerator profile and ends in Serve("child-ready"), as Serve's documentation and
// WRITING_A_PLUGIN tell an ordinary native plugin to; a host waits for no
// line of it. `aiisdk test` waited for event=ready and then for sdk=, and
// refused the healthy skeleton after its whole start timeout.
func TestThePlainNativeSkeletonQualifies(t *testing.T) {
	dir := nativeSkelTree(t, nil)
	code, byName := aiisdkTestHere(t, dir, "-timeout", "20s")
	for _, name := range []string{"package", "readiness", "exit"} {
		if e := byName[name]; e.Status != "pass" {
			t.Errorf("%s: %+v", name, e)
		}
	}
	if code != exitPass {
		t.Fatalf("aiisdk test on the documented plain native skeleton exits %d: %+v", code, byName)
	}
}

// A PROFILED VARIANT IS STILL HELD TO ITS READY LINE. The same carrier
// under a variant that declares an accelerator profile is one a host
// refuses: its plain mark is no event=ready line. `aiisdk test` refuses it
// too, at readiness, and runs nothing after.
func TestAProfiledVariantWithAPlainMarkIsRefused(t *testing.T) {
	dir := nativeSkelTree(t, func(doc, variant map[string]any) {
		doc["aiios_min_version"] = "0.1.15"
		doc["models"] = []any{map[string]any{"name": "base", "url": "https://example.com/base", "sha256": strings.Repeat("a", 64), "size": 1024}}
		variant["accelerator"] = map[string]any{"os": variant["platform"], "arch": variant["arch"], "backend": "cpu", "precision": "fp16",
			"models": []any{"base"}, "memory_bytes": 1048576, "session_limit": 1, "fallback": "none"}
	})
	code, byName := aiisdkTestHere(t, dir, "-timeout", "2s")
	if e := byName["package"]; e.Status != "pass" {
		t.Fatalf("the profiled tree packs: %+v", e)
	}
	if e := byName["readiness"]; e.Status != "fail" || !strings.Contains(e.Detail, "no ready banner") {
		t.Fatalf("a profiled variant that prints a plain mark is refused at readiness: %+v", e)
	}
	if _, ran := byName["exit"]; ran || code != exitFail {
		t.Fatalf("the refused carrier was run on (exit %d): %+v", code, byName)
	}
}

// exampleModule copies an example's sources into a fresh module beside
// this checkout's kit (replace), with fake bytes at every artifact
// plugin.json names, and returns its directory.
func exampleModule(t *testing.T, example string, files ...string) string {
	t.Helper()
	kit, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(kit, "examples", example)
	dir := t.TempDir()
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		writeAt(t, dir, rel, b)
	}
	writeAt(t, dir, "go.mod", []byte(goModTemplate("example.com/"+strings.ReplaceAll(example, "-", ""), kit, "")))
	sum, err := os.ReadFile(filepath.Join(kit, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, dir, "go.sum", sum)
	cfg, err := aiiospkg.LoadAuthorConfig(filepath.Join(dir, aiiospkg.AuthorFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range cfg.Variants {
		writeAt(t, dir, v.Artifact, []byte("carrier for "+v.VariantID))
	}
	return dir
}

func writeAt(t *testing.T, dir, rel string, b []byte) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readinessOf runs the check child under the name artifact as the carrier
// of one variant here, with the family and profile given, and returns the
// report's readiness entry and whether anything ran after it.
func readinessOf(t *testing.T, child, artifact, family string, profile *aiiospkg.AcceleratorProfile, timeout time.Duration) (checkEntry, bool) {
	t.Helper()
	platform, arch := hostPlatformArch()
	dir := t.TempDir()
	raw, err := os.ReadFile(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName(artifact)), raw, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "org.example.checked", Version: "0.1.0", PluginFamily: family,
		Variants: []aiiospkg.AuthorVariant{{VariantID: "native-here", Platform: platform, Arch: arch, ExecutionRuntime: "native_t3_component", Artifact: exeName(artifact), Accelerator: profile}}}
	h, err := newHarness(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rep := &report{}
	runNative(dir, cfg, rep, h, &cfg.Variants[0], timeout, "", "")
	if len(rep.entries) == 0 || rep.entries[0].Name != "readiness" {
		t.Fatalf("readiness is reported first: %+v", rep.entries)
	}
	return rep.entries[0], len(rep.entries) > 1
}

// AN AWAITED LINE IS HELD TO WHAT A HOST HOLDS IT TO, AND NO MORE. A host
// requires the three fields of a profiled variant's ready line and never
// sdk=: a line with the three and no sdk= passes, and one that names the
// kit but lacks the three is refused, naming what it lacks.
func TestAProfiledLineIsHeldToTheHostsFieldsNotToTheKit(t *testing.T) {
	child := buildCheckChild(t)
	profile := &aiiospkg.AcceleratorProfile{}
	e, _ := readinessOf(t, child, "child-nokit", "", profile, 20*time.Second)
	if e.Status != "pass" || !strings.Contains(e.Detail, "models_loaded=1") || strings.Contains(e.Detail, "sdk=") {
		t.Fatalf("a profiled line with the three fields and no sdk= is ready: %+v", e)
	}
	e, ranOn := readinessOf(t, child, "child-nofields", "", profile, 20*time.Second)
	if e.Status != "fail" || ranOn {
		t.Fatalf("a profiled line without the three fields is refused, and nothing runs after: %+v (ran on %v)", e, ranOn)
	}
	for _, f := range []string{"models_loaded=", "accelerator=", "probe_ms="} {
		if !strings.Contains(e.Detail, f) {
			t.Errorf("the refusal names %s: %s", f, e.Detail)
		}
	}
}

// A RESIDENT ENGINE IS AWAITED, PROFILE OR NONE. A model-bearing engine's
// readiness is not weakened: a voice_interface carrier with no profile that
// prints only a plain mark is refused at readiness, where an ordinary
// native plugin with the same mark would be started and sent its checks.
func TestAResidentEngineWithNoProfileIsStillAwaited(t *testing.T) {
	child := buildCheckChild(t)
	e, ranOn := readinessOf(t, child, "child-plain", "voice_interface", nil, 2*time.Second)
	if e.Status != "fail" || !strings.Contains(e.Detail, "no ready banner") || ranOn {
		t.Fatalf("a resident engine with no profile and a plain mark is refused at readiness: %+v (ran on %v)", e, ranOn)
	}
	e, _ = readinessOf(t, child, "child-plain", "", nil, 2*time.Second)
	if e.Status != "pass" {
		t.Fatalf("the same mark from an ordinary native plugin is no refusal: %+v", e)
	}
}

func TestReadyRefusal(t *testing.T) {
	for _, c := range []struct {
		banner   string
		profiled bool
		refused  bool
	}{
		{"m event=ready models_loaded=4 accelerator=mlx probe_ms=38 sdk=v0.1.24", true, false},
		{"m event=ready models_loaded=4 accelerator=mlx probe_ms=38", true, false},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=0", true, false},
		{"m event=ready models_loaded=0 accelerator=cpu probe_ms=38", true, true},
		{"m event=ready models_loaded=-1 accelerator=cpu probe_ms=38", true, true},
		{"m event=ready models_loaded=unknown accelerator=cpu probe_ms=38", true, true},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=-1", true, true},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=unknown", true, true},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=38 models_loaded=0", true, true},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=38 accelerator=", true, true},
		// Match the host's field parsing: a malformed later duration does not
		// erase an earlier valid duration; a later valid value does replace it.
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=38 probe_ms=unknown", true, false},
		{"m event=ready models_loaded=4 accelerator=cpu probe_ms=38 probe_ms=-1", true, true},
		{"m event=ready models_loaded=4 accelerator= probe_ms=38", true, true},
		{"m event=ready sdk=v0.1.24", true, true},
		{"m event=ready", false, false},
	} {
		if got := readyRefusal(c.banner, c.profiled) != ""; got != c.refused {
			t.Errorf("%q (profiled %v): refused %v, want %v", c.banner, c.profiled, got, c.refused)
		}
	}
}
