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
// .

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func cmdRevoke(args []string) int {
	fs := flag.NewFlagSet("aiisdk revoke", flag.ExitOnError)
	keysFlag := fs.String("keys", "", "dev chain directory (default: .keys)")
	sigFlag := fs.String("sig", "", "envelope file to revoke (default: the staged release's signatures/publisher.sig)")
	kindFlag := fs.String("kind", "", "artifact_kind to revoke (with -digest; certifier domain only)")
	digestFlag := fs.String("digest", "", "sha256:<64-lowercase-hex> canonical payload digest to revoke (with -kind)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk revoke [flags]

Revokes one signed trust payload in the dev certifier's snapshot:
reads .keys/`+statusFile+`, appends the
(artifact_kind, payload_sha256) pair in canonical order, bumps
trust_epoch, re-signs with the dev certifier key, and rewrites the
file. Idempotent: re-revoking an already-listed payload changes
nothing.

The target names the EXACT signed payload (the envelope's own
payload_sha256), never package bytes or a key id:

  aiisdk revoke                      # revoke the staged release
                                     # (dist/pkg/<id>-<version>/signatures/publisher.sig)
  aiisdk revoke -sig path/to/x.sig   # revoke that envelope's payload
  aiisdk revoke -kind plugin.publisher_certificate \
      -digest sha256:<hex>           # revoke by explicit pair
                                     # (pulling the cert pulls every release)

After revoking, re-verify to watch enforcement land:

  aii plugin verify -certifier-key .keys/certifier-root.pub.json \
      -trust-dir .keys dist/<id>-<version>.aiiospkg
  -> NOT VERIFIED: TRUST_PAYLOAD_REVOKED

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*kindFlag == "") != (*digestFlag == "") {
		return fail("-kind and -digest travel together (or use -sig / the staged default)")
	}
	if *kindFlag != "" && *sigFlag != "" {
		return fail("-sig and -kind/-digest are two ways to name ONE target — pass one")
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail("%v", err)
	}
	keys := *keysFlag
	if keys == "" {
		keys = keysDir(dir)
	}
	certifier, err := aiiospkg.LoadKeyFile(filepath.Join(keys, certifierKeyFile))
	if err != nil {
		return fail("no dev certifier key (%v) — run 'aiisdk devcert' first", err)
	}

	// .
	kind, digest := *kindFlag, *digestFlag
	if kind == "" {
		sigPath := *sigFlag
		if sigPath == "" {
			cfg, cerr := loadConfigHere(dir)
			if cerr != nil {
				return fail("no -sig/-kind and no plugin.json to locate the staged release: %v", cerr)
			}
			sigPath = filepath.Join(stageDir(dir, cfg), "signatures", aiiospkg.SigFilePublisherSig)
		}
		raw, rerr := os.ReadFile(sigPath)
		if rerr != nil {
			return fail("read %s (%v) — run 'aiisdk sign' first, or name the target with -sig/-kind+-digest", sigPath, rerr)
		}
		var env aiiospkg.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fail("%s is not a signature envelope: %v", sigPath, err)
		}
		kind, digest = env.ArtifactKind, env.PayloadSHA256
	}
	if !aiiospkg.CertifierMayRevoke(kind) {
		return fail("artifact_kind %q is outside the dev certifier's revocation domain (%s, %s)",
			kind, aiiospkg.ArtifactKindPublisherCert, aiiospkg.ArtifactKindManifestSig)
	}
	if err := aiiospkg.ValidateRevocationDigest(digest); err != nil {
		return fail("%v", err)
	}

	// .
	// .
	// .
	statusPath := filepath.Join(keys, statusFile)
	epoch := int64(0)
	var entries []aiiospkg.RevocationEntry
	if raw, rerr := os.ReadFile(statusPath); rerr == nil {
		var env aiiospkg.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fail("%s is not an envelope: %v", statusPath, err)
		}
		if err := certifier.VerifyOwnEnvelope(raw, aiiospkg.ArtifactKindRevocationStatus); err != nil {
			return fail("existing snapshot does not verify under the dev certifier (%v) — refusing to extend a snapshot this chain did not sign", err)
		}
		if epoch, entries, err = aiiospkg.ParseRevocationStatusPayload(env.Payload); err != nil {
			return fail("existing snapshot: %v", err)
		}
	} else if !os.IsNotExist(rerr) {
		return fail("read %s: %v", statusPath, rerr)
	} else {
		fmt.Printf("no snapshot at %s — minting the chain's first (pre-snapshot chains never verified T1 anyway)\n", statusPath)
	}

	for _, e := range entries {
		if e.ArtifactKind == kind && e.PayloadSHA256 == digest {
			fmt.Printf("already revoked: %s %s (trust_epoch %d unchanged)\n", kind, digest, epoch)
			return 0
		}
	}
	entries = append(entries, aiiospkg.RevocationEntry{ArtifactKind: kind, PayloadSHA256: digest})
	payload, err := aiiospkg.BuildRevocationStatusPayload(epoch+1, entries)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Println("re-signing the snapshot (SLH-DSA signing is the slow half)...")
	status, err := certifier.Sign(aiiospkg.ArtifactKindRevocationStatus, payload)
	if err != nil {
		return fail("%v", err)
	}
	if err := certifier.VerifyOwnEnvelope(status, aiiospkg.ArtifactKindRevocationStatus); err != nil {
		return fail("re-signed snapshot failed self-verification: %v", err)
	}
	if err := os.WriteFile(statusPath, status, 0o644); err != nil {
		return fail("%v", err)
	}

	fmt.Printf("revoked %s %s\n", kind, digest)
	fmt.Printf("  snapshot   %s\n  trust_epoch %d -> %d\n  entries    %d\n", statusPath, epoch, epoch+1, len(entries))
	fmt.Printf("verify enforcement: aii plugin verify -certifier-key %s -trust-dir %s <bundle>\n",
		filepath.Join(keys, rootPubFile), keys)
	return 0
}
