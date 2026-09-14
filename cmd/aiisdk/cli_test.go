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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// .
func inDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

func TestPipelineInitPackageDevcertSign(t *testing.T) {
	if testing.Short() {
		t.Skip("pipeline test compiles the scaffold and mints PQ keys; skipped in -short")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	const id = "com.example.clitest"

	// .
	inDir(t, base, func() {
		if code := cmdInit([]string{"-sdk", repoRoot, id}); code != 0 {
			t.Fatalf("init exit %d", code)
		}
	})
	plugin := filepath.Join(base, id)
	for _, f := range []string{"plugin.json", "main.go", ".gitignore", "go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(plugin, f)); err != nil {
			t.Fatalf("scaffold missing %s: %v", f, err)
		}
	}

	// .
	inDir(t, base, func() {
		if code := cmdInit([]string{"-sdk", repoRoot, id}); code == 0 {
			t.Fatal("init overwrote a non-empty directory")
		}
	})

	// .
	fakeWasm := append([]byte("\x00asm"), bytes.Repeat([]byte("fake-module"), 64)...)
	if err := os.MkdirAll(filepath.Join(plugin, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "dist", id+".wasm"), fakeWasm, 0o644); err != nil {
		t.Fatal(err)
	}

	// .
	inDir(t, plugin, func() {
		if code := cmdPackage(nil); code != 0 {
			t.Fatalf("package exit %d", code)
		}
	})
	bundleFile := filepath.Join(plugin, "dist", id+"-0.1.0.aiiospkg")
	t0, err := os.ReadFile(bundleFile)
	if err != nil {
		t.Fatal(err)
	}
	filesT0 := decodeBundle(t, t0)
	if _, ok := filesT0[id+"-0.1.0/signatures/publisher.sig"]; ok {
		t.Fatal("unsigned package carries signatures")
	}

	// .
	inDir(t, plugin, func() {
		if code := cmdDevcert([]string{"-days", "2"}); code != 0 {
			t.Fatalf("devcert exit %d", code)
		}
		if code := cmdDevcert([]string{"-days", "2"}); code == 0 {
			t.Fatal("devcert overwrote an existing chain without -force")
		}
		if code := cmdSign(nil); code != 0 {
			t.Fatalf("sign exit %d", code)
		}
	})
	if info, err := os.Stat(filepath.Join(plugin, ".keys", "dev-publisher.key.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("publisher key file: %v / %v", err, info)
	}

	t1, err := os.ReadFile(bundleFile)
	if err != nil {
		t.Fatal(err)
	}
	files := decodeBundle(t, t1)
	root := id + "-0.1.0/"

	// .
	manifest, ok := files[root+"manifest.json"]
	if !ok {
		t.Fatal("no manifest.json")
	}
	schemaFile := root + "install-root/interfaces/" + id + ".core.v1.schema.json"
	descriptors, ok := files[schemaFile]
	if !ok {
		t.Fatalf("no interface schema file %s; members: %v", schemaFile, memberNames(files))
	}
	wasmMember, ok := files[root+"install-root/variants/"+hostVariantID()+"/plugin.wasm"]
	if !ok || !bytes.Equal(wasmMember, fakeWasm) {
		t.Fatalf("variant entrypoint missing or drifted (present=%v)", ok)
	}
	for _, sig := range []string{"publisher.sig", "publisher.cert"} {
		if _, ok := files[root+"signatures/"+sig]; !ok {
			t.Fatalf("signed bundle missing signatures/%s", sig)
		}
	}

	// .
	ids, err := aiiospkg.DescriptorIDs(descriptors)
	if err != nil || strings.Join(ids, ",") != "core.echo" {
		t.Fatalf("descriptor ids %v (%v)", ids, err)
	}

	// .
	// .
	// .
	install := map[string][]byte{}
	for name, content := range files {
		if rel, ok := strings.CutPrefix(name, root+"install-root/"); ok {
			install[rel] = content
		}
	}
	var m struct {
		PackageHash string `json:"package_hash"`
		Interfaces  struct {
			Core []struct {
				SchemaHash string `json:"schema_hash"`
			} `json:"core"`
		} `json:"interfaces"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	if got := aiiospkg.PackageHash(install); got != m.PackageHash {
		t.Fatalf("manifest package_hash %s != recomputed %s", m.PackageHash, got)
	}
	schemaSum := sha256.Sum256(descriptors)
	if m.Interfaces.Core[0].SchemaHash != "sha256:"+hex.EncodeToString(schemaSum[:]) {
		t.Fatal("schema_hash is not the descriptor bytes' digest")
	}
	manifestHash, err := aiiospkg.ManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var sigEnv aiiospkg.Envelope
	if err := json.Unmarshal(files[root+"signatures/publisher.sig"], &sigEnv); err != nil {
		t.Fatal(err)
	}
	if sigEnv.ArtifactKind != aiiospkg.ArtifactKindManifestSig {
		t.Fatalf("publisher.sig artifact_kind %q", sigEnv.ArtifactKind)
	}
	var pair struct {
		PackageHash  string `json:"package_hash"`
		ManifestHash string `json:"manifest_hash"`
	}
	if err := json.Unmarshal(sigEnv.Payload, &pair); err != nil {
		t.Fatal(err)
	}
	if pair.PackageHash != m.PackageHash || pair.ManifestHash != manifestHash {
		t.Fatalf("signed pair %+v does not bind the recomputed quartet (%s / %s)", pair, m.PackageHash, manifestHash)
	}

	// .
	// .
	// .
	// .
	certifier, err := aiiospkg.LoadKeyFile(filepath.Join(plugin, ".keys", "dev-certifier.key.json"))
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(plugin, ".keys", aiiospkg.StatusFileCertifier)
	statusRaw, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("devcert minted no revocation snapshot: %v", err)
	}
	if err := certifier.VerifyOwnEnvelope(statusRaw, aiiospkg.ArtifactKindRevocationStatus); err != nil {
		t.Fatalf("snapshot self-verification: %v", err)
	}
	var statusEnv aiiospkg.Envelope
	if err := json.Unmarshal(statusRaw, &statusEnv); err != nil {
		t.Fatal(err)
	}
	epoch, entries, err := aiiospkg.ParseRevocationStatusPayload(statusEnv.Payload)
	if err != nil || epoch != 1 || len(entries) != 0 {
		t.Fatalf("fresh snapshot must be empty at epoch 1: epoch=%d entries=%d err=%v", epoch, len(entries), err)
	}

	// .
	// .
	inDir(t, plugin, func() {
		if code := cmdRevoke(nil); code != 0 {
			t.Fatalf("revoke exit %d", code)
		}
	})
	statusRaw2, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := certifier.VerifyOwnEnvelope(statusRaw2, aiiospkg.ArtifactKindRevocationStatus); err != nil {
		t.Fatalf("revoked snapshot self-verification: %v", err)
	}
	if err := json.Unmarshal(statusRaw2, &statusEnv); err != nil {
		t.Fatal(err)
	}
	epoch2, entries2, err := aiiospkg.ParseRevocationStatusPayload(statusEnv.Payload)
	if err != nil || epoch2 != 2 || len(entries2) != 1 {
		t.Fatalf("post-revoke snapshot: epoch=%d entries=%d err=%v", epoch2, len(entries2), err)
	}
	if entries2[0].ArtifactKind != aiiospkg.ArtifactKindManifestSig || entries2[0].PayloadSHA256 != sigEnv.PayloadSHA256 {
		t.Fatalf("revoked entry %+v does not name the staged release's publisher.sig payload (%s)", entries2[0], sigEnv.PayloadSHA256)
	}
	inDir(t, plugin, func() {
		if code := cmdRevoke(nil); code != 0 {
			t.Fatalf("idempotent revoke exit %d", code)
		}
	})
	statusRaw3, _ := os.ReadFile(statusPath)
	if !bytes.Equal(statusRaw2, statusRaw3) {
		t.Fatal("re-revoking an already-listed payload must not rewrite the snapshot")
	}
	// .
	inDir(t, plugin, func() {
		if code := cmdRevoke([]string{"-kind", "plugin.attestation", "-digest", "sha256:" + strings.Repeat("a", 64)}); code == 0 {
			t.Fatal("revoke accepted a kind outside the certifier domain")
		}
	})

	// .
	// .
	stage := filepath.Join(plugin, "dist", "pkg", id+"-0.1.0")
	if err := os.WriteFile(filepath.Join(stage, "install-root", "interfaces", id+".core.v1.schema.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	inDir(t, plugin, func() {
		if code := cmdSign(nil); code == 0 {
			t.Fatal("sign accepted a drifted staged tree")
		}
	})
}

// .
// .
func decodeBundle(t *testing.T, bundle []byte) map[string][]byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	files := map[string][]byte{}
	prev := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if prev != "" && prev >= name {
			t.Fatalf("member order broken: %q after %q", name, prev)
		}
		prev = name
		if h.Typeflag == tar.TypeReg {
			content, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			files[h.Name] = content
		}
	}
	return files
}

func memberNames(files map[string][]byte) []string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	return names
}

func hostVariantID() string {
	platform, arch := hostPlatformArch()
	return platform + "-" + arch + "-wasm"
}
