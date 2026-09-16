package main

// .
// .
// .
// .

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"unicode"
)

const sdkModulePath = "github.com/aiii-dot-id/aii-plugin-sdk"

func cmdInit(args []string) int {
	fs := flag.NewFlagSet("aiisdk init", flag.ExitOnError)
	dirFlag := fs.String("dir", "", "directory to scaffold into (default: ./<id>; must be empty or absent)")
	sdkFlag := fs.String("sdk", "", "path to a local aii-plugin-sdk checkout for an offline `replace` directive (default: $AII_SDK_DIR, then the enclosing checkout if any)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk init [flags] <plugin-id>

Scaffolds a new plugin. <plugin-id> is the manifest id (lowercase
dotted, e.g. com.example.hello) — it names the plugin everywhere:
manifest, bundle root, artifact filename.

The scaffold registers handlers in init(), not main(): the wasm host
runs only the module initializer, so main() is dead code in the guest.
On a host, main() runs and emits the plugin's descriptor surface —
that is how 'aiisdk package' reads what you declared, from the one
place it lives: your Describe calls.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	id := fs.Arg(0)
	if !aiiospkg.ValidPluginID(id) {
		return fail("plugin id %q does not match the manifest id grammar (lowercase dotted, e.g. com.example.hello)", id)
	}

	dir := *dirFlag
	if dir == "" {
		dir = id
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fail("%s is not empty — init refuses to write into existing work (pick -dir or an empty directory)", dir)
	}

	// .
	// .
	// .
	// .
	// .
	// .
	sdkDir := findSDKDir(*sdkFlag)
	version := ""
	if sdkDir == "" {
		if version = installedVersion(); version == "" {
			return fail("no aii-plugin-sdk checkout found, and this aiisdk was not installed from its origin (a build from a checkout is not one): pass -sdk <path to the checkout>, set AII_SDK_DIR, or install it with 'go install %s/cmd/aiisdk@latest'", sdkModulePath)
		}
	}
	// .
	// .
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("%v", err)
	}

	iface := interfaceIDFor(id)
	platform, arch := hostPlatformArch()
	variantID := platform + "-" + arch + "-wasm"

	files := map[string]string{
		aiiospkg.AuthorFileName: pluginJSONTemplate(id, iface, variantID, platform, arch),
		"main.go":               mainGoTemplate(id),
		".gitignore":            "dist/\n.keys/\n*.wasm\n",
		"go.mod":                goModTemplate(id, sdkDir, version),
		"schemas/echo_in.json":  echoInSchema(),
		"schemas/echo_out.json": echoOutSchema(),
		"tests/echo.json":       echoCaseTemplate(),
	}
	for name, content := range files {
		mode := os.FileMode(0o644)
		fullPath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fail("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), mode); err != nil {
			return fail("write %s: %v", name, err)
		}
	}
	if sdkDir != "" {
		// .
		// .
		// .
		if sum, err := os.ReadFile(filepath.Join(sdkDir, "go.sum")); err == nil {
			if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
				return fail("write go.sum: %v", err)
			}
		}
	} else if err := resolveModules(dir); err != nil {
		// .
		// .
		// .
		_ = os.RemoveAll(dir)
		return fail("the kit at %s could not be fetched, so no scaffold was written: %v\n  offline, or before a release is published, pass -sdk <path to a checkout> or set AII_SDK_DIR", version, err)
	}

	fmt.Printf("scaffolded %s in %s/\n", id, dir)
	if sdkDir != "" {
		fmt.Printf("  go.mod replaces %s => %s (offline-ready)\n", sdkModulePath, sdkDir)
	} else {
		fmt.Printf("  go.mod requires %s %s (go.sum written)\n", sdkModulePath, version)
	}
	fmt.Printf("\nnext:\n  cd %s\n  # write your handlers in main.go, then:\n  aiisdk build && aiisdk package && aiisdk test && aiisdk devcert && aiisdk sign\n", dir)
	return 0
}

// .
// .
// .
func findSDKDir(flagValue string) string {
	if flagValue != "" {
		if abs, err := filepath.Abs(flagValue); err == nil && isSDKDir(abs) {
			return abs
		}
		return ""
	}
	if env := os.Getenv("AII_SDK_DIR"); env != "" && isSDKDir(env) {
		return env
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if isSDKDir(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// .
// .
// .
// .
// .
// .
// .
func installedVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return fetchableVersion(bi)
}

// .
// .
// .
// .
func fetchableVersion(bi *debug.BuildInfo) string {
	if bi.Main.Path != sdkModulePath {
		return ""
	}
	return fetchable(bi.Main.Version, bi.Main.Sum)
}

// .
// .
func fetchable(version, sum string) string {
	if isReleaseVersion(version) {
		return version
	}
	if sum != "" && strings.HasPrefix(version, "v") && !strings.Contains(version, "+") && pseudoVersion.MatchString(version) {
		return version
	}
	return ""
}

var pseudoVersion = regexp.MustCompile(`-(0\.)?[0-9]{14}-[0-9a-f]{12}`)

// .
// .
func isReleaseVersion(v string) bool {
	if !strings.HasPrefix(v, "v") || strings.Contains(v, "+") || pseudoVersion.MatchString(v) {
		return false
	}
	return regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(v)
}

// .
// .
// .
// .
func resolveModules(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy: %v\n%s", err, bytes.TrimSpace(out))
	}
	return nil
}

// .
// .
// .
// .
// .
func ensureModules(dir string) error {
	root, mod := enclosingModule(dir)
	if root == "" || strings.Contains(mod, "module "+sdkModulePath+"\n") || strings.Contains(mod, "replace "+sdkModulePath) {
		return nil
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil && strings.Contains(string(sum), sdkModulePath+" ") {
		return nil
	}
	fmt.Println("resolving the kit's modules (go mod tidy)")
	return resolveModules(root)
}

// .
// .
func enclosingModule(dir string) (root, mod string) {
	for {
		if raw, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			return dir, string(raw)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func isSDKDir(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(raw), "module "+sdkModulePath)
}

// .
// .
// .
func interfaceIDFor(id string) string {
	segs := strings.Split(id, ".")
	for i, s := range segs {
		s = strings.ReplaceAll(s, "-", "_")
		if s == "" || !(s[0] >= 'a' && s[0] <= 'z') {
			s = "p" + s
		}
		segs[i] = s
	}
	return strings.Join(segs, ".") + ".core"
}

// .
// .
// .
func hostPlatformArch() (string, string) {
	platform := "linux"
	switch runtime.GOOS {
	case "darwin":
		platform = "macos"
	case "windows":
		platform = "windows"
	case "linux":
		platform = "linux"
	}
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return platform, arch
}

func pluginJSONTemplate(id, iface, variantID, platform, arch string) string {
	return fmt.Sprintf(`{
  "id": %q,
  "version": "0.1.0",
  "plugin_family": "tool_bridge",
  "interface": { "id": %q, "version": 1 },
  "capability_envelope": [],
  "variants": [
    {
      "variant_id": %q,
      "platform": %q,
      "arch": %q,
      "topology": "full_identity_host",
      "execution_runtime": "wasm_component",
      "admission_profile": "wasm_sandbox",
      "variant_capabilities": []
    }
  ]
}
`, id, iface, variantID, platform, arch)
}

func mainGoTemplate(id string) string {
	return fmt.Sprintf(`// %s — an AII OS plugin built with the Go authoring kit.
//
// Handlers and their descriptors register in init(), NOT main(): the
// wasm host runs only the module initializer and never main() (see
// the aiiosdk package doc — this is measured behavior). main() runs
// only on a host, where it emits your descriptor surface for
// 'aiisdk package'.
package main

import (
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func init() {
	p := sdk.New(%q)

	// One operation to start from: echo returns its arguments
	// unchanged. Input/Output name the JSON-Schema files for the
	// operation's params and result — the scaffold creates them in
	// schemas/ and the packaging step includes them in the .aiiospkg
	// so the runtime can forward accurate parameter types to the LLM.
	p.Describe("core.echo", sdk.Descriptor{
		Summary: "Return the arguments unchanged",
		Input:   "schemas/echo_in.json",
		Output:  "schemas/echo_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("core.echo", func(c sdk.Call) (any, error) {
		// c.Arguments is raw JSON already inside the strict domain;
		// returning it makes it the operation_result verbatim. Use
		// c.Args().String("name") etc. for typed member reads, and
		// sdk.Fail / sdk.Deny for honest failure results.
		return c.Arguments, nil
	})

	p.Run() // last, still inside init()
}

// main never runs in the guest. On a host it prints the registered
// descriptor surface — 'aiisdk package' captures these bytes as the
// interface schema file and mints schema_hash from them.
func main() { sdk.MainDescribe() }
`, id, id)
}

// .
// .
func goModTemplate(id, sdkDir, version string) string {
	if version == "" {
		version = "v0.0.0"
	}
	mod := fmt.Sprintf("module %s\n\ngo 1.25\n\nrequire %s %s\n", id, sdkModulePath, version)
	if sdkDir != "" {
		mod += fmt.Sprintf("\n// Local SDK checkout — remove when the SDK is fetched from its origin.\nreplace %s => %s\n", sdkModulePath, modString(sdkDir))
	}
	return mod
}

// .
// .
// .
// .
func modString(s string) string {
	for _, r := range s {
		if unicode.IsSpace(r) || !unicode.IsGraphic(r) || strings.ContainsRune("\"'`", r) {
			return strconv.Quote(s)
		}
	}
	return s
}

// .
// .
func echoInSchema() string {
	return `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "description": "Arguments to echo — returned unchanged.",
  "additionalProperties": true
}
`
}

// .
// .
func echoOutSchema() string {
	return `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "description": "Echo result — the input arguments, unchanged.",
  "additionalProperties": true
}
`
}

// .
// .
func echoCaseTemplate() string {
	return `{
  "name": "echo returns its arguments",
  "operation": "core.echo",
  "arguments": {"hello": "world"},
  "expect": {"status": "succeeded", "result_contains": "\"hello\""}
}
`
}
