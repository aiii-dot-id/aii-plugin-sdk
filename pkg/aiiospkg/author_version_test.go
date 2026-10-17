package aiiospkg

import "testing"

// THE VERSION IS A PATH COMPONENT. `package` rebuilds dist/pkg/<id>-<version>
// from scratch — RemoveAll first — and the bundle's root carries the same
// name; a version that is not one component named a directory elsewhere
// ("../../victim" removed a sibling directory before the bundle grammar
// refused the root).
func TestAVersionIsOnePathComponent(t *testing.T) {
	for _, ok := range []string{"0.1.0", "0.1.0-beta.1", "2026.09.16+build.7", "1"} {
		if err := validVersion(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"../../victim", "a/b", `a\b`, "1.0.", "", " 1.0", "1.0 ", "con"} {
		if err := validVersion(bad); err == nil {
			t.Errorf("%q accepted as a version", bad)
		}
	}
	cfg := &AuthorConfig{ID: "com.example.x", Version: "../../victim"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a version that is a path")
	}
}
