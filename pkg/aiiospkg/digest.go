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

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// .
// .
func PackageHash(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	agg := sha256.New()
	for _, p := range paths {
		sum := sha256.Sum256(files[p])
		agg.Write([]byte(p))
		agg.Write([]byte{0})
		agg.Write([]byte(hex.EncodeToString(sum[:])))
		agg.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(agg.Sum(nil))
}

// .
// .
func ManifestHash(raw []byte) (string, error) {
	canonical, err := CanonicalizeV1(raw)
	if err != nil {
		return "", fmt.Errorf("manifest is not canonicalizable JSON: %w", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &members); err != nil {
		return "", fmt.Errorf("manifest is not a JSON object: %w", err)
	}
	delete(members, "package_hash")
	stripped, err := json.Marshal(members)
	if err != nil {
		return "", err
	}
	view, err := CanonicalizeV1(stripped)
	if err != nil {
		return "", fmt.Errorf("manifest-hash view not canonicalizable: %w", err)
	}
	return SHA256Prefixed(view), nil
}
