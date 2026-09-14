package main

// .
// .
// .
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
	"path/filepath"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// .
type hashPair struct {
	PackageHash  string `json:"package_hash"`
	ManifestHash string `json:"manifest_hash"`
}

func cmdSign(args []string) int {
	fs := flag.NewFlagSet("aiisdk sign", flag.ExitOnError)
	stageFlag := fs.String("dir", "", "staged package tree to sign (default: dist/pkg/<id>-<version> from plugin.json)")
	keysFlag := fs.String("keys", "", "dev chain directory (default: .keys)")
	outFlag := fs.String("out", "", "output bundle path (default: dist/<id>-<version>.aiiospkg)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk sign [flags]

Signs the staged package (from 'aiisdk package') with the dev
publisher key (from 'aiisdk devcert') and repacks it canonically:

  1. recomputes package_hash over the staged install-root/ and
     manifest_hash from the staged manifest.json — the signature
     binds measured bytes, never claims;
  2. emits signatures/publisher.sig (artifact kind plugin.manifest,
     closed {package_hash, manifest_hash} payload, dual-PQ) and
     packages signatures/publisher.cert beside it — the pair is T1
     evidence, neither valid alone;
  3. repacks the whole tree into the canonical archive (adding
     members changes canonical order, so the archive is re-emitted).

Verify the result against the runtime's own oracle (the -trust-dir
carries the devcert-minted revocation snapshot — T1 fails closed
without it):

  aii plugin verify -certifier-key .keys/certifier-root.pub.json \
      -trust-dir .keys dist/<id>-<version>.aiiospkg

Flags:
`)
		fs.PrintDefaults()
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
	stage := *stageFlag
	if stage == "" {
		stage = stageDir(dir, cfg)
	}
	keys := *keysFlag
	if keys == "" {
		keys = keysDir(dir)
	}
	out := *outFlag
	if out == "" {
		out = bundlePath(dir, cfg)
	}

	if _, err := os.Stat(stage); err != nil {
		return fail("no staged tree at %s — run 'aiisdk package' first", stage)
	}
	publisher, err := aiiospkg.LoadKeyFile(filepath.Join(keys, publisherKeyFile))
	if err != nil {
		return fail("no dev publisher key (%v) — run 'aiisdk devcert' first", err)
	}
	cert, err := os.ReadFile(filepath.Join(keys, publisherCrtFile))
	if err != nil {
		return fail("no publisher certificate (%v) — run 'aiisdk devcert' first", err)
	}
	var certEnv aiiospkg.Envelope
	if err := json.Unmarshal(cert, &certEnv); err != nil || certEnv.ArtifactKind != aiiospkg.ArtifactKindPublisherCert {
		return fail("%s/%s is not a publisher certificate envelope", keys, publisherCrtFile)
	}

	tree, err := aiiospkg.ReadTree(stage)
	if err != nil {
		return fail("staged tree: %v", err)
	}
	if tree.Root != cfg.Root() {
		return fail("staged tree root %q is not the manifest identity %q", tree.Root, cfg.Root())
	}
	manifest, ok := tree.Files["manifest.json"]
	if !ok {
		return fail("staged tree has no manifest.json — re-run 'aiisdk package'")
	}

	// .
	// .
	// .
	// .
	packageHash := aiiospkg.PackageHash(tree.InstallFiles())
	manifestHash, err := aiiospkg.ManifestHash(manifest)
	if err != nil {
		return fail("staged manifest.json: %v", err)
	}
	var declared struct {
		PackageHash string `json:"package_hash"`
	}
	if err := json.Unmarshal(manifest, &declared); err != nil {
		return fail("staged manifest.json: %v", err)
	}
	if declared.PackageHash != packageHash {
		return fail("staged install-root hashes to %s but manifest.json declares %s — the tree changed after packaging; re-run 'aiisdk package'", packageHash, declared.PackageHash)
	}

	fmt.Println("signing the exact release (SLH-DSA signing is the slow half)...")
	sig, err := publisher.Sign(aiiospkg.ArtifactKindManifestSig, hashPair{
		PackageHash:  packageHash,
		ManifestHash: manifestHash,
	})
	if err != nil {
		return fail("%v", err)
	}
	if err := publisher.VerifyOwnEnvelope(sig, aiiospkg.ArtifactKindManifestSig); err != nil {
		return fail("minted signature failed self-verification: %v", err)
	}

	// .
	// .
	// .
	sigDir := filepath.Join(stage, "signatures")
	if err := os.MkdirAll(sigDir, 0o755); err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(sigDir, aiiospkg.SigFilePublisherCrt), cert, 0o644); err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(sigDir, aiiospkg.SigFilePublisherSig), sig, 0o644); err != nil {
		return fail("%v", err)
	}
	tree.Add("signatures/"+aiiospkg.SigFilePublisherCrt, cert)
	tree.Add("signatures/"+aiiospkg.SigFilePublisherSig, sig)

	bundle, err := aiiospkg.WriteBundle(tree)
	if err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(out, bundle, 0o644); err != nil {
		return fail("%v", err)
	}

	fmt.Printf("signed %s (%d bytes, T1 under the dev root)\n", out, len(bundle))
	fmt.Printf("  id            %s\n  version       %s\n", cfg.ID, cfg.Version)
	fmt.Printf("  package_hash  %s\n", packageHash)
	fmt.Printf("  manifest_hash %s\n", manifestHash)
	fmt.Printf("  publisher key %s\n", publisher.KeyID)
	fmt.Printf("verify: aii plugin verify -certifier-key %s -trust-dir %s %s\n", filepath.Join(keys, rootPubFile), keys, out)
	return 0
}
