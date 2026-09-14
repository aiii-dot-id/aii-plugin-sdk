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

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// .
// .
// .
type RevocationEntry struct {
	ArtifactKind  string `json:"artifact_kind"`
	PayloadSHA256 string `json:"payload_sha256"`
}

// .
// .
// .
var certifierRevocableKinds = map[string]bool{
	ArtifactKindPublisherCert: true,
	ArtifactKindManifestSig:   true,
}

// .
// .
func CertifierMayRevoke(kind string) bool { return certifierRevocableKinds[kind] }

// .
// .
// .
// .
func BuildRevocationStatusPayload(epoch int64, entries []RevocationEntry) (map[string]interface{}, error) {
	if epoch < 1 {
		return nil, fmt.Errorf("trust_epoch must be a positive integer, got %d", epoch)
	}
	sorted := append([]RevocationEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ArtifactKind != sorted[j].ArtifactKind {
			return sorted[i].ArtifactKind < sorted[j].ArtifactKind
		}
		return sorted[i].PayloadSHA256 < sorted[j].PayloadSHA256
	})
	out := sorted[:0]
	for i, e := range sorted {
		if err := validateRevocationEntry(e); err != nil {
			return nil, err
		}
		if i > 0 && e == sorted[i-1] {
			continue
		}
		out = append(out, e)
	}
	if out == nil {
		out = []RevocationEntry{}
	}
	return map[string]interface{}{
		"schema_version": 1, "trust_epoch": epoch, "revoked": out,
	}, nil
}

// .
// .
// .
// .
func ParseRevocationStatusPayload(raw json.RawMessage) (epoch int64, entries []RevocationEntry, err error) {
	var p struct {
		SchemaVersion *int              `json:"schema_version"`
		TrustEpoch    *int64            `json:"trust_epoch"`
		Revoked       []RevocationEntry `json:"revoked"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return 0, nil, fmt.Errorf("snapshot payload: %w", err)
	}
	if p.SchemaVersion == nil || p.TrustEpoch == nil || p.Revoked == nil {
		return 0, nil, fmt.Errorf("snapshot payload is missing schema_version, trust_epoch, or revoked")
	}
	if *p.SchemaVersion != 1 {
		return 0, nil, fmt.Errorf("snapshot schema_version %d is not 1", *p.SchemaVersion)
	}
	if *p.TrustEpoch < 1 {
		return 0, nil, fmt.Errorf("snapshot trust_epoch %d is not positive", *p.TrustEpoch)
	}
	for _, e := range p.Revoked {
		if err := validateRevocationEntry(e); err != nil {
			return 0, nil, err
		}
	}
	return *p.TrustEpoch, p.Revoked, nil
}

func validateRevocationEntry(e RevocationEntry) error {
	if !CertifierMayRevoke(e.ArtifactKind) {
		return fmt.Errorf("artifact_kind %q is outside the dev certifier's revocation domain (%s, %s)",
			e.ArtifactKind, ArtifactKindPublisherCert, ArtifactKindManifestSig)
	}
	return ValidateRevocationDigest(e.PayloadSHA256)
}

// .
// .
func ValidateRevocationDigest(v string) error {
	if !strings.HasPrefix(v, "sha256:") {
		return fmt.Errorf("payload_sha256 %q must start with sha256:", v)
	}
	hexPart := strings.TrimPrefix(v, "sha256:")
	if len(hexPart) != 64 {
		return fmt.Errorf("payload_sha256 must carry 64 hex chars, got %d", len(hexPart))
	}
	for _, c := range hexPart {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("payload_sha256 hex must be lowercase [0-9a-f], got %q", c)
		}
	}
	return nil
}
