package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// THE DESCRIBE READ FINDS THE TOOLCHAIN'S CACHE AND SETTINGS WHERE THE
// AUTHOR'S SHELL DOES. 'go run .' under the scrubbed environment needs a
// build cache — without one the go command refuses to build ("build cache
// is required, but could not be located") — and reads the author's
// `go env -w` settings, a moved module cache among them, from its
// configuration file. With neither GOCACHE nor GOENV set, the go command
// finds both from the user's cache and configuration homes: HOME on unix,
// %LocalAppData% and %AppData% on Windows. An author who never set them
// is the case.
func TestTheDescribeReadFindsTheToolchainsCacheAndSettings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	for _, k := range []string{"GOCACHE", "GOENV"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	author := toolchainHomes(t, os.Environ())
	if !filepath.IsAbs(author.GOCACHE) || author.GOENV == "" {
		t.Skipf("this environment gives the go command no build cache or settings file of its own (GOCACHE=%q GOENV=%q), so there is nothing for the describe read to keep", author.GOCACHE, author.GOENV)
	}
	read := toolchainHomes(t, scrubbedEnv())
	if !filepath.IsAbs(read.GOCACHE) {
		t.Errorf("under the describe read's environment the go command has no build cache (GOCACHE=%q; the author's is %s), so 'go run .' cannot build", read.GOCACHE, author.GOCACHE)
	}
	if read.GOENV == "" {
		t.Errorf("under the describe read's environment the go command reads no settings file (the author's is %s), so what the author set with `go env -w` is not what it builds with", author.GOENV)
	}
}

type homes struct{ GOCACHE, GOENV string }

func toolchainHomes(t *testing.T, env []string) homes {
	t.Helper()
	cmd := exec.Command("go", "env", "-json", "GOCACHE", "GOENV")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env: %v", err)
	}
	var h homes
	if err := json.Unmarshal(out, &h); err != nil {
		t.Fatalf("go env: %v\n%s", err, out)
	}
	return h
}
