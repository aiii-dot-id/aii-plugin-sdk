package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const platformArtifactKind = "plugin.platform_release"

func attachPlatformSignature(unsigned, signature, output, platformKey, trustDir string) error {
	in, err := filepath.Abs(unsigned)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	inInfo, err := os.Stat(in)
	if err != nil {
		return err
	}
	outInfo, statErr := os.Stat(out)
	if in == out || statErr == nil && os.SameFile(inInfo, outInfo) {
		return fmt.Errorf("-out must differ from the unsigned package; the original is never replaced")
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if statErr == nil && !outInfo.Mode().IsRegular() {
		return fmt.Errorf("-out must name a regular file, not replace another file type")
	}
	for _, input := range []string{signature, platformKey} {
		if input == "" {
			continue
		}
		path, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if path == out || err == nil && statErr == nil && os.SameFile(info, outInfo) {
			return fmt.Errorf("-out must not replace a signature or pinned public key input")
		}
	}
	verifier := findVerifier()
	if verifier == "" {
		return fmt.Errorf("no host verifier: set AII_OS_BIN to a directory holding aii, or put aii on PATH")
	}

	data, err := readSigningFile(in, aiiospkg.MaxBundleBytes)
	if err != nil {
		return err
	}
	tree, err := aiiospkg.ReadBundle(data)
	if err != nil {
		return fmt.Errorf("unsigned package: %w", err)
	}
	for rel := range tree.Files {
		if strings.HasPrefix(rel, "signatures/") {
			return fmt.Errorf("unsigned package already carries signature member %q", rel)
		}
	}
	manifest := tree.Files["manifest.json"]
	canonical, err := aiiospkg.CanonicalizeV1(manifest)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	var m struct {
		Kind, ID, Version string
		PackageHash       string `json:"package_hash"`
	}
	if err := json.Unmarshal(canonical, &m); err != nil {
		return err
	}
	if (m.Kind != "plugin" && m.Kind != "engine") || m.ID == "" || m.Version == "" || tree.Root != m.ID+"-"+m.Version {
		return fmt.Errorf("package root must name its plugin or engine manifest identity and version")
	}
	packageHash := aiiospkg.PackageHash(tree.InstallFiles())
	manifestHash, err := aiiospkg.ManifestHash(manifest)
	if err != nil {
		return err
	}
	if packageHash != m.PackageHash {
		return fmt.Errorf("manifest package_hash differs from the unsigned install-root bytes")
	}
	sig, err := readSigningFile(signature, 1<<20)
	if err != nil {
		return err
	}
	canonicalSig, err := aiiospkg.CanonicalizeV1(sig)
	if err != nil {
		return fmt.Errorf("platform envelope: %w", err)
	}
	var env aiiospkg.Envelope
	if err := json.Unmarshal(canonicalSig, &env); err != nil {
		return err
	}
	var pair hashPair
	dec := json.NewDecoder(bytes.NewReader(env.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pair); err != nil {
		return fmt.Errorf("platform envelope requires the closed package_hash/manifest_hash pair: %w", err)
	}
	if env.ArtifactKind != platformArtifactKind || pair.PackageHash != packageHash || pair.ManifestHash != manifestHash {
		return fmt.Errorf("platform envelope does not bind this package's exact hashes as %s", platformArtifactKind)
	}

	tree.Add("signatures/platform.sig", sig)
	bundle, err := aiiospkg.WriteBundle(tree)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".aiisdk-platform-*.aiiospkg")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bundle); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	args := []string{"plugin", "verify"}
	if platformKey != "" {
		args = append(args, "-platform-key", platformKey)
	}
	if trustDir != "" {
		args = append(args, "-trust-dir", trustDir)
	}
	args = append(args, tmp.Name())
	summary, diagnostics, err := verifyPackageCommand(exec.Command(verifier, args...), m.ID, m.Version, packageHash, manifestHash)
	if err != nil {
		return fmt.Errorf("platform attachment not verified: %w; diagnostics: %s", err, diagnostics)
	}
	if summary != "VERIFIED T3 "+m.ID+" "+m.Version {
		return fmt.Errorf("host did not prove T3: %s", summary)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return err
	}
	fmt.Printf("attached platform signature: %s\n%s\n  package_hash %s\n  manifest_hash %s\n", out, summary, packageHash, manifestHash)
	return nil
}

func readSigningFile(path string, maxBytes int64) ([]byte, error) {

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes {
		return nil, fmt.Errorf("%s must be a regular file of 1..%d bytes", path, maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes {
		return nil, fmt.Errorf("%s must be a regular file of 1..%d bytes", path, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s grew past its %d-byte ceiling", path, maxBytes)
	}
	return data, nil
}
