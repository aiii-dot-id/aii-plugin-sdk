package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

type hashPair struct {
	PackageHash  string `json:"package_hash"`
	ManifestHash string `json:"manifest_hash"`
}

func cmdSign(args []string) int {
	fs := flag.NewFlagSet("aiisdk sign", flag.ExitOnError)
	stageFlag := fs.String("dir", "", "staged package tree to sign (default: dist/pkg/<id>-<version> from plugin.json)")
	keysFlag := fs.String("keys", "", "dev chain directory (default: .keys)")
	outFlag := fs.String("out", "", "output bundle path (default: dist/<id>-<version>.aiiospkg)")
	platformSignature := fs.String("platform-signature", "", "attach this already issued plugin.platform_release envelope; no private key is read")
	packageFlag := fs.String("pkg", "", "canonical unsigned archive for -platform-signature (required with it)")
	platformKey := fs.String("platform-key", "", "pinned platform public key for host verification (default: the host's shipped root)")
	trustDir := fs.String("trust-dir", "", "signed revocation snapshots for host verification (default: the host's shipped snapshots)")
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
		fmt.Fprint(os.Stderr, `
For an independently issued platform signature, work from the unsigned
archive rather than a staging filesystem. This reads no private key:

  aiisdk sign -pkg /path/unsigned.aiiospkg \
      -platform-signature /path/platform.sig -out /path/signed.aiiospkg

The original archive is unchanged. File bytes and executable modes are
preserved. The host's offline verifier must prove exactly this package at
T3 before the output is replaced. Set AII_OS_BIN to its binary directory,
or put aii on PATH. For a non-shipped root, also name -platform-key and
-trust-dir. Neither -dir nor -keys applies to this mode.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return fail("sign takes flags, not positional arguments")
	}
	if *platformSignature != "" {
		if *stageFlag != "" || *keysFlag != "" || *packageFlag == "" || *outFlag == "" {
			return fail("-platform-signature requires -pkg and distinct -out; -dir and -keys do not apply")
		}
		if err := attachPlatformSignature(*packageFlag, *platformSignature, *outFlag, *platformKey, *trustDir); err != nil {
			return fail("%v", err)
		}
		return 0
	}
	if *packageFlag != "" || *platformKey != "" || *trustDir != "" {
		return fail("-pkg, -platform-key and -trust-dir require -platform-signature")
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
