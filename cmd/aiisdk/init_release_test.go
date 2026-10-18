package main

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A scaffold wired to a RELEASE builds straight out of init: the kit is
// fetched from its origin and go.sum written. Proven offline against a
// module proxy served from this checkout as v0.1.0, with the kit's own
// dependencies taken from the module cache that built it — the same
// resolution an author's `go install ...@v0.1.0` then `aiisdk init`
// performs once the release exists.
func TestAScaffoldWiredToAReleaseBuildsStraightOutOfInit(t *testing.T) {
	proveScaffoldBuildsFrom(t, "v0.1.0")
}

// AN UNTAGGED ORIGIN IS AN ORIGIN. `go install ...@latest` against a
// repository with no release tag installs the commit's pseudo-version
// with its module sum; a scaffold wired to that version fetches it the
// same way. Before this proof, init refused such a binary and told the
// author to run the command they had just run.
func TestAScaffoldWiredToAFetchedCommitBuildsStraightOutOfInit(t *testing.T) {
	proveScaffoldBuildsFrom(t, "v0.0.0-20000102030405-0123456789ab")
}

func proveScaffoldBuildsFrom(t *testing.T, version string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	kit, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	proxy := t.TempDir()
	if err := serveKitAsRelease(kit, proxy, version); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	// The toolchain extracts modules read-only; its own clean takes them
	// out before the temp dir's cleanup would fail on them.
	t.Cleanup(func() {
		clean := exec.Command("go", "clean", "-modcache")
		clean.Env = append(os.Environ(), "GOMODCACHE="+cache, "GOFLAGS=")
		_ = clean.Run()
	})
	real := strings.TrimSpace(goEnv(t, "GOMODCACHE"))
	for _, dep := range kitDependencies(t, kit) {
		src := filepath.Join(real, "cache", "download", dep.path, "@v")
		dst := filepath.Join(cache, "cache", "download", dep.path, "@v")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		copied := 0
		for _, suffix := range []string{".info", ".mod", ".zip", ".ziphash"} {
			data, err := os.ReadFile(filepath.Join(src, dep.version+suffix))
			if err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(dst, dep.version+suffix), data, 0o644); err != nil {
				t.Fatal(err)
			}
			copied++
		}
		if copied < 2 {
			t.Skipf("the module cache that built this checkout lacks %s@%s; run `go mod download` in the kit first", dep.path, dep.version)
		}
	}
	t.Setenv("GOPROXY", fileURL(proxy))
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GONOSUMDB", "github.com/aiii-dot-id")
	t.Setenv("GONOSUMCHECK", "1")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "")

	dir := filepath.Join(t.TempDir(), "com.example.hello")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go.mod":  goModTemplate("com.example.hello", "", version),
		"main.go": mainGoTemplate("com.example.hello"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := resolveModules(dir); err != nil {
		t.Fatalf("the release could not be resolved from its origin: %v", err)
	}
	sum, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil || !strings.Contains(string(sum), sdkModulePath+" "+version) {
		t.Fatalf("go.sum names the kit at the release: %v\n%s", err, sum)
	}
	if err := ensureModules(dir); err != nil {
		t.Fatalf("a resolved scaffold needs nothing more: %v", err)
	}
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the scaffold does not build: %v\n%s", err, out)
	}
	// And a go.sum that went missing is resolved again by the build's guard.
	if err := os.Remove(filepath.Join(dir, "go.sum")); err != nil {
		t.Fatal(err)
	}
	if err := ensureModules(dir); err != nil {
		t.Fatalf("the build's guard resolves a missing go.sum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.sum")); err != nil {
		t.Fatal("go.sum was not written back")
	}
}

// fileURL names a directory as the go command reads a file:// GOPROXY:
// the path after three slashes, a Windows drive inside it (file:///C:/…).
// "file://"+path would put the drive in the host field (file://C:/…),
// which the go command refuses before it reads anything.
func fileURL(dir string) string {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

type moduleDep struct{ path, version string }

// kitDependencies reads the kit's own requirements, which the scaffold's
// module graph carries.
func kitDependencies(t *testing.T, kit string) []moduleDep {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(kit, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var deps []moduleDep
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "require"))
		f := strings.Fields(line)
		if len(f) >= 2 && strings.Contains(f[0], ".") && strings.HasPrefix(f[1], "v") {
			deps = append(deps, moduleDep{f[0], f[1]})
		}
	}
	return deps
}

func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// serveKitAsRelease lays this checkout out as a GOPROXY directory
// serving one version: list, .info, .mod and the module zip.
func serveKitAsRelease(kit, proxy, version string) error {
	base := filepath.Join(proxy, filepath.FromSlash(sdkModulePath), "@v")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	gomod, err := os.ReadFile(filepath.Join(kit, "go.mod"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, "list"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, version+".info"), []byte(`{"Version":"`+version+`","Time":"2026-09-06T00:00:00Z"}`), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, version+".mod"), gomod, 0o644); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	prefix := sdkModulePath + "@" + version + "/"
	err = filepath.WalkDir(kit, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(kit, path)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", ".keys", "testdata":
				if rel != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") && rel != "go.mod" && rel != "go.sum" && rel != "LICENSE" {
			return nil
		}
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		w, err := zw.Create(prefix + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(base, version+".zip"), buf.Bytes(), 0o644)
}
