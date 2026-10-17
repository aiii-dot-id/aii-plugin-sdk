//go:build windows

package aiiospkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/fileperm"
)

// openFolder is the DACL Windows hands a folder made at a drive root, such
// as C:\src: SYSTEM and Administrators full control, Users read and
// execute, Authenticated Users modify, each to everything made beneath it.
const openFolder = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)(A;OICI;0x1301bf;;;AU)"

// others are the accounts a private file must grant nothing: Everyone,
// Users, Authenticated Users.
var others = map[string]string{"S-1-1-0": "Everyone", "S-1-5-32-545": "Users", "S-1-5-11": "Authenticated Users"}

func grantedToOthers(t *testing.T, path string) []string {
	t.Helper()
	_, entries, err := fileperm.DACL(path)
	if err != nil {
		t.Fatal(err)
	}
	var granted []string
	for _, e := range entries {
		if name, ok := others[e.SID]; ok && e.Allow {
			granted = append(granted, name)
		}
	}
	return granted
}

// A KEY FILE IS CLOSED TO EVERY OTHER ACCOUNT WHEREVER IT IS SAVED. An
// author keeps a plugin where they like, and devcert saves its keys in the
// plugin's .keys folder; on Windows a file there takes what its folder
// hands down unless it is given a DACL of its own, and 0600 gives it none.
// The rig is a folder made at a drive root. The control half proves the
// probe sees its grants: a file written there with os.WriteFile at 0600 —
// how SaveKeyFile wrote a key until 2026-10-09 — carries them.
func TestAKeyFileIsClosedUnderAFolderThatGrantsOthers(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "src")
	if err := fileperm.MkdirSDDL(parent, openFolder); err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(parent, "com.example.hello", ".keys")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}

	// Control half.
	path := filepath.Join(keys, "dev-publisher.key.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(grantedToOthers(t, path)) == 0 {
		t.Fatalf("control: %s, written at 0600 under the open folder, grants none of Everyone, Users, Authenticated Users — the rig opens nothing and the checks below would prove nothing", path)
	}

	// A key saved over that file (devcert -force), and a key saved new.
	role := fixtureRole(t)
	fresh := filepath.Join(keys, "dev-certifier.key.json")
	for _, p := range []string{path, fresh} {
		if err := SaveKeyFile(role, p); err != nil {
			t.Fatal(err)
		}
		if granted := grantedToOthers(t, p); len(granted) != 0 {
			t.Errorf("the key file %s grants %v", p, granted)
		}
		if err := fileperm.CheckPrivate(p); err != nil {
			t.Errorf("the key file %s: %v", p, err)
		}
		if _, err := LoadKeyFile(p); err != nil {
			t.Errorf("the key file %s does not load back: %v", p, err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(keys, ".*.tmp")); len(left) != 0 {
		t.Errorf("work files were left behind: %v", left)
	}
}
