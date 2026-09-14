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

import (
	"encoding/json"
	"fmt"
)

// .
// .
const (
	// .
	ProfileRoot = "AIII-PQ-SIGNATURE-V1-ROOT"

	// .
	CanonicalizationV1 = "AIII-CANONICAL-JSON-V1"

	// .
	AlgMLDSA87 = "ML-DSA-87"
	AlgSLHDSA  = "SLH-DSA-SHA2-256s"

	// .
	// .
	ArtifactKindPublisherCert    = "plugin.publisher_certificate"
	ArtifactKindManifestSig      = "plugin.manifest"
	ArtifactKindRevocationStatus = "plugin.revocation_status"

	// .
	// .
	// .
	// .
	StatusFileCertifier = "aiii_plugin_publisher_certifier_status.json"

	// .
	KeyTypePublisher          = "plugin_publisher"
	KeyTypePublisherCertifier = "plugin_publisher_certifier"

	// .
	// .
	SigFilePublisherSig = "publisher.sig"
	SigFilePublisherCrt = "publisher.cert"

	// .
	// .
	// .
	keyEnvelopeKind = "aiii.server_key.public"
)

// .
// .
type Envelope struct {
	ArtifactKind     string           `json:"artifact_kind"`
	Payload          json.RawMessage  `json:"payload"`
	PayloadSHA256    string           `json:"payload_sha256"`
	Canonicalization string           `json:"canonicalization"`
	SignatureProfile string           `json:"signature_profile"`
	Signatures       []SignatureEntry `json:"signatures"`
}

// .
type SignatureEntry struct {
	Alg                  string `json:"alg"`
	KeyID                string `json:"key_id"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
	SignatureInputSHA256 string `json:"signature_input_sha256"`
	SigB64               string `json:"sig_b64"`
}

// .
// .
// .
type PublicKeyEnvelope struct {
	V         int                 `json:"v"`
	Kind      string              `json:"kind"`
	KeyID     string              `json:"key_id"`
	KeyType   string              `json:"key_type"`
	Profile   string              `json:"profile"`
	CreatedAt string              `json:"created_at"`
	NotBefore string              `json:"not_before"`
	ExpiresAt string              `json:"expires_at"`
	Keys      []PublicKeyMaterial `json:"keys"`
}

// .
type PublicKeyMaterial struct {
	Alg                  string `json:"alg"`
	PublicKeyB64         string `json:"public_key_b64"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

// .
// .
// .
func PublicKeyFingerprint(alg, keyID, publicKeyB64 string) string {
	input := fmt.Sprintf("AIII-PUBLIC-KEY-FINGERPRINT-V1\nalg:%s\nkey_id:%s\npublic_key_b64:%s\n", alg, keyID, publicKeyB64)
	return SHA256Prefixed([]byte(input))
}

// .
// .
// .
func SignatureInput(artifactKind, profile, alg, keyID, pubKeyFingerprint, payloadSHA256 string) string {
	return fmt.Sprintf("AIII-SIGNATURE-V1\nartifact_kind:%s\ncanonicalization:%s\nsignature_profile:%s\nalg:%s\nkey_id:%s\npublic_key_fingerprint:%s\npayload_sha256:%s\n",
		artifactKind, CanonicalizationV1, profile, alg, keyID, pubKeyFingerprint, payloadSHA256)
}
