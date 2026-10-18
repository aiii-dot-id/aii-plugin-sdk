//go:build e2e

package e2e

// `aiisdk test` qualifies the bytes the package carries, and no others.
//
// A variant may name its own artifact, and `aiisdk package` then packs
// those bytes for it; `aiisdk test` ran the module `aiisdk build` left in
// dist/ and reported the result as that package's. Matching descriptors
// say nothing about behaviour, so a package could pass on bytes it does
// not carry. Two modules here describe
// themselves identically and answer core.echo differently; the variant
// names the one that does not echo, so a run that tests what is packaged
// fails the scaffold's echo case, and says which variant's bytes it ran.
//
// Environment knobs: TINYGO, AII_OS_DIR, AII_OS_GO (see e2e_test.go).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAiisdkTestRunsTheBytesThePackageCarries(t *testing.T) {
	tinygo := findTinygo(t)
	cli := buildCLI(t)
	aii := buildVerifyOracle(t)
	worker := buildWorker(t)
	if filepath.Dir(aii) != filepath.Dir(worker) {
		t.Fatalf("rig: the oracles are built apart (%s, %s)", aii, worker)
	}
	oracles := filepath.Dir(aii)

	work := t.TempDir()
	runIn(t, work, nil, cli, "init", "-sdk", repoRoot(t), "twin")
	dir := filepath.Join(work, "twin")
	build := []string{"TINYGO=" + tinygo, "GOPROXY=off"}
	built := filepath.Join(dir, "dist", "twin.wasm")

	// The module `aiisdk build` leaves in dist/: the scaffold's echo.
	runIn(t, dir, build, cli, "build")
	echoes := readFile(t, built)

	// Its twin: the same descriptors, and core.echo answers otherwise.
	mainGo := filepath.Join(dir, "main.go")
	src := string(readFile(t, mainGo))
	const echo = "return c.Arguments, nil"
	if !strings.Contains(src, echo) {
		t.Fatalf("rig: the scaffold's echo handler moved:\n%s", src)
	}
	writeFile(t, mainGo, []byte(strings.Replace(src, echo, `return map[string]string{"answered_by": "the packaged twin"}, nil`, 1)))
	runIn(t, dir, build, cli, "build")
	writeFile(t, filepath.Join(dir, "twin-variant.wasm"), readFile(t, built))
	writeFile(t, mainGo, []byte(src))
	writeFile(t, built, echoes)

	// The variant names the twin as its own artifact: the package carries it.
	cfgPath := filepath.Join(dir, "plugin.json")
	cfg := string(readFile(t, cfgPath))
	const caps = `"variant_capabilities": []`
	if strings.Count(cfg, caps) != 1 {
		t.Fatalf("rig: the scaffold's one variant moved:\n%s", cfg)
	}
	writeFile(t, cfgPath, []byte(strings.Replace(cfg, caps, caps+`,
      "artifact": "twin-variant.wasm"`, 1)))

	cmd := exec.Command(cli, "test", "-skip-build")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "AII_OS_BIN="+oracles, "GOPROXY=off")
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	t.Logf("aiisdk test:\n%s", out)
	// The rig: the package carries the twin, and the twin is not the echo.
	staged, _ := filepath.Glob(filepath.Join(dir, "dist", "pkg", "twin-0.1.0", "install-root", "variants", "*", "plugin.wasm"))
	twin := readFile(t, filepath.Join(dir, "twin-variant.wasm"))
	if len(staged) != 1 || string(readFile(t, staged[0])) != string(twin) || string(twin) == string(echoes) {
		t.Fatalf("rig: the package must carry the twin, and the twin differ from the echo (staged %v)", staged)
	}
	if err == nil || strings.Contains(out, "RESULT: PASS") {
		t.Fatalf("aiisdk test passed the package on the echo of a module it does not carry (err=%v)", err)
	}
	if !strings.Contains(out, "FAIL  case") || !strings.Contains(out, "answered_by") {
		t.Fatalf("the echo case must fail on the packaged twin's own answer:\n%s", out)
	}
	if !strings.Contains(out, "PASS  account") {
		t.Fatalf("the twin describes itself as the package does; the account must pass on it:\n%s", out)
	}
	if !strings.Contains(out, "artifact") || !strings.Contains(out, "install-root/variants/") || !strings.Contains(out, "twin-variant.wasm") {
		t.Fatalf("the report must name the variant whose packaged bytes it ran, and where they came from:\n%s", out)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
