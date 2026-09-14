package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// .
// .
// .
// .
func TestArtifactPathsCannotLeaveThePluginTree(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.bin"), []byte("real artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	// .
	if _, err := resolveArtifact(dir, "plugin.bin"); err != nil {
		t.Fatalf("an ordinary artifact was refused: %v", err)
	}
	sub := filepath.Join(dir, "dist")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "plugin.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveArtifact(dir, "dist/plugin.bin"); err != nil {
		t.Fatalf("a subdirectory artifact was refused: %v", err)
	}

	// .
	for _, bad := range []string{
		"../id_ed25519",
		"../../etc/passwd",
		"dist/../../id_ed25519",
		secret,
	} {
		if _, err := resolveArtifact(dir, bad); err == nil {
			t.Errorf("artifact %q escaped the plugin tree", bad)
		}
	}

	// .
	// .
	// .
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "innocent.bin")
		if err := os.Symlink(secret, link); err != nil {
			t.Fatal(err)
		}
		_, err := resolveArtifact(dir, "innocent.bin")
		if err == nil {
			t.Fatal("A SYMLINK POINTING OUTSIDE THE TREE WAS PACKAGED — an arbitrary file would have been signed and shipped")
		}
		if !strings.Contains(err.Error(), "outside the plugin directory") {
			t.Errorf("refused, but not for leaving the tree: %v", err)
		}
	}
}

// .
func TestArtifactMustBeARegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "notafile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveArtifact(dir, "notafile"); err == nil {
		t.Fatal("a directory was accepted as an artifact")
	}
}

// .
func TestAMissingArtifactIsNamed(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveArtifact(dir, "nope.bin")
	if err == nil {
		t.Fatal("a missing artifact was accepted")
	}
	if !strings.Contains(err.Error(), "nope.bin") {
		t.Errorf("the error does not name the artifact: %v", err)
	}
}

// .
// .
// .
// .
func TestSchemaReferencesCannotLeaveThePluginTree(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "core.schema.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveContained(dir, "core.schema.json", "schema"); err != nil {
		t.Fatalf("an ordinary schema was refused: %v", err)
	}
	for _, bad := range []string{"../id_ed25519", "../../etc/passwd", secret} {
		if _, err := resolveContained(dir, bad, "schema"); err == nil {
			t.Errorf("schema %q escaped the plugin tree", bad)
		}
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "innocent.schema.json")
		if err := os.Symlink(secret, link); err != nil {
			t.Fatal(err)
		}
		err := resolveContainedErr(dir, "innocent.schema.json")
		if err == nil {
			t.Fatal("A SCHEMA SYMLINK PACKAGED OUTSIDE BYTES — under a safe archive name, which is the part that makes it invisible")
		}
		if !strings.Contains(err.Error(), "schema") {
			t.Errorf("the refusal does not name what it refused: %v", err)
		}
	}
}

func resolveContainedErr(dir, rel string) error {
	_, err := resolveContained(dir, rel, "schema")
	return err
}

// .
// .
// .
// .
func TestBackslashesAreRefusedOnEveryPlatform(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{`dist\plugin.bin`, `..\..\id_ed25519`, `a\b`} {
		if _, err := resolveContained(dir, bad, "artifact"); err == nil {
			t.Errorf("%q was accepted; on a non-Windows host it is a filename, on Windows it traverses", bad)
		}
	}
}
