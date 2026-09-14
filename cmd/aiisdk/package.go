package main

// .
// .
// .
// .
// .
// .
// .

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func cmdPackage(args []string) int {
	fs := flag.NewFlagSet("aiisdk package", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk package

Assembles dist/<id>-<version>.aiiospkg from the current directory's
plugin (unsigned — trust tier T0):

  1. reads the plugin's own descriptor surface (the bytes your Describe
     calls declare — they become install-root/interfaces/<interface>.
     v<N>.schema.json): from the built module's aiii-plugin-describe
     export through a worker oracle (AII_OS_BIN, or aii-plugin-worker
     on PATH), else by running 'go run .' under a scrubbed environment;
     the host proves the packaged file against the artifact either way;
  2. stages the canonical tree under dist/pkg/<id>-<version>/ —
     install-root/ with the schema file and every declared variant's
     plugin.wasm, plus the derived manifest.json whose package_hash,
     schema_hash, and artifact_hash are computed from the staged
     bytes;
  3. writes the canonical archive (strict USTAR + pinned gzip
     envelope) and prints the exact-release quartet.

'aiisdk sign' turns the staged tree into the T1 bundle.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail("%v", err)
	}
	cfg, err := loadConfigHere(dir)
	if err != nil {
		return fail("%v", err)
	}
	// .
	// .
	// .
	// .
	// .
	artifacts, err := variantArtifacts(dir, cfg)
	if err != nil {
		return fail("%v", err)
	}

	descriptors, err := describeSurface(dir, cfg)
	if err != nil {
		return fail("%v", err)
	}
	methods, err := aiiospkg.DescriptorIDs(descriptors)
	if err != nil {
		return fail("descriptor emission from 'go run .': %v", err)
	}

	// .
	// .
	// .
	// .
	// .
	ifaces := cfg.InterfaceList()
	byIface, perr := aiiospkg.PartitionMethods(ifaces, methods)
	if perr != nil {
		return fail("interfaces: %v", perr)
	}
	install := map[string][]byte{}
	for _, iface := range ifaces {
		subset := descriptors
		if len(ifaces) > 1 {
			subset, err = aiiospkg.DescriptorsSubset(descriptors, byIface[iface.ID])
			if err != nil {
				return fail("interface %s: %v", iface.ID, err)
			}
		}
		install[aiiospkg.SchemaFileRel(iface)] = subset
	}
	for _, v := range cfg.Variants {
		install[aiiospkg.EntrypointRel(v)] = artifacts[v.VariantID]
	}
	// .
	// .
	if len(cfg.Settings) > 0 {
		settings, err := aiiospkg.SettingsJSON(cfg.Settings)
		if err != nil {
			return fail("settings: %v", err)
		}
		install[aiiospkg.SettingsFile] = settings
	}
	// .
	// .
	if len(cfg.Webhooks) > 0 {
		if err := aiiospkg.CheckWebhookOperations(cfg.Webhooks, methods); err != nil {
			return fail("webhooks: %v", err)
		}
		hooks, err := aiiospkg.WebhooksJSON(cfg.Webhooks, cfg.Settings)
		if err != nil {
			return fail("webhooks: %v", err)
		}
		install[aiiospkg.WebhooksFile] = hooks
	}
	// .
	// .
	if len(cfg.Subscriptions) > 0 {
		if err := aiiospkg.CheckSubscriptionOperations(cfg.Subscriptions, methods); err != nil {
			return fail("subscriptions: %v", err)
		}
		subs, err := aiiospkg.SubscriptionsJSON(cfg.Subscriptions)
		if err != nil {
			return fail("subscriptions: %v", err)
		}
		install[aiiospkg.SubscriptionsFile] = subs
	}
	// .
	// .
	if accel, present, err := aiiospkg.AcceleratorJSON(cfg.Variants); err != nil {
		return fail("accelerator: %v", err)
	} else if present {
		install[aiiospkg.AcceleratorFile] = accel
	}
	if len(cfg.Models) > 0 {
		models, err := aiiospkg.ModelsJSON(cfg.Models)
		if err != nil {
			return fail("models: %v", err)
		}
		install[aiiospkg.ModelsFile] = models
	}
	// .
	// .
	if len(cfg.Runtimes) > 0 {
		if err := aiiospkg.ValidateRuntimes(cfg.Runtimes, cfg.Variants); err != nil {
			return fail("runtimes: %v", err)
		}
		// .
		// .
		for _, d := range cfg.Runtimes {
			if req := aiiospkg.DeclaredCeilings(d); len(req) > 0 {
				fmt.Fprintf(os.Stderr, "runtimes: variant %s exceeds the host's default ceilings; it activates only where the operator sets %s (the Plugins page, runtime ceilings) — state this in your README\n", d.VariantID, aiiospkg.FormatCeilings(req))
			}
		}
		runtimes, err := aiiospkg.RuntimesJSON(cfg.Runtimes)
		if err != nil {
			return fail("runtimes: %v", err)
		}
		install[aiiospkg.RuntimesFile] = runtimes
	}

	// .
	// .
	// .
	// .
	// .
	// .
	if err := addSchemaFiles(install, descriptors, dir); err != nil {
		return fail("%v", err)
	}

	manifest, err := aiiospkg.BuildManifest(cfg, methods, install)
	if err != nil {
		return fail("%v", err)
	}

	tree := aiiospkg.NewTree(cfg.Root())
	tree.Add("manifest.json", manifest)
	for rel, content := range install {
		tree.Add("install-root/"+rel, content)
	}

	// .
	// .
	// .
	stage := stageDir(dir, cfg)
	if err := os.RemoveAll(stage); err != nil {
		return fail("%v", err)
	}
	for rel, content := range tree.Files {
		path := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fail("%v", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return fail("%v", err)
		}
	}

	bundle, err := aiiospkg.WriteBundle(tree)
	if err != nil {
		return fail("%v", err)
	}
	out := bundlePath(dir, cfg)
	if err := os.WriteFile(out, bundle, 0o644); err != nil {
		return fail("%v", err)
	}

	manifestHash, err := aiiospkg.ManifestHash(manifest)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Printf("packaged %s (%d bytes, unsigned T0)\n", out, len(bundle))
	fmt.Printf("  id            %s\n  version       %s\n", cfg.ID, cfg.Version)
	fmt.Printf("  package_hash  %s\n", aiiospkg.PackageHash(install))
	fmt.Printf("  manifest_hash %s\n", manifestHash)
	fmt.Printf("  staged tree   %s\n", stage)
	fmt.Printf("next: aiisdk devcert (once per machine), then aiisdk sign\n")
	return 0
}

// .
// .
// .
func runDescribe(dir string) ([]byte, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("the Go toolchain is required to read the plugin's descriptor surface: %w", err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	// .
	// .
	// .
	cmd.Env = append(scrubbedEnv(), "AIISDK_DESCRIBE=1")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("'go run .' failed (%v) — the plugin package must compile on the host; its stderr:\n%s", err, stderr.String())
	}
	out := stdout.Bytes()
	if len(out) == 0 {
		return nil, fmt.Errorf("'go run .' printed nothing — a wasm main must call sdk.MainDescribe() (the scaffold's one-liner); a native main must end in p.Serve or p.ServeReady, which answer the packager's AIISDK_DESCRIBE=1 with the descriptors")
	}
	return out, nil
}

// .
// .
// .
// .
// .
// .
// .
func describeSurface(dir string, cfg *aiiospkg.AuthorConfig) ([]byte, error) {
	module := wasmPath(dir, cfg)
	if _, err := os.Stat(module); err == nil {
		if argv := describeOracle(); argv != nil {
			out, err := runOracleDescribe(argv, module)
			if err == nil {
				return out, nil
			}
			fmt.Fprintf(os.Stderr, "aiisdk package: the worker oracle could not read the module's account (%v); running the plugin package on this host instead\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "aiisdk package: no worker oracle found (AII_OS_BIN, or aii-plugin-worker on PATH); reading the descriptors by running the plugin package on this host under a scrubbed environment — the host proves them against the artifact at activation")
		}
	}
	return runDescribe(dir)
}

// .
// .
// .
// .
// .
func describeOracle() []string {
	names := []string{"aii-plugin-worker", "aii-plugin-worker.exe"}
	if bin := os.Getenv("AII_OS_BIN"); bin != "" {
		for _, n := range names {
			if p := filepath.Join(bin, n); fileExists(p) {
				return []string{p}
			}
		}
		for _, n := range []string{"aii", "aii.exe"} {
			if p := filepath.Join(bin, n); fileExists(p) {
				return []string{p, "plugin-worker"}
			}
		}
	}
	if p, err := exec.LookPath("aii-plugin-worker"); err == nil {
		return []string{p}
	}
	return nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func runOracleDescribe(argv []string, module string) ([]byte, error) {
	args := append(append([]string{}, argv[1:]...), "-describe", module)
	cmd := exec.Command(argv[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %v: %s", strings.Join(argv, " "), err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("the module's account is empty")
	}
	return stdout.Bytes(), nil
}

// .
// .
// .
func scrubbedEnv() []string {
	keep := []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "SystemRoot", "USERPROFILE"}
	var env []string
	for _, k := range keep {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "GOFLAGS=-buildvcs=false", "GOPROXY=off", "GOTOOLCHAIN=local")
}

// .
// .
// .
// .
// .
// .
// .
func addSchemaFiles(install map[string][]byte, descriptors []byte, srcDir string) error {
	var list []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(descriptors, &list); err != nil {
		return fmt.Errorf("parsing descriptors for schema references: %w", err)
	}
	for _, op := range list {
		for _, ref := range []string{op.Input, op.Output} {
			if ref == "" {
				continue
			}
			// .
			if _, exists := install[ref]; exists {
				continue
			}
			path, rerr := resolveContained(srcDir, ref, "schema")
			if rerr != nil {
				return fmt.Errorf("descriptor references %w", rerr)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("descriptor references schema %q but the file is not found at %s: %w", ref, path, err)
			}
			// .
			// .
			// .
			if err := aiiospkg.CheckSchemaSubset(data); err != nil {
				return fmt.Errorf("descriptor references schema %q, which is outside the closed subset the host compiles: %w", ref, err)
			}
			install[ref] = data
		}
	}
	return nil
}

// .
// .
// .
// .
// .
// .
// .
// .
func variantArtifacts(dir string, cfg *aiiospkg.AuthorConfig) (map[string][]byte, error) {
	out := make(map[string][]byte, len(cfg.Variants))
	var built []byte
	var builtErr error
	builtRead := false

	for _, v := range cfg.Variants {
		if v.Artifact != "" {
			path, rerr := resolveArtifact(dir, v.Artifact)
			if rerr != nil {
				return nil, fmt.Errorf("variant %s: %w", v.VariantID, rerr)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("variant %s: artifact %s: %w", v.VariantID, v.Artifact, err)
			}
			if len(b) == 0 {
				return nil, fmt.Errorf("variant %s: artifact %s is empty", v.VariantID, v.Artifact)
			}
			out[v.VariantID] = b
			continue
		}
		if v.RequiresOwnArtifact() {
			return nil, fmt.Errorf("variant %s runs %s and declares no artifact — a native variant needs its own bytes; the built wasm module is not a substitute",
				v.VariantID, v.ExecutionRuntime)
		}
		if !builtRead {
			built, builtErr = os.ReadFile(wasmPath(dir, cfg))
			builtRead = true
		}
		if builtErr != nil {
			return nil, fmt.Errorf("variant %s declares no artifact and there is no built module at %s — run 'aiisdk build' first, or give the variant an \"artifact\" (%v)",
				v.VariantID, wasmPath(dir, cfg), builtErr)
		}
		out[v.VariantID] = built
	}
	return out, nil
}

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
func resolveArtifact(dir, rel string) (string, error) {
	return resolveContained(dir, rel, "artifact")
}

// .
// .
// .
// .
// .
// .
// .
// .
// .
func resolveContained(root, rel, kind string) (string, error) {
	// .
	// .
	// .
	// .
	if strings.ContainsRune(rel, '\\') {
		return "", fmt.Errorf("%s %q must use forward slashes", kind, rel)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%s %q is absolute; it must be relative to the plugin directory", kind, rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s %q leaves the plugin directory", kind, rel)
	}
	path := filepath.Join(root, clean)
	dir := root

	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolving the plugin directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", kind, rel, err)
	}
	inside, err := filepath.Rel(root, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s %q resolves to %s, outside the plugin directory — a signing tool packages what it is pointed at, so this is refused rather than followed", kind, rel, real)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", kind, rel, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s %q is not a regular file", kind, rel)
	}
	return path, nil
}
