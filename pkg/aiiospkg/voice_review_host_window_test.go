package aiiospkg

import "testing"

// .
func TestVoiceReviewHostWindowMustRejectMalformedOrEmpty(t *testing.T) {
	for _, tc := range []struct{ name, min, max string }{
		{"leading-zero numeric prerelease", "1.2.3-01", ""},
		{"empty prerelease identifier", "1.2.3-alpha..1", ""},
		{"empty prerelease window", "1.2.3-beta.1", "1.2.3-beta.1"},
		{"large core must not collapse to zero", "999999999999999999999999999999999999.0.0", "1.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.AiiosMinVersion, cfg.AiiosMaxExclusiveVersion = tc.min, tc.max
			if err := cfg.Validate(); err == nil {
				t.Fatalf("SDK accepted malformed/empty host window min=%q max=%q", tc.min, tc.max)
			}
		})
	}
}
