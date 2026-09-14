package aiiospkg

// .
// .
// .
// .
// .
// .
// .

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	roleOnce sync.Once
	roleFix  *Role
	roleErr  error
)

func fixtureRole(t *testing.T) *Role {
	t.Helper()
	roleOnce.Do(func() {
		roleFix, roleErr = GenerateRole("test_publisher_k1", KeyTypePublisher,
			time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(24*time.Hour))
	})
	if roleErr != nil {
		t.Fatalf("GenerateRole: %v", roleErr)
	}
	return roleFix
}

func TestPublicEnvelopeShape(t *testing.T) {
	env, err := fixtureRole(t).PublicEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	if env.V != 1 || env.Profile != ProfileRoot || env.KeyID != "test_publisher_k1" || env.KeyType != KeyTypePublisher {
		t.Fatalf("envelope header: %+v", env)
	}
	if len(env.Keys) != 2 || env.Keys[0].Alg != AlgMLDSA87 || env.Keys[1].Alg != AlgSLHDSA {
		t.Fatalf("ProfileRoot needs exactly [ML-DSA-87, SLH-DSA-SHA2-256s], got %+v", env.Keys)
	}
	// .
	wantSizes := map[string]int{AlgMLDSA87: 2592, AlgSLHDSA: 64}
	for _, k := range env.Keys {
		raw, err := base64.StdEncoding.DecodeString(k.PublicKeyB64)
		if err != nil || len(raw) != wantSizes[k.Alg] {
			t.Fatalf("%s public key: %d bytes (err %v), want %d", k.Alg, len(raw), err, wantSizes[k.Alg])
		}
		if k.PublicKeyFingerprint != PublicKeyFingerprint(k.Alg, env.KeyID, k.PublicKeyB64) {
			t.Fatalf("%s fingerprint does not bind alg+key_id+key", k.Alg)
		}
	}
	if _, err := time.Parse(time.RFC3339, env.NotBefore); err != nil {
		t.Fatalf("not_before: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, env.ExpiresAt); err != nil {
		t.Fatalf("expires_at: %v", err)
	}
}

func TestSignEmitsExactDualPQSet(t *testing.T) {
	role := fixtureRole(t)
	payload := map[string]string{"package_hash": SHA256Prefixed([]byte("p")), "manifest_hash": SHA256Prefixed([]byte("m"))}
	bundle, err := role.Sign(ArtifactKindManifestSig, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := role.VerifyOwnEnvelope(bundle, ArtifactKindManifestSig); err != nil {
		t.Fatalf("self-verification: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(bundle, &env); err != nil {
		t.Fatal(err)
	}
	if env.Canonicalization != CanonicalizationV1 || env.SignatureProfile != ProfileRoot {
		t.Fatalf("envelope constants: %+v", env)
	}
	// .
	canon, err := CanonicalizeV1(env.Payload)
	if err != nil || !bytes.Equal(canon, env.Payload) {
		t.Fatalf("payload not stored canonical (%v)", err)
	}
	if SHA256Prefixed(env.Payload) != env.PayloadSHA256 {
		t.Fatal("payload_sha256 mismatch")
	}
	// .
	// .
	if len(env.Signatures) != 2 || env.Signatures[0].Alg != AlgMLDSA87 || env.Signatures[1].Alg != AlgSLHDSA {
		t.Fatalf("signature set: %+v", env.Signatures)
	}
	wantSig := map[string]int{AlgMLDSA87: 4627, AlgSLHDSA: 29792}
	for _, s := range env.Signatures {
		raw, err := base64.StdEncoding.DecodeString(s.SigB64)
		if err != nil || len(raw) != wantSig[s.Alg] {
			t.Fatalf("%s signature: %d bytes (err %v), want %d", s.Alg, len(raw), err, wantSig[s.Alg])
		}
		input := SignatureInput(ArtifactKindManifestSig, ProfileRoot, s.Alg, s.KeyID, s.PublicKeyFingerprint, env.PayloadSHA256)
		if SHA256Prefixed([]byte(input)) != s.SignatureInputSHA256 {
			t.Fatalf("%s signature_input_sha256 does not rebuild", s.Alg)
		}
	}

	// .
	tampered := bytes.Replace(bundle, []byte("package_hash"), []byte("package_hasX"), 1)
	if err := role.VerifyOwnEnvelope(tampered, ArtifactKindManifestSig); err == nil {
		t.Fatal("tampered envelope self-verified")
	}
	// .
	if err := role.VerifyOwnEnvelope(bundle, ArtifactKindPublisherCert); err == nil {
		t.Fatal("kind confusion accepted")
	}
}

func TestKeyFileRoundTrip(t *testing.T) {
	role := fixtureRole(t)
	path := filepath.Join(t.TempDir(), "keys", "role.key.json")
	if err := SaveKeyFile(role, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file mode %04o, want 0600", perm)
	}

	loaded, err := LoadKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// .
	// .
	// .
	origEnv, _ := role.PublicEnvelope()
	loadEnv, err := loaded.PublicEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(origEnv)
	b, _ := json.Marshal(loadEnv)
	if !bytes.Equal(a, b) {
		t.Fatalf("reloaded envelope drifted:\n%s\n%s", a, b)
	}
	sig, err := loaded.Sign(ArtifactKindManifestSig, map[string]string{"package_hash": "sha256:00", "manifest_hash": "sha256:11"})
	if err != nil {
		t.Fatal(err)
	}
	if err := role.VerifyOwnEnvelope(sig, ArtifactKindManifestSig); err != nil {
		t.Fatalf("cross-verification after reload: %v", err)
	}
}

func TestLoadKeyFileRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	writeTreeFile(t, dir, "bad.json", `{"v":2}`)
	if _, err := LoadKeyFile(filepath.Join(dir, "bad.json")); err == nil || !strings.Contains(err.Error(), "v1") {
		t.Fatalf("bad version accepted: %v", err)
	}
	writeTreeFile(t, dir, "short.json", `{"v":1,"key_id":"k","key_type":"t","mldsa87_seed_b64":"AAAA"}`)
	if _, err := LoadKeyFile(filepath.Join(dir, "short.json")); err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("short seed accepted: %v", err)
	}
}
