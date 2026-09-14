package main

// .
// .
// .
// .
// .
// .

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func cmdRuntimePack(args []string) int {
	fs := flag.NewFlagSet("aiisdk runtime-pack", flag.ExitOnError)
	dir := fs.String("dir", "", "the runtime tree to pack (required)")
	root := fs.String("root", "runtime", "the archive's root directory name")
	out := fs.String("o", "", "the archive to write (required)")
	d := aiiospkg.RuntimeLimits
	installed := fs.String("max-installed-bytes", "", fmt.Sprintf("budget for the installed tree (bytes, or with a K/M/G suffix); the host's default is %d", d.MaxInstalledBytes))
	files := fs.Int("max-files", d.MaxFiles, "budget for the number of files")
	fileBytes := fs.String("max-file-bytes", "", fmt.Sprintf("budget for the largest file; the host's default is %d", d.MaxFileBytes))
	compressed := fs.String("max-compressed-bytes", "", fmt.Sprintf("budget for the archive itself; the host's default is %d", d.MaxCompressedBytes))
	depth := fs.Int("max-depth", d.MaxDepth, "budget for path depth in segments")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk runtime-pack -dir <tree> -o <archive.tar.gz> [-root runtime]

Packs a native engine's runtime tree — interpreter, libraries, engine
code; never model data — as the companion runtime archive the host
installs beside your carrier: the canonical gzip tar the bundle format
defines, led by an inventory of every file (path, size, sha256, mode),
directories before their children, bytewise order, exec bits kept.
Symlinks are refused. Prints, as JSON, the numbers your plugin.json
runtime declaration carries once the archive is published at its URL:
sha256, size, installed_bytes, files, inventory_sha256.

A tree past the host's default ceilings is refused unless you pass the
budget it needs (-max-installed-bytes, -max-files, -max-file-bytes,
-max-compressed-bytes, -max-depth). The budget is your declared
requirement, never permission: the report then names, as
requires_operator_ceilings, the settings the operator must raise on the
host's Plugins page before the activation is admitted. State them in
your README.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || *out == "" {
		fs.Usage()
		return 2
	}
	budget := aiiospkg.TreeLimits{MaxFiles: *files, MaxDepth: *depth}
	for _, f := range []struct {
		name string
		raw  string
		dst  *int64
	}{
		{"max-installed-bytes", *installed, &budget.MaxInstalledBytes},
		{"max-file-bytes", *fileBytes, &budget.MaxFileBytes},
		{"max-compressed-bytes", *compressed, &budget.MaxCompressedBytes},
	} {
		if f.raw == "" {
			continue
		}
		n, err := parseBytes(f.raw)
		if err != nil {
			return fail("-%s: %v", f.name, err)
		}
		*f.dst = n
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fail("%v", err)
	}
	rep, werr := aiiospkg.WriteRuntimeTreeWithin(f, *dir, *root, budget)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(*out)
		return fail("runtime-pack: %v", werr)
	}
	if cerr != nil {
		return fail("%v", cerr)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	report := map[string]interface{}{
		"archive": *out, "sha256": rep.SHA256, "size": rep.Size, "installed_bytes": rep.InstalledBytes,
		"files": rep.Files, "inventory_sha256": rep.InventorySHA256,
		"largest_file_bytes": rep.LargestFileBytes, "depth": rep.Depth,
	}
	if req := rep.RequiredCeilings(); len(req) > 0 {
		report["requires_operator_ceilings"] = req
		fmt.Fprintf(os.Stderr, "runtime-pack: this runtime exceeds the host's default ceilings; it activates only where the operator sets %s (the Plugins page, runtime ceilings) — state this in your README\n", aiiospkg.FormatCeilings(req))
	}
	if err := enc.Encode(report); err != nil {
		return fail("%v", err)
	}
	return 0
}

// .
// .
func parseBytes(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "i")
	mult := int64(1)
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K', 'k':
			mult, s = 1<<10, s[:n-1]
		case 'M', 'm':
			mult, s = 1<<20, s[:n-1]
		case 'G', 'g':
			mult, s = 1<<30, s[:n-1]
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a positive byte count (digits with an optional K, M or G)", raw)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("%q is out of range", raw)
	}
	return n * mult, nil
}
