package fileperm

import (
	"os"
	"path/filepath"
	"testing"
)

// WritePrivate leaves what CheckPrivate asks for, and CheckPrivate can say
// no: a file written the ordinary way at 0644 is not private on any
// platform. Two files written alike have the same Access; a private file
// and an ordinary one do not. Nothing is left beside the file written.
func TestWritePrivateIsWhatCheckPrivateAsksFor(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "role.key.json")
	if err := WritePrivate(private, []byte("secret\n")); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(private); err != nil || string(raw) != "secret\n" {
		t.Fatalf("read back %q, %v", raw, err)
	}
	if err := CheckPrivate(private); err != nil {
		t.Fatalf("a file WritePrivate wrote is not private: %v", err)
	}

	var access []string
	for _, name := range []string{"a.json", "b.json"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := CheckPrivate(p); err == nil {
			t.Fatalf("%s, written at 0644, was read as private", name)
		}
		a, err := Access(p)
		if err != nil {
			t.Fatal(err)
		}
		access = append(access, a)
	}
	if access[0] != access[1] {
		t.Fatalf("two files written alike differ: %s / %s", access[0], access[1])
	}
	if a, err := Access(private); err != nil || a == access[0] {
		t.Fatalf("a private file and an ordinary one have the same access %s (%v)", a, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("the directory holds %d entries, want the three files: %v", len(entries), entries)
	}
}
