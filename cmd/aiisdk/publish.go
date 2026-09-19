package main

// .
// .
// .
// .
// .
// .
// .

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

type catalogPackage struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type catalogEntry struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Tier    string `json:"tier"`
	Summary string `json:"summary,omitempty"`
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	AiiosMinVersion          string           `json:"aiios_min_version,omitempty"`
	AiiosMaxExclusiveVersion string           `json:"aiios_max_exclusive_version,omitempty"`
	Packages                 []catalogPackage `json:"packages"`
}

func cmdPublish(args []string) int {
	fs := flag.NewFlagSet("aiisdk publish", flag.ContinueOnError)
	url := fs.String("url", "", "the URL where the .aiiospkg is (or will be) hosted (required)")
	tier := fs.String("tier", "T1", "the tier the package is published at: T0, T1 or T2 for a WASM package; T3 is the platform's tier, and the only one for native code")
	summary := fs.String("summary", "", "one-line summary (defaults to packaged title, then description)")
	pkgPath := fs.String("pkg", "", "path to the built .aiiospkg (default: dist/<id>-<version>.aiiospkg)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk publish -url <where the package is hosted> [flags]

Prints the plugin's catalog entry: its id, version, tier and summary,
and its package — the URL you host the .aiiospkg at, with its hash and
size. A WASM plugin publishes one portable package that runs on every
host; a native plugin publishes one catalog row per platform/architecture,
all pointing to the same archive carrying its variants. The kit hosts
nothing and signs no catalog: paste the entry into
the platform's catalog, which the platform signs.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" {
		fmt.Fprintln(os.Stderr, "aiisdk publish: -url is required (where the package is hosted)")
		return 2
	}
	switch *tier {
	case "T0", "T1", "T2", "T3":
	default:
		fmt.Fprintf(os.Stderr, "aiisdk publish: -tier must be T0, T1, T2 or T3, not %q\n", *tier)
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
	entry, err := buildCatalogEntry(cfg, *pkgPath, dir, *url, *tier, *summary)
	if err != nil {
		return fail("%v", err)
	}
	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fail("%v", err)
	}
	fmt.Println(string(out))
	return 0
}

// .
// .
// .
// .
// .
// .
func buildCatalogEntry(cfg *aiiospkg.AuthorConfig, pkgPath, dir, url, tier, summary string) (*catalogEntry, error) {
	if pkgPath == "" {
		pkgPath = filepath.Join(dir, "dist", cfg.ID+"-"+cfg.Version+".aiiospkg")
	}
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("read package %s: %w (run 'aiisdk package' first)", pkgPath, err)
	}
	manifest, err := readCatalogManifest(data)
	if err != nil {
		return nil, err
	}
	if err := manifest.matchesAuthor(cfg); err != nil {
		return nil, err
	}
	if summary == "" {
		if summary = manifest.Title; summary == "" {
			summary = manifest.Description
		}
	}
	sum := sha256.Sum256(data)
	base := catalogPackage{URL: url, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(data))}
	entry := &catalogEntry{ID: manifest.ID, Version: manifest.Version, Tier: tier, Summary: summary,
		AiiosMinVersion: manifest.AiiosMinVersion, AiiosMaxExclusiveVersion: manifest.AiiosMaxExclusiveVersion}

	portable, native := false, false
	for _, v := range manifest.Variants {
		if isWasmRuntime(v.Runtime) {
			portable = true
		} else {
			native = true
		}
	}
	// .
	// .
	// .
	if native && tier != "T3" {
		return nil, fmt.Errorf("a package with a native variant runs only at T3: publish it with -tier T3, or drop the native variant")
	}
	if !native && tier == "T3" {
		return nil, fmt.Errorf("T3 is the platform's tier for native code; a WASM package publishes at T0, T1 or T2")
	}
	if portable {
		p := base
		p.Platform, p.Arch = "*", "*"
		entry.Packages = append(entry.Packages, p)
		return entry, nil
	}
	seen := map[string]bool{}
	for _, v := range manifest.Variants {
		key := v.Platform + "/" + v.Arch
		if seen[key] {
			continue
		}
		seen[key] = true
		p := base
		p.Platform, p.Arch = v.Platform, v.Arch
		entry.Packages = append(entry.Packages, p)
	}
	return entry, nil
}

func isWasmRuntime(r string) bool {
	return r == "wasm_component" || r == "wasm_aot_component"
}
