package aiiospkg

import "fmt"

const VariantPreferenceMinHost = "0.1.12"

func validateVariantPreference(order []string, minHost string, variants map[string]bool) error {
	if len(order) == 0 || len(order) > 64 {
		return fmt.Errorf("variant_preference must be an array of 1..64 variant IDs; omit it for default selection")
	}
	if !ValidHostVersion(minHost) || CompareHostVersion(minHost, VariantPreferenceMinHost) < 0 {
		return fmt.Errorf("variant_preference requires aiios_min_version >= %s", VariantPreferenceMinHost)
	}
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		if !variants[id] || seen[id] {
			return fmt.Errorf("variant_preference names unknown or repeated variant %q", id)
		}
		seen[id] = true
	}
	if len(seen) != len(variants) {
		return fmt.Errorf("variant_preference must name every declared variant exactly once")
	}
	return nil
}
