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
// .
// .
// .
// .
// .
// .
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const usageText = `aiisdk — the AII OS Go plugin authoring kit

Usage:
  aiisdk <command> [flags]

Commands:
  init <id>   Scaffold a new plugin in a fresh directory: plugin.json
              (the authoring file), main.go (handlers register in
              init()), go.mod. Run it once per plugin.
  build       Compile the guest module: TinyGo, wasm-unknown target,
              into dist/<id>.wasm. Needs TinyGo 0.42 or newer on PATH
              (or TINYGO=/path/to/tinygo).
  package     Assemble dist/<id>-<version>.aiiospkg from the built
              module: emits the plugin's own descriptor surface as the
              interface schema, computes the exact-release quartet,
              and writes the canonical archive. Unsigned (T0).
  devcert     Mint the local dev signing chain into .keys/: a dev
              certifier root, a certified dev publisher, and the
              root's EMPTY revocation snapshot (T1 fails closed
              without it). Run once per plugin directory; another
              plugin reuses the chain with 'sign -keys <dir>'.
  sign        Sign the staged package with the dev publisher key and
              repack: dist/<id>-<version>.aiiospkg becomes a T1
              bundle any host proves by pinning the dev root.
  revoke      Revoke a signed trust payload in the dev snapshot:
              append its (artifact_kind, payload_sha256), bump
              trust_epoch, re-sign. Default target: the staged
              release's publisher.sig.
  test        Prove the plugin on this machine against the distributed
              host binaries: build, package, verify with the host's own
              verifier, hold the packaged descriptor to the module's
              account, run it on the worker under the grants you name
              (-grant kv, -grant net.outbound:host:port) and the cases
              in tests/*.json. Observations are local, never receipts.
  publish     Print the plugin's catalog entry: hashes the built
              .aiiospkg and prints one catalog entry for aiios-plugins.md
              — a portable "*" package for WASM, per-platform for native.
              The operator hosts the package and signs the catalog.
              Exit 0 pass, 1 fail, 3 incomplete (a prerequisite missing).
  runtime-pack
              Pack a native engine's runtime tree (interpreter,
              libraries, code — never model data) as the companion
              runtime archive the host installs beside your carrier,
              and print the numbers your plugin.json runtime
              declaration carries once the archive is published.

Run 'aiisdk <command> -h' for that command's flags and details.

The loop, end to end:
  aiisdk init com.example.hello && cd com.example.hello
  # write handlers in main.go, then:
  aiisdk build && aiisdk package && aiisdk test && aiisdk devcert && aiisdk sign
  aii plugin verify -certifier-key .keys/certifier-root.pub.json \
      -trust-dir .keys dist/com.example.hello-0.1.0.aiiospkg
`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "-help" || os.Args[1] == "--help" || os.Args[1] == "help" {
		fmt.Fprint(os.Stderr, usageText)
		if len(os.Args) < 2 {
			os.Exit(2)
		}
		return
	}
	var code int
	switch os.Args[1] {
	case "init":
		code = cmdInit(os.Args[2:])
	case "build":
		code = cmdBuild(os.Args[2:])
	case "package":
		code = cmdPackage(os.Args[2:])
	case "devcert":
		code = cmdDevcert(os.Args[2:])
	case "sign":
		code = cmdSign(os.Args[2:])
	case "revoke":
		code = cmdRevoke(os.Args[2:])
	case "test":
		code = cmdTest(os.Args[2:])
	case "publish":
		code = cmdPublish(os.Args[2:])
	case "runtime-pack":
		code = cmdRuntimePack(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "aiisdk: unknown command %q\n\n%s", os.Args[1], usageText)
		code = 2
	}
	os.Exit(code)
}

// .
func fail(format string, args ...interface{}) int {
	fmt.Fprintf(os.Stderr, "aiisdk: "+format+"\n", args...)
	return 1
}

// .
// .
func loadConfigHere(dir string) (*aiiospkg.AuthorConfig, error) {
	path := filepath.Join(dir, aiiospkg.AuthorFileName)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no %s here — run this command from a plugin directory (aiisdk init creates one)", aiiospkg.AuthorFileName)
	}
	return aiiospkg.LoadAuthorConfig(path)
}

// .
func distDir(dir string) string { return filepath.Join(dir, "dist") }
func wasmPath(dir string, cfg *aiiospkg.AuthorConfig) string {
	return filepath.Join(distDir(dir), cfg.ID+".wasm")
}
func stageDir(dir string, cfg *aiiospkg.AuthorConfig) string {
	return filepath.Join(distDir(dir), "pkg", cfg.Root())
}
func bundlePath(dir string, cfg *aiiospkg.AuthorConfig) string {
	return filepath.Join(distDir(dir), cfg.Root()+".aiiospkg")
}
func keysDir(dir string) string { return filepath.Join(dir, ".keys") }

// .
// .
// .
func marshalIndented(v interface{}) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
