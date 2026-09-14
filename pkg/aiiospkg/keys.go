package aiiospkg

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
// .
// .
// .
// .
// .
// .

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/cloudflare/circl/sign/slhdsa"
)

// .
// .
const slhParamID = slhdsa.SHA2_256s

// .
// .
// .
type Role struct {
	KeyID     string
	KeyType   string
	CreatedAt string
	NotBefore string
	ExpiresAt string

	mlPriv  *mldsa87.PrivateKey
	mlPub   *mldsa87.PublicKey
	slhPriv slhdsa.PrivateKey
}

// .
// .
type keyFile struct {
	V          int    `json:"v"`
	KeyID      string `json:"key_id"`
	KeyType    string `json:"key_type"`
	CreatedAt  string `json:"created_at"`
	NotBefore  string `json:"not_before"`
	ExpiresAt  string `json:"expires_at"`
	MLDSA87    string `json:"mldsa87_seed_b64"`
	SLHDSA256s string `json:"slhdsa_sha2_256s_priv_b64"`
}

// .
// .
// .
func GenerateRole(keyID, keyType string, notBefore, expiresAt time.Time) (*Role, error) {
	if keyID == "" || keyType == "" {
		return nil, fmt.Errorf("a role needs a key_id and a key_type")
	}
	mlPub, mlPriv, err := mldsa87.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ML-DSA-87 keygen: %w", err)
	}
	_, slhPriv, err := slhdsa.GenerateKey(rand.Reader, slhParamID)
	if err != nil {
		return nil, fmt.Errorf("SLH-DSA keygen: %w", err)
	}
	return &Role{
		KeyID:     keyID,
		KeyType:   keyType,
		CreatedAt: notBefore.UTC().Format(time.RFC3339),
		NotBefore: notBefore.UTC().Format(time.RFC3339),
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		mlPriv:    mlPriv,
		mlPub:     mlPub,
		slhPriv:   slhPriv,
	}, nil
}

// .
// .
// .
// .
func SaveKeyFile(r *Role, path string) error {
	kf := keyFile{
		V: 1, KeyID: r.KeyID, KeyType: r.KeyType,
		CreatedAt: r.CreatedAt, NotBefore: r.NotBefore, ExpiresAt: r.ExpiresAt,
		MLDSA87: base64.StdEncoding.EncodeToString(r.mlPriv.Seed()),
	}
	slhBytes, err := r.slhPriv.MarshalBinary()
	if err != nil {
		return err
	}
	kf.SLHDSA256s = base64.StdEncoding.EncodeToString(slhBytes)
	raw, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// .
func LoadKeyFile(path string) (*Role, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf keyFile
	if err := json.Unmarshal(raw, &kf); err != nil {
		return nil, fmt.Errorf("%s is not a key file: %w", path, err)
	}
	if kf.V != 1 || kf.KeyID == "" || kf.KeyType == "" {
		return nil, fmt.Errorf("%s is not a v1 key file", path)
	}
	seed, err := base64.StdEncoding.DecodeString(kf.MLDSA87)
	if err != nil || len(seed) != mldsa87.SeedSize {
		return nil, fmt.Errorf("%s: mldsa87_seed_b64 is not a %d-byte seed", path, mldsa87.SeedSize)
	}
	var seedArr [mldsa87.SeedSize]byte
	copy(seedArr[:], seed)
	mlPub, mlPriv := mldsa87.NewKeyFromSeed(&seedArr)
	slhBytes, err := base64.StdEncoding.DecodeString(kf.SLHDSA256s)
	if err != nil {
		return nil, fmt.Errorf("%s: slhdsa_sha2_256s_priv_b64 undecodable", path)
	}
	slhPriv := slhdsa.PrivateKey{ID: slhParamID}
	if err := slhPriv.UnmarshalBinary(slhBytes); err != nil {
		return nil, fmt.Errorf("%s: SLH-DSA private key: %w", path, err)
	}
	return &Role{
		KeyID: kf.KeyID, KeyType: kf.KeyType,
		CreatedAt: kf.CreatedAt, NotBefore: kf.NotBefore, ExpiresAt: kf.ExpiresAt,
		mlPriv: mlPriv, mlPub: mlPub, slhPriv: slhPriv,
	}, nil
}

// .
// .
// .
// .
func (r *Role) PublicEnvelope() (*PublicKeyEnvelope, error) {
	mlB64 := base64.StdEncoding.EncodeToString(r.mlPub.Bytes())
	slhPub := r.slhPriv.PublicKey()
	slhBytes, err := slhPub.MarshalBinary()
	if err != nil {
		return nil, err
	}
	slhB64 := base64.StdEncoding.EncodeToString(slhBytes)
	return &PublicKeyEnvelope{
		V: 1, Kind: keyEnvelopeKind, KeyID: r.KeyID, KeyType: r.KeyType,
		Profile:   ProfileRoot,
		CreatedAt: r.CreatedAt, NotBefore: r.NotBefore, ExpiresAt: r.ExpiresAt,
		Keys: []PublicKeyMaterial{
			{Alg: AlgMLDSA87, PublicKeyB64: mlB64,
				PublicKeyFingerprint: PublicKeyFingerprint(AlgMLDSA87, r.KeyID, mlB64)},
			{Alg: AlgSLHDSA, PublicKeyB64: slhB64,
				PublicKeyFingerprint: PublicKeyFingerprint(AlgSLHDSA, r.KeyID, slhB64)},
		},
	}, nil
}

// .
// .
// .
// .
// .
func (r *Role) Sign(artifactKind string, payload interface{}) ([]byte, error) {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	canonical, err := CanonicalizeV1(payloadRaw)
	if err != nil {
		return nil, err
	}
	payloadSHA := SHA256Prefixed(canonical)

	env, err := r.PublicEnvelope()
	if err != nil {
		return nil, err
	}
	var sigs []SignatureEntry
	for _, key := range env.Keys {
		input := SignatureInput(artifactKind, ProfileRoot, key.Alg, r.KeyID, key.PublicKeyFingerprint, payloadSHA)
		var raw []byte
		switch key.Alg {
		case AlgMLDSA87:
			raw = make([]byte, mldsa87.SignatureSize)
			if err := mldsa87.SignTo(r.mlPriv, []byte(input), nil, true, raw); err != nil {
				return nil, fmt.Errorf("ML-DSA-87 sign: %w", err)
			}
		case AlgSLHDSA:
			raw, err = slhdsa.SignRandomized(&r.slhPriv, rand.Reader, slhdsa.NewMessage([]byte(input)), nil)
			if err != nil {
				return nil, fmt.Errorf("SLH-DSA sign: %w", err)
			}
		}
		sigs = append(sigs, SignatureEntry{
			Alg: key.Alg, KeyID: r.KeyID, PublicKeyFingerprint: key.PublicKeyFingerprint,
			SignatureInputSHA256: SHA256Prefixed([]byte(input)),
			SigB64:               base64.StdEncoding.EncodeToString(raw),
		})
	}
	return json.Marshal(Envelope{
		ArtifactKind: artifactKind, Payload: canonical, PayloadSHA256: payloadSHA,
		Canonicalization: CanonicalizationV1,
		SignatureProfile: ProfileRoot, Signatures: sigs,
	})
}

// .
// .
// .
// .
func (r *Role) VerifyOwnEnvelope(bundle []byte, expectedKind string) error {
	var env Envelope
	if err := json.Unmarshal(bundle, &env); err != nil {
		return err
	}
	if env.ArtifactKind != expectedKind {
		return fmt.Errorf("artifact_kind %q is not %q", env.ArtifactKind, expectedKind)
	}
	canonical, err := CanonicalizeV1(env.Payload)
	if err != nil {
		return err
	}
	if SHA256Prefixed(canonical) != env.PayloadSHA256 {
		return fmt.Errorf("payload_sha256 mismatch")
	}
	pub, err := r.PublicEnvelope()
	if err != nil {
		return err
	}
	if len(env.Signatures) != 2 {
		return fmt.Errorf("ProfileRoot requires exactly the two-algorithm signature set, got %d entries", len(env.Signatures))
	}
	for _, sig := range env.Signatures {
		var mat *PublicKeyMaterial
		for i := range pub.Keys {
			if pub.Keys[i].Alg == sig.Alg {
				mat = &pub.Keys[i]
			}
		}
		if mat == nil {
			return fmt.Errorf("signature alg %q has no key material", sig.Alg)
		}
		input := SignatureInput(env.ArtifactKind, env.SignatureProfile, sig.Alg, sig.KeyID, sig.PublicKeyFingerprint, env.PayloadSHA256)
		if SHA256Prefixed([]byte(input)) != sig.SignatureInputSHA256 {
			return fmt.Errorf("signature_input_sha256 mismatch for %s", sig.Alg)
		}
		raw, err := base64.StdEncoding.DecodeString(sig.SigB64)
		if err != nil {
			return err
		}
		switch sig.Alg {
		case AlgMLDSA87:
			if !mldsa87.Verify(r.mlPub, []byte(input), nil, raw) {
				return fmt.Errorf("ML-DSA-87 self-verification failed")
			}
		case AlgSLHDSA:
			slhPub := r.slhPriv.PublicKey()
			if !slhdsa.Verify(&slhPub, slhdsa.NewMessage([]byte(input)), raw, nil) {
				return fmt.Errorf("SLH-DSA self-verification failed")
			}
		default:
			return fmt.Errorf("unexpected algorithm %q", sig.Alg)
		}
	}
	return nil
}
