//go:build e2e

package e2e

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
// .
// .
// .

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildCLI(t *testing.T) string {
	t.Helper()
	tools := filepath.Join(repoRoot(t), ".tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tools, "aiisdk")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/aiisdk")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build aiisdk: %v\n%s", err, out)
	}
	return bin
}

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
// .
// .
func buildVerifyOracle(t *testing.T) string {
	t.Helper()
	sibling := findSibling(t)
	goDir := os.Getenv("AII_OS_GO")
	if goDir == "" {
		if p, err := exec.LookPath("go"); err == nil {
			goDir = filepath.Dir(p)
		}
	}
	goBin := filepath.Join(goDir, "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skipf("e2e: the host's Go toolchain was not found (set AII_OS_GO to its bin directory)")
	}
	tools := filepath.Join(repoRoot(t), ".tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	oracle := filepath.Join(tools, "aii")

	args := []string{"build", "-buildvcs=false", "-o", oracle, "./cmd/aii"}
	trustPath := filepath.Join(sibling, "internal", "packagefmt", "trust.go")
	trustSrc, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatalf("read sibling trust.go: %v", err)
	}
	const broken = "ValidatePublicKeyEnvelope(&env); err"
	const fixed = "ValidatePublicKeyEnvelope(&env, crypto.ProfileRoot); err"
	if bytes.Contains(trustSrc, []byte(broken)) {
		t.Logf("sibling still carries the LoadPinnedRoot empty-profile bug (trust.go:102); building the oracle with the one-token overlay fix")
		patched := filepath.Join(t.TempDir(), "trust_fixed.go")
		if err := os.WriteFile(patched, bytes.Replace(trustSrc, []byte(broken), []byte(fixed), 1), 0o644); err != nil {
			t.Fatal(err)
		}
		overlay := filepath.Join(t.TempDir(), "overlay.json")
		overlayJSON, err := json.Marshal(map[string]map[string]string{"Replace": {trustPath: patched}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(overlay, overlayJSON, 0o644); err != nil {
			t.Fatal(err)
		}
		args = []string{"build", "-buildvcs=false", "-overlay", overlay, "-o", oracle, "./cmd/aii"}
	}
	cmd := exec.Command(goBin, args...)
	cmd.Dir = sibling
	cmd.Env = append(os.Environ(),
		"PATH="+goDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOTOOLCHAIN=local",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build verify oracle: %v\n%s", err, out)
	}
	return oracle
}

// .
func runIn(t *testing.T, dir string, env []string, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestAiisdkT1AgainstVerifyOracle(t *testing.T) {
	tinygo := findTinygo(t)
	aiisdk := buildCLI(t)
	oracle := buildVerifyOracle(t)

	const id = "com.aiii.examples.s2echo"
	base := t.TempDir()
	runIn(t, base, nil, aiisdk, "init", "-sdk", repoRoot(t), id)
	plugin := filepath.Join(base, id)
	env := []string{"TINYGO=" + tinygo}

	runIn(t, plugin, env, aiisdk, "build")
	pkgOut := runIn(t, plugin, env, aiisdk, "package")
	bundle := filepath.Join(plugin, "dist", id+"-0.1.0.aiiospkg")

	// .
	t0Out := runIn(t, plugin, nil, oracle, "plugin", "verify", bundle)
	if !strings.Contains(t0Out, "VERIFIED T0 "+id+" 0.1.0") {
		t.Fatalf("T0 verification: %s", t0Out)
	}
	// .
	for _, line := range []string{"package_hash", "manifest_hash"} {
		want := grabHash(t, pkgOut, line)
		if !strings.Contains(t0Out, want) {
			t.Fatalf("oracle %s differs from aiisdk's:\naiisdk: %s\noracle: %s", line, pkgOut, t0Out)
		}
	}

	runIn(t, plugin, nil, aiisdk, "devcert", "-days", "2")
	runIn(t, plugin, nil, aiisdk, "sign")

	// .
	// .
	// .
	// .
	keys := filepath.Join(plugin, ".keys")
	rootPin := filepath.Join(keys, "certifier-root.pub.json")
	t1Out := runIn(t, plugin, nil, oracle, "plugin", "verify", "-certifier-key", rootPin, "-trust-dir", keys, bundle)
	if !strings.Contains(t1Out, "VERIFIED T1 "+id+" 0.1.0") {
		t.Fatalf("T1 verification: %s", t1Out)
	}
	if !strings.Contains(t1Out, "publisher") {
		t.Fatalf("no certified publisher identity in: %s", t1Out)
	}

	t.Run("signed bundle without the revocation snapshot rejects", func(t *testing.T) {
		cmd := exec.Command(oracle, "plugin", "verify", "-certifier-key", rootPin, bundle)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "REVOCATION_STATUS_UNAVAILABLE") {
			t.Fatalf("snapshot-less tier must fail closed, got (%v): %s", err, out)
		}
	})

	t.Run("revoked release rejects", func(t *testing.T) {
		runIn(t, plugin, nil, aiisdk, "revoke")
		cmd := exec.Command(oracle, "plugin", "verify", "-certifier-key", rootPin, "-trust-dir", keys, bundle)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "TRUST_PAYLOAD_REVOKED") {
			t.Fatalf("revoked release must reject, got (%v): %s", err, out)
		}
	})

	t.Run("signed bundle without a pinned root rejects", func(t *testing.T) {
		// .
		// .
		// .
		// .
		cmd := exec.Command(oracle, "plugin", "verify", bundle)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "PUBLISHER_CERT_INVALID") {
			t.Fatalf("unverifiable evidence must reject with the shipped-root reason, got (%v): %s", err, out)
		}
	})

	t.Run("tampered bundle rejects", func(t *testing.T) {
		raw, err := os.ReadFile(bundle)
		if err != nil {
			t.Fatal(err)
		}
		raw[2000] ^= 0x01
		tampered := filepath.Join(t.TempDir(), "tampered.aiiospkg")
		if err := os.WriteFile(tampered, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(oracle, "plugin", "verify", "-certifier-key", rootPin, tampered)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "NOT VERIFIED") {
			t.Fatalf("tampered bundle verified (%v): %s", err, out)
		}
	})
}

func grabHash(t *testing.T, out, field string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, field) {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.HasPrefix(fields[len(fields)-1], "sha256:") {
				return fields[len(fields)-1]
			}
		}
	}
	t.Fatalf("no %s in output:\n%s", field, out)
	return ""
}
