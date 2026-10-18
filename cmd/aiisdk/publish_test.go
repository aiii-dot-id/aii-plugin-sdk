package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// Build an actual canonical SDK archive, not arbitrary "PACKAGE-BYTES".
// A passing catalog test must exercise the same artifact seam as publish.
func writeCatalogFixture(t *testing.T, cfg *aiiospkg.AuthorConfig, path string) []byte {
	t.Helper()
	cfg.PluginFamily = "tool_bridge"
	cfg.Interface = aiiospkg.AuthorInterface{ID: "com.example.core", Version: 1}
	files := map[string][]byte{aiiospkg.SchemaFileRel(cfg.Interface): []byte(`[{"id":"core.echo"}]`)}
	for i := range cfg.Variants {
		if cfg.Variants[i].VariantID == "" {
			cfg.Variants[i].VariantID = fmt.Sprintf("variant-%d", i)
		}
		files[aiiospkg.EntrypointRel(cfg.Variants[i])] = []byte("fixture executable")
	}
	manifest, err := aiiospkg.BuildManifest(cfg, []string{"core.echo"}, files)
	if err != nil {
		t.Fatal(err)
	}
	tree := aiiospkg.NewTree(cfg.ID + "-" + cfg.Version)
	tree.Add("manifest.json", manifest)
	for path, data := range files {
		tree.Add("install-root/"+path, data)
	}
	body, err := aiiospkg.WriteBundle(tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestBuildCatalogEntryPortableWasm(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.mem-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	body := writeCatalogFixture(t, cfg, pkg)
	e, err := buildCatalogEntry(cfg, "", dir, "https://h/mem.aiiospkg", "T2", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "com.x.mem" || e.Version != "1.0.0" || e.Tier != "T2" || e.Summary != "Mem" {
		t.Fatalf("entry meta wrong: %+v", e)
	}
	if len(e.Packages) != 1 || e.Packages[0].Platform != "*" || e.Packages[0].Arch != "*" {
		t.Fatalf("a WASM plugin publishes one portable package: %+v", e.Packages)
	}
	sum := sha256.Sum256(body)
	if e.Packages[0].SHA256 != "sha256:"+hex.EncodeToString(sum[:]) || e.Packages[0].Size != int64(len(body)) {
		t.Fatalf("hash/size wrong: %+v", e.Packages[0])
	}
	if e.Packages[0].URL != "https://h/mem.aiiospkg" {
		t.Fatalf("url: %s", e.Packages[0].URL)
	}
}

func TestBuildCatalogEntryNativeRowsShareOneArchive(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "p.aiiospkg")
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "2.0.0", Description: "Engine",
		Variants: []aiiospkg.AuthorVariant{
			{Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
			{Platform: "macos", Arch: "arm64", ExecutionRuntime: "native_t3_component"},
			{Platform: "windows", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
			{Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"}, // dup, dropped
		}}
	body := writeCatalogFixture(t, cfg, pkg)
	e, err := buildCatalogEntry(cfg, pkg, dir, "https://h/p.aiiospkg", "T3", "override")
	if err != nil {
		t.Fatal(err)
	}
	if e.Summary != "override" {
		t.Fatalf("explicit summary wins: %q", e.Summary)
	}
	if len(e.Packages) != 3 {
		t.Fatalf("a native plugin publishes one row per distinct platform: %+v", e.Packages)
	}
	for _, p := range e.Packages {
		if p.URL != "https://h/p.aiiospkg" || p.SHA256 != aiiospkg.SHA256Prefixed(body) || p.Size != int64(len(body)) {
			t.Fatalf("all platforms must point at the exact same archive: %+v", e.Packages)
		}
	}
}

// Native code requires T3. A platform may also sign WASM at T3: authority
// follows the signing role, not the choice of execution runtime.
func TestBuildCatalogEntryRefusesAnIncoherentTier(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.mem-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	wasm := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	writeCatalogFixture(t, wasm, pkg)
	if entry, err := buildCatalogEntry(wasm, "", dir, "https://h/mem.aiiospkg", "T3", ""); err != nil || entry.Tier != "T3" || len(entry.Packages) != 1 || entry.Packages[0].Platform != "*" || entry.Packages[0].Arch != "*" {
		t.Fatalf("platform-signed WASM must retain its T3 portable catalog row: %+v (%v)", entry, err)
	}
	native := &aiiospkg.AuthorConfig{ID: "com.x.mem", Version: "1.0.0", Title: "Mem",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"}}}
	writeCatalogFixture(t, native, pkg)
	if _, err := buildCatalogEntry(native, "", dir, "https://h/mem.aiiospkg", "T1", ""); err == nil {
		t.Fatal("a package with a native variant must not publish below T3")
	}
	if _, err := buildCatalogEntry(native, "", dir, "https://h/mem.aiiospkg", "T3", ""); err != nil {
		t.Fatalf("a native package at T3: %v", err)
	}
}

func TestBuildCatalogEntryRejectsStaleAuthor(t *testing.T) {
	for _, field := range []string{"id", "version", "platform", "arch", "runtime", "variant_id", "removed_variant", "added_variant"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "p.aiiospkg")
			cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "1.0.0", Variants: []aiiospkg.AuthorVariant{
				{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
			}}
			writeCatalogFixture(t, cfg, path)
			switch field {
			case "id":
				cfg.ID = "com.x.other"
			case "version":
				cfg.Version = "9.9.9"
			case "platform":
				cfg.Variants[0].Platform = "windows"
			case "arch":
				cfg.Variants[0].Arch = "arm64"
			case "runtime":
				cfg.Variants[0].ExecutionRuntime = "wasm_component"
			case "variant_id":
				cfg.Variants[0].VariantID = "other"
			case "removed_variant":
				cfg.Variants = nil
			case "added_variant":
				v := cfg.Variants[0]
				v.VariantID = "extra"
				cfg.Variants = append(cfg.Variants, v)
			}
			// Choose the otherwise coherent tier, so runtime mutation cannot
			// be killed by the unrelated tier guard instead of archive binding.
			tier := "T3"
			if field == "runtime" || field == "removed_variant" {
				tier = "T1"
			}
			if _, err := buildCatalogEntry(cfg, path, dir, "https://h/p.aiiospkg", tier, ""); err == nil || !strings.Contains(err.Error(), "differs from plugin.json") {
				t.Fatalf("stale %s must fail by archive binding, got %v", field, err)
			}
		})
	}
}

func TestBuildCatalogEntryRejectsBrokenArchive(t *testing.T) {
	for _, name := range []string{"not_package", "truncated", "bad_crc", "suffix", "second_stream", "no_manifest"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "p.aiiospkg")
			cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "1.0.0", Variants: []aiiospkg.AuthorVariant{
				{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
			}}
			body := writeCatalogFixture(t, cfg, path)
			switch name {
			case "not_package":
				body = []byte("PACKAGE-BYTES")
			case "truncated":
				body = body[:len(body)-1]
			case "bad_crc":
				body[len(body)-8] ^= 1
			case "suffix":
				body = append(body, 1)
			case "second_stream":
				body = append(body, body...)
			case "no_manifest":
				tree := aiiospkg.NewTree("root")
				tree.Add("install-root/a", []byte("x"))
				var err error
				body, err = aiiospkg.WriteBundle(tree)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := buildCatalogEntry(cfg, path, dir, "https://h/p.aiiospkg", "T3", ""); err == nil {
				t.Fatalf("%s archive published", name)
			}
		})
	}
}

func TestBuildCatalogEntrySummaryComesFromPackage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.aiiospkg")
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.eng", Version: "1.0.0", Title: "Packaged title", Variants: []aiiospkg.AuthorVariant{
		{VariantID: "native", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "native_t3_component"},
	}}
	writeCatalogFixture(t, cfg, path)
	cfg.Title = "Unpackaged edit"
	e, err := buildCatalogEntry(cfg, path, dir, "https://h/p.aiiospkg", "T3", "")
	if err != nil || e.Summary != "Packaged title" {
		t.Fatalf("summary is detached from archive: %+v %v", e, err)
	}
}

// THE HOST WINDOW TRAVELS WITH THE ROW. A package that declares which
// AII OS versions it runs on is refused by every host outside that
// window — correctly. A catalog row that dropped the window OFFERED it
// to them anyway, so an operator's store showed Install on a release
// their host would then refuse, with the reason arriving only after the
// download. The catalog must carry the signed bounds.
//
// The fields come from the SIGNED MANIFEST, never from plugin.json.
// The package is already signed: what it says about the hosts it runs
// on is settled, and a catalog row describes that package rather than
// getting a second chance to describe it.
func TestACatalogRowCarriesTheSignedHostWindow(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.windowed-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.windowed", Version: "1.0.0", Title: "Windowed",
		AiiosMinVersion: "0.1.7", AiiosMaxExclusiveVersion: "0.2.0",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	writeCatalogFixture(t, cfg, pkg)

	// Whatever plugin.json says NOW, the row must describe the package
	// that was signed.
	cfg.AiiosMinVersion, cfg.AiiosMaxExclusiveVersion = "9.9.9", "10.0.0"
	e, err := buildCatalogEntry(cfg, "", dir, "https://h/w.aiiospkg", "T2", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.AiiosMinVersion != "0.1.7" || e.AiiosMaxExclusiveVersion != "0.2.0" {
		t.Fatalf("the row must carry the window the SIGNED package declares: %+v", e)
	}

	// And it must reach the JSON an author actually publishes.
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"aiios_min_version":"0.1.7"`, `"aiios_max_exclusive_version":"0.2.0"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("published row is missing %s: %s", want, raw)
		}
	}
}

// AND AN UNBOUNDED RELEASE STAYS UNBOUNDED. A plugin that declares no
// window runs on every host, and a row that invented one would hide it
// from hosts it runs on perfectly well.
func TestACatalogRowInventsNoWindowWhereThePackageDeclaresNone(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "dist", "com.x.any-1.0.0.aiiospkg")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.x.any", Version: "1.0.0", Title: "Any",
		Variants: []aiiospkg.AuthorVariant{{VariantID: "wasm", Platform: "linux", Arch: "x86_64", ExecutionRuntime: "wasm_component"}}}
	writeCatalogFixture(t, cfg, pkg)

	e, err := buildCatalogEntry(cfg, "", dir, "https://h/any.aiiospkg", "T2", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.AiiosMinVersion != "" || e.AiiosMaxExclusiveVersion != "" {
		t.Fatalf("no window was declared, so none may be published: %+v", e)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "aiios_min_version") || strings.Contains(string(raw), "aiios_max_exclusive_version") {
		t.Fatalf("an unbounded release must publish no window fields at all: %s", raw)
	}
}
