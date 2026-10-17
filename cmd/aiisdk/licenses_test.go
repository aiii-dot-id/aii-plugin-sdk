// Copyright (c) AI Identity Incorporated. Licensed under Apache-2.0.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func TestPackagePreservesLicenseFiles(t *testing.T) {
	dir := t.TempDir()
	install := map[string][]byte{"plugin.wasm": []byte("fixture")}
	before := aiiospkg.PackageHash(install)
	if err := addLicenseFiles(install, dir); err != nil || len(install) != 1 || aiiospkg.PackageHash(install) != before {
		t.Fatalf("absent optional notices changed package: %v", err)
	}
	want := map[string][]byte{
		"LICENSE":              []byte("author license\r\n"),
		"NOTICE":               []byte("author notice\n"),
		"THIRD_PARTY_LICENSES": []byte("upstream notices\n\n"),
	}
	for name, raw := range want {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "private-notes.txt"), []byte("not a package input"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addLicenseFiles(install, dir); err != nil {
		t.Fatal(err)
	}
	if len(install) != 4 {
		t.Fatalf("unexpected package inputs: %v", install)
	}
	for name, raw := range want {
		if !bytes.Equal(install[name], raw) {
			t.Fatalf("%s bytes changed", name)
		}
		with := aiiospkg.PackageHash(install)
		delete(install, name)
		if aiiospkg.PackageHash(install) == with {
			t.Fatalf("%s is not hash-covered", name)
		}
		install[name] = raw
	}
}

func TestPackageRefusesNonregularLicenseFiles(t *testing.T) {
	for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_LICENSES"} {
		t.Run(name+"_directory", func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := addLicenseFiles(map[string][]byte{}, dir); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("directory accepted: %v", err)
			}
		})
		t.Run(name+"_symlink", func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "external")
			if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := addLicenseFiles(map[string][]byte{}, dir); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("symlink accepted: %v", err)
			}
		})
	}
}

func TestPackageRefusesReplacedLicenseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LICENSE")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicenseFile(path, before); err == nil {
		t.Fatal("replaced file accepted")
	}
}
