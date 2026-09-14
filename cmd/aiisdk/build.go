package main

// .
// .

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func cmdBuild(args []string) int {
	fs := flag.NewFlagSet("aiisdk build", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk build

Compiles the plugin in the current directory to dist/<id>.wasm with
the pinned guest recipe:

  tinygo build -o dist/<id>.wasm -target=wasm-unknown \
      -scheduler=none -gc=conservative -no-debug .

TinyGo 0.42 or newer, driving a Go it accepts (1.25 through 1.27 for
0.42). Discovery: $TINYGO, then tinygo on PATH.
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
	tinygo, err := findTinygo()
	if err != nil {
		return fail("%v", err)
	}
	if err := os.MkdirAll(distDir(dir), 0o755); err != nil {
		return fail("%v", err)
	}
	if err := ensureModules(dir); err != nil {
		return fail("%v", err)
	}
	out := wasmPath(dir, cfg)
	cmd := exec.Command(tinygo, "build", "-o", out,
		"-target=wasm-unknown", "-scheduler=none", "-gc=conservative", "-no-debug", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fail("tinygo build failed: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		return fail("tinygo reported success but %s is missing: %v", out, err)
	}
	fmt.Printf("built %s (%d bytes)\n", out, info.Size())
	return 0
}

// .
func findTinygo() (string, error) {
	if v := os.Getenv("TINYGO"); v != "" {
		return v, nil
	}
	if p, err := exec.LookPath("tinygo"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("tinygo not found — install TinyGo 0.42 or newer (https://tinygo.org) or set TINYGO=/path/to/tinygo")
}
