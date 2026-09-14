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
// .
// .

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// .
// .
const (
	certifierKeyFile = "dev-certifier.key.json"
	publisherKeyFile = "dev-publisher.key.json"
	rootPubFile      = "certifier-root.pub.json"
	publisherCrtFile = "publisher.cert"
	// .
	// .
	// .
	statusFile = aiiospkg.StatusFileCertifier
)

func cmdDevcert(args []string) int {
	fs := flag.NewFlagSet("aiisdk devcert", flag.ExitOnError)
	publisherID := fs.String("publisher-id", "", "certified publisher identity (default: dev.publisher.<hostname>)")
	days := fs.Int("days", 365, "validity window in days for both keys")
	force := fs.Bool("force", false, "overwrite an existing dev chain in .keys/")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk devcert [flags]

Mints the local dev signing chain into .keys/ (created 0700,
gitignored by the scaffold):

  dev-certifier.key.json   dev certifier root keypair        (0600)
  dev-publisher.key.json   dev publisher keypair             (0600)
  certifier-root.pub.json  the PIN FILE: the dev root's public
                           keyset envelope — hand it to
                           'aii plugin verify -certifier-key' or pin
                           it as plugins.certifier_root       (0644)
  publisher.cert           the certifier-signed publisher
                           certificate 'aiisdk sign' packages (0644)
  aiii_plugin_publisher_certifier_status.json
                           the certifier's EMPTY signed revocation
                           snapshot (trust_epoch 1). The runtime
                           fails T1 closed without it — point
                           'aii plugin verify -trust-dir' at .keys/
                           or install it into <data>/trust/. The
                           'aiisdk revoke' verb maintains it  (0644)

Both roles are dual-PQ (ML-DSA-87 + SLH-DSA-SHA2-256s, profile
AIII-PQ-SIGNATURE-V1-ROOT). Key ids are clearly dev-named
(dev_certifier_<hostname>_k1). SLH-DSA keygen and signing are slow by
design — the chain is minted once and reused across plugins.

Key material is written only to the 0600 files, never printed.

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
	keys := keysDir(dir)
	for _, name := range []string{certifierKeyFile, publisherKeyFile, rootPubFile, publisherCrtFile, statusFile} {
		if _, err := os.Stat(filepath.Join(keys, name)); err == nil && !*force {
			return fail(".keys/%s exists — the dev chain is already minted (use -force to replace it, which orphans anything the old chain signed)", name)
		}
	}
	if err := os.MkdirAll(keys, 0o700); err != nil {
		return fail("%v", err)
	}

	host := sanitizeHost()
	pubID := *publisherID
	if pubID == "" {
		pubID = "dev.publisher." + host
	}
	notBefore := time.Now().UTC().Add(-time.Hour)
	expires := time.Now().UTC().Add(time.Duration(*days) * 24 * time.Hour)

	fmt.Println("minting the dev certifier root (dual-PQ keygen; SLH-DSA takes a moment)...")
	certifier, err := aiiospkg.GenerateRole("dev_certifier_"+host+"_k1", aiiospkg.KeyTypePublisherCertifier, notBefore, expires)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Println("minting the dev publisher key...")
	publisher, err := aiiospkg.GenerateRole("dev_publisher_"+host+"_k1", aiiospkg.KeyTypePublisher, notBefore, expires)
	if err != nil {
		return fail("%v", err)
	}

	if err := aiiospkg.SaveKeyFile(certifier, filepath.Join(keys, certifierKeyFile)); err != nil {
		return fail("%v", err)
	}
	if err := aiiospkg.SaveKeyFile(publisher, filepath.Join(keys, publisherKeyFile)); err != nil {
		return fail("%v", err)
	}

	rootEnv, err := certifier.PublicEnvelope()
	if err != nil {
		return fail("%v", err)
	}
	rootRaw, err := marshalIndented(rootEnv)
	if err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(keys, rootPubFile), rootRaw, 0o644); err != nil {
		return fail("%v", err)
	}

	// .
	// .
	// .
	// .
	publisherEnv, err := publisher.PublicEnvelope()
	if err != nil {
		return fail("%v", err)
	}
	fmt.Println("signing the publisher certificate (SLH-DSA signing is the slow half)...")
	cert, err := certifier.Sign(aiiospkg.ArtifactKindPublisherCert, map[string]interface{}{
		"publisher_id":  pubID,
		"publisher_key": publisherEnv,
	})
	if err != nil {
		return fail("%v", err)
	}
	if err := certifier.VerifyOwnEnvelope(cert, aiiospkg.ArtifactKindPublisherCert); err != nil {
		return fail("minted certificate failed self-verification: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keys, publisherCrtFile), cert, 0o644); err != nil {
		return fail("%v", err)
	}

	// .
	// .
	// .
	// .
	// .
	statusPayload, err := aiiospkg.BuildRevocationStatusPayload(1, nil)
	if err != nil {
		return fail("%v", err)
	}
	status, err := certifier.Sign(aiiospkg.ArtifactKindRevocationStatus, statusPayload)
	if err != nil {
		return fail("%v", err)
	}
	if err := certifier.VerifyOwnEnvelope(status, aiiospkg.ArtifactKindRevocationStatus); err != nil {
		return fail("minted revocation snapshot failed self-verification: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keys, statusFile), status, 0o644); err != nil {
		return fail("%v", err)
	}

	fmt.Printf("dev chain minted in %s/\n", keys)
	fmt.Printf("  certifier  %s (key file %s)\n", certifier.KeyID, certifierKeyFile)
	fmt.Printf("  publisher  %s as %q (key file %s)\n", publisher.KeyID, pubID, publisherKeyFile)
	fmt.Printf("  pin file   %s — pass to 'aii plugin verify -certifier-key' or pin as plugins.certifier_root\n", rootPubFile)
	fmt.Printf("  status     %s — the empty revocation snapshot (trust_epoch 1); point 'aii plugin verify -trust-dir' at .keys/ or install into <data>/trust/\n", statusFile)
	fmt.Printf("  window     %s .. %s\n", certifier.NotBefore, certifier.ExpiresAt)
	fmt.Printf("next: aiisdk sign\n")
	return 0
}

func sanitizeHost() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "localhost"
	}
	host = strings.ToLower(host)
	var b strings.Builder
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
