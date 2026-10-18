package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// Hermetic command-adapter fixture, not a cryptographic oracle. The separate
// opt-in host proof below uses a real aii and freshly generated synthetic roots.
func TestMain(m *testing.M) {
	if path := os.Getenv("AIISDK_PLATFORM_FIFO_PROBE"); path != "" {
		if _, err := readSigningFile(path, 1<<20); err != nil && strings.Contains(err.Error(), "regular file") {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if mode := os.Getenv("AIISDK_PLATFORM_VERIFIER_FIXTURE"); mode != "" && len(os.Args) > 3 && os.Args[1] == "plugin" && os.Args[2] == "verify" {
		if mode == "refuse" {
			fmt.Fprintln(os.Stderr, "fixture signature refused")
			os.Exit(7)
		}
		data, err := os.ReadFile(os.Args[len(os.Args)-1])
		if err != nil {
			panic(err)
		}
		tree, err := aiiospkg.ReadBundle(data)
		if err != nil {
			panic(err)
		}
		var head struct{ ID, Version string }
		if err := json.Unmarshal(tree.Files["manifest.json"], &head); err != nil {
			panic(err)
		}
		mh, err := aiiospkg.ManifestHash(tree.Files["manifest.json"])
		if err != nil {
			panic(err)
		}
		ph := aiiospkg.PackageHash(tree.InstallFiles())
		tier := "T3"
		if mode == "wrong_tier" {
			tier = "T0"
		}
		if mode == "wrong_hash" {
			ph = "sha256:wrong"
		}
		fmt.Printf("VERIFIED %s %s %s\n  package_hash %s\n  manifest_hash %s\n", tier, head.ID, head.Version, ph, mh)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func platformVerifierFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	in, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, exeName("aii")), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AII_OS_BIN", dir)
	t.Setenv("AIISDK_PLATFORM_VERIFIER_FIXTURE", "valid")
}

func platformPackageFixture(t *testing.T, id string, native bool) (string, *aiiospkg.Tree, *aiiospkg.AuthorConfig) {
	t.Helper()
	cfg := &aiiospkg.AuthorConfig{
		ID: id, Version: "1.2.3", PluginFamily: "tool_bridge", CapabilityEnvelope: []string{},
		Interface: aiiospkg.AuthorInterface{ID: "example.core", Version: 1},
		Variants:  []aiiospkg.AuthorVariant{{VariantID: "core", Platform: "linux", Arch: "x86_64", Topology: "full_identity_host", ExecutionRuntime: "wasm_component", AdmissionProfile: "wasm_sandbox", VariantCapabilities: []string{}}},
	}
	if native {
		cfg.Variants[0].ExecutionRuntime = "native_t3_component"
		cfg.Variants[0].AdmissionProfile = "platform_reserved"
	}
	files := map[string][]byte{
		aiiospkg.SchemaFileRel(cfg.Interface):   []byte(`[{"id":"core.echo","summary":"s","input":"","output":"","effects":"read.internal","capabilities":[]}]`),
		aiiospkg.EntrypointRel(cfg.Variants[0]): []byte("\x00asm\x01\x00\x00\x00"),
		"bin/helper":                            []byte("executable helper bytes"),
		"data/payload":                          []byte("data bytes\x00with binary"),
	}
	manifest, err := aiiospkg.BuildManifest(cfg, []string{"core.echo"}, files)
	if err != nil {
		t.Fatal(err)
	}
	tree := aiiospkg.NewTree(cfg.Root())
	tree.Add("manifest.json", manifest)
	tree.Add("README.md", []byte("Public package documentation.\n"))
	tree.Add("provenance/built-by.json", []byte(`{"builder":"example"}`))
	tree.Add("provenance/source-url.txt", []byte("https://example.com/source\n"))
	tree.Add("provenance/reproducible-build.txt", []byte("go build ./...\n"))
	tree.Dirs["install-root/cache/empty"] = true
	tree.Dirs["signatures"] = true
	for rel, body := range files {
		tree.Add("install-root/"+rel, body)
	}
	tree.Exec["install-root/bin/helper"] = true
	if native {
		tree.Exec["install-root/"+aiiospkg.EntrypointRel(cfg.Variants[0])] = true
	}
	path := filepath.Join(t.TempDir(), cfg.Root()+".aiiospkg")
	writePlatformBundle(t, path, tree)
	return path, tree, cfg
}

func writePlatformBundle(t *testing.T, path string, tree *aiiospkg.Tree) {
	t.Helper()
	data, err := aiiospkg.WriteBundle(tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func platformEnvelopeFixture(t *testing.T, tree *aiiospkg.Tree) []byte {
	t.Helper()
	mh, err := aiiospkg.ManifestHash(tree.Files["manifest.json"])
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(hashPair{aiiospkg.PackageHash(tree.InstallFiles()), mh})
	if err != nil {
		t.Fatal(err)
	}
	env := aiiospkg.Envelope{ArtifactKind: platformArtifactKind, Payload: payload}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPlatformAttachmentPreservesOriginalMembersAndModes(t *testing.T) {
	platformVerifierFixture(t)
	for _, id := range []string{"com.aiii.memory", "id.aiii.meaning", "com.example.external"} {
		t.Run(id, func(t *testing.T) {
			unsigned, tree, cfg := platformPackageFixture(t, id, true)
			original, err := os.ReadFile(unsigned)
			if err != nil {
				t.Fatal(err)
			}
			sig := platformEnvelopeFixture(t, tree)
			sigFile, output := filepath.Join(t.TempDir(), "platform.sig"), filepath.Join(t.TempDir(), "signed.aiiospkg")
			if err := os.WriteFile(sigFile, sig, 0o644); err != nil {
				t.Fatal(err)
			}
			// Deliberately no plugin.json in this directory: the archive owns
			// its identity; a package can be attached anywhere.
			inDir(t, t.TempDir(), func() {
				if code := cmdSign([]string{"-pkg", unsigned, "-platform-signature", sigFile, "-out", output}); code != 0 {
					t.Fatalf("attach exit %d", code)
				}
			})
			unchanged, err := os.ReadFile(unsigned)
			if err != nil || !bytes.Equal(unchanged, original) {
				t.Fatalf("unsigned input changed: %v", err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			got, err := aiiospkg.ReadBundle(data)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != cfg.Root() || len(got.Files) != len(tree.Files)+1 {
				t.Fatalf("package identity/member set changed: %s %d", got.Root, len(got.Files))
			}
			for rel, want := range tree.Files {
				if !bytes.Equal(got.Files[rel], want) || got.Exec[rel] != tree.Exec[rel] {
					t.Fatalf("original member %s changed bytes or executable mode", rel)
				}
			}
			for rel, present := range tree.Dirs {
				if present && !got.Dirs[rel] {
					t.Fatalf("original directory %q was lost", rel)
				}
			}
			if !bytes.Equal(got.Files["signatures/platform.sig"], sig) || got.Exec["signatures/platform.sig"] {
				t.Fatal("issued signature bytes or mode changed")
			}
		})
	}
}

func TestPlatformAttachmentRefusesUnsafeInputsWithoutReplacingOutput(t *testing.T) {
	platformVerifierFixture(t)
	for _, name := range []string{"signed_input", "root_identity", "install_drift", "signature_pair", "signature_kind", "signature_extra", "duplicate_json", "host_refusal", "host_wrong_tier", "host_wrong_hash", "same_output", "signature_output"} {
		t.Run(name, func(t *testing.T) {
			unsigned, tree, _ := platformPackageFixture(t, "com.aiii.fixture", false)
			sig := platformEnvelopeFixture(t, tree)
			output := filepath.Join(t.TempDir(), "signed.aiiospkg")
			before := []byte("existing output must survive refusal")
			if err := os.WriteFile(output, before, 0o644); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "signed_input":
				tree.Add("signatures/publisher.sig", []byte("present"))
			case "root_identity":
				tree.Root = "com.other.fixture-1.2.3"
			case "install_drift":
				tree.Add("install-root/changed", []byte("later bytes"))
				sig = platformEnvelopeFixture(t, tree) // freshly signed hashes cannot repair a stale manifest.
			case "signature_pair":
				sig = bytes.Replace(sig, []byte("sha256:"), []byte("sha257:"), 1)
			case "signature_kind":
				sig = bytes.Replace(sig, []byte(platformArtifactKind), []byte(aiiospkg.ArtifactKindManifestSig), 1)
			case "signature_extra":
				sig = bytes.Replace(sig, []byte(`"package_hash":`), []byte(`"unexpected":true,"package_hash":`), 1)
			case "duplicate_json":
				sig = bytes.Replace(sig, []byte(`"package_hash":`), []byte(`"package_hash":"bad","package_hash":`), 1)
			case "host_refusal":
				t.Setenv("AIISDK_PLATFORM_VERIFIER_FIXTURE", "refuse")
			case "host_wrong_tier":
				t.Setenv("AIISDK_PLATFORM_VERIFIER_FIXTURE", "wrong_tier")
			case "host_wrong_hash":
				t.Setenv("AIISDK_PLATFORM_VERIFIER_FIXTURE", "wrong_hash")
			case "same_output":
				output = unsigned
				var err error
				before, err = os.ReadFile(unsigned)
				if err != nil {
					t.Fatal(err)
				}
			}
			writePlatformBundle(t, unsigned, tree)
			sigFile := filepath.Join(t.TempDir(), "platform.sig")
			if err := os.WriteFile(sigFile, sig, 0o644); err != nil {
				t.Fatal(err)
			}
			if name == "signature_output" {
				output, before = sigFile, sig
			}
			if err := attachPlatformSignature(unsigned, sigFile, output, "", ""); err == nil {
				t.Fatalf("accepted unsafe %s", name)
			}
			after, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("refused attachment changed existing output: %v", err)
			}
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".aiisdk-platform-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("temporary output leaked: %v %v", matches, err)
			}
		})
	}
}

func TestPlatformAttachmentWithRealHost(t *testing.T) {
	if os.Getenv("AIISDK_PLATFORM_HOST_PROOF") != "1" {
		t.Skip("set AIISDK_PLATFORM_HOST_PROOF=1 and AII_OS_BIN for synthetic-root real host verification")
	}
	if findVerifier() == "" {
		t.Fatal("real host proof requested without host verifier")
	}
	now := time.Now().UTC()
	root, err := aiiospkg.GenerateRole("aiii_platform_release_test", "platform_release", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := root.PublicEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	pubRaw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	trust := t.TempDir()
	rootPath := filepath.Join(trust, "platform.pub.json")
	if err := os.WriteFile(rootPath, pubRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := root.Sign(aiiospkg.ArtifactKindRevocationStatus, map[string]any{"schema_version": 1, "trust_epoch": 1, "revoked": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trust, "aiii_platform_release_status.json"), status, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) {
			unsigned, tree, cfg := platformPackageFixture(t, "com.aiii.fixture", native)
			if !native {
				// Also prove the host admits empty provenance, not merely a
				// directory made nonempty by its optional advisory records.
				for rel := range tree.Files {
					if strings.HasPrefix(rel, "provenance/") {
						delete(tree.Files, rel)
					}
				}
				tree.Dirs["provenance"] = true
				writePlatformBundle(t, unsigned, tree)
			}
			mh, err := aiiospkg.ManifestHash(tree.Files["manifest.json"])
			if err != nil {
				t.Fatal(err)
			}
			sig, err := root.Sign(platformArtifactKind, hashPair{aiiospkg.PackageHash(tree.InstallFiles()), mh})
			if err != nil {
				t.Fatal(err)
			}
			sigFile, output := filepath.Join(t.TempDir(), "platform.sig"), filepath.Join(t.TempDir(), "signed.aiiospkg")
			if err := os.WriteFile(sigFile, sig, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := attachPlatformSignature(unsigned, sigFile, output, rootPath, trust); err != nil {
				t.Fatal(err)
			}
			attached, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			preserved, err := aiiospkg.ReadBundle(attached)
			if err != nil {
				t.Fatal(err)
			}
			for rel, present := range tree.Dirs {
				if present && !preserved.Dirs[rel] {
					t.Fatalf("host-admitted directory %q was lost", rel)
				}
			}
			entry, err := buildCatalogEntry(cfg, output, "", "https://example.com/plugin.aiiospkg", "T3", "")
			if err != nil || entry.Tier != "T3" {
				t.Fatalf("host-admitted T3 catalog row: %+v %v", entry, err)
			}
			// Keep the bound hashes but corrupt one actual signature. The
			// cryptographic host, not the assembler's shape guard, must refuse.
			var env aiiospkg.Envelope
			if err := json.Unmarshal(sig, &env); err != nil {
				t.Fatal(err)
			}
			env.Signatures[0].SigB64 = strings.Repeat("A", len(env.Signatures[0].SigB64))
			bad, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sigFile, bad, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := attachPlatformSignature(unsigned, sigFile, filepath.Join(t.TempDir(), "tampered.aiiospkg"), rootPath, trust); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("host did not reject tampered signature: %v", err)
			}
			// Layout belongs to the host, too. Preserve valid advisory
			// members above; let the real verifier reject a forbidden one.
			if err := os.WriteFile(sigFile, sig, 0o644); err != nil {
				t.Fatal(err)
			}
			tree.Add("outside-install", []byte("not an admitted advisory member"))
			writePlatformBundle(t, unsigned, tree)
			if err := attachPlatformSignature(unsigned, sigFile, filepath.Join(t.TempDir(), "foreign.aiiospkg"), rootPath, trust); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("host did not reject forbidden layout: %v", err)
			}
		})
	}
}
