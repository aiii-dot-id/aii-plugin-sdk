package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// .
// .
// .
// .
const catalogMaxTarBytes = int64(2147483648) + 2048*512 + 1024*1024 + 1023*511 + 2*512

type catalogVariant struct {
	ID       string `json:"variant_id"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Runtime  string `json:"execution_runtime"`
}

type catalogManifest struct {
	Kind        string           `json:"kind"`
	ID          string           `json:"id"`
	Version     string           `json:"version"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Variants    []catalogVariant `json:"variants"`
}

func readCatalogManifest(data []byte) (*catalogManifest, error) {
	compressed := bytes.NewReader(data)
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("package gzip: %w", err)
	}
	defer z.Close()
	z.Multistream(false)
	limited := &io.LimitedReader{R: z, N: catalogMaxTarBytes + 1}
	r := tar.NewReader(limited)
	root := ""
	seen := map[string]bool{}
	var manifest []byte
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if limited.N <= 0 {
				return nil, fmt.Errorf("package exceeds the canonical tar ceiling (%d bytes)", catalogMaxTarBytes)
			}
			return nil, fmt.Errorf("package tar: %w", err)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if root == "" {
			if h.Typeflag != tar.TypeDir || name == "" || strings.Contains(name, "/") {
				return nil, fmt.Errorf("package must begin with its sole root directory")
			}
			root = name
		}
		if seen[name] || len(seen) >= 1024 || (name != root && !strings.HasPrefix(name, root+"/")) {
			return nil, fmt.Errorf("duplicate, excessive or foreign package member %q", name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." || strings.Contains(part, "\\") {
				return nil, fmt.Errorf("invalid package member %q", name)
			}
		}
		seen[name] = true
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return nil, fmt.Errorf("package member %q is not a regular file or directory", name)
		}
		if name == root+"/manifest.json" {
			if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 1<<20 {
				return nil, fmt.Errorf("manifest must be a regular file of 1..1048576 bytes")
			}
			manifest, err = io.ReadAll(r)
		} else {
			_, err = io.Copy(io.Discard, r)
		}
		if err != nil {
			if limited.N <= 0 {
				return nil, fmt.Errorf("package exceeds the canonical tar ceiling (%d bytes)", catalogMaxTarBytes)
			}
			return nil, fmt.Errorf("package member %q: %w", name, err)
		}
	}
	if root == "" {
		return nil, fmt.Errorf("package is empty")
	}
	if manifest == nil {
		return nil, fmt.Errorf("package has no manifest.json under its root %q — is this an .aiiospkg from 'aiisdk package'?", root)
	}
	// .
	// .
	// .
	extra, err := io.Copy(io.Discard, limited)
	if err != nil {
		return nil, fmt.Errorf("package trailer: %w", err)
	}
	if limited.N <= 0 || extra != 0 || compressed.Len() != 0 {
		return nil, fmt.Errorf("package has excess tar bytes or trailing data")
	}
	canonical, err := aiiospkg.CanonicalizeV1(manifest)
	if err != nil {
		return nil, fmt.Errorf("package manifest: %w", err)
	}
	var m catalogManifest
	if err := json.Unmarshal(canonical, &m); err != nil {
		return nil, fmt.Errorf("package manifest: %w", err)
	}
	if m.Kind != "plugin" || m.ID == "" || m.Version == "" || len(m.Variants) == 0 {
		return nil, fmt.Errorf("package manifest requires plugin kind, identity, version and variants")
	}
	// .
	// .
	// .
	if root != m.ID+"-"+m.Version {
		return nil, fmt.Errorf("package root %q does not name its manifest's %s-%s", root, m.ID, m.Version)
	}
	return &m, nil
}

func (v catalogVariant) String() string {
	return fmt.Sprintf("%s (%s/%s, %s)", v.ID, v.Platform, v.Arch, v.Runtime)
}

// .
// .
func (m *catalogManifest) matchesAuthor(cfg *aiiospkg.AuthorConfig) error {
	if m.ID != cfg.ID || m.Version != cfg.Version {
		return fmt.Errorf("packaged identity %s@%s differs from plugin.json %s@%s; rebuild or select the matching package", m.ID, m.Version, cfg.ID, cfg.Version)
	}
	want := make(map[catalogVariant]bool, len(cfg.Variants))
	authored := make([]string, 0, len(cfg.Variants))
	byID := map[string]catalogVariant{}
	for _, v := range cfg.Variants {
		cv := catalogVariant{v.VariantID, v.Platform, v.Arch, v.ExecutionRuntime}
		want[cv] = true
		byID[cv.ID] = cv
		authored = append(authored, cv.String())
	}
	if len(want) != len(cfg.Variants) || len(want) != len(m.Variants) {
		packaged := make([]string, 0, len(m.Variants))
		for _, v := range m.Variants {
			packaged = append(packaged, v.String())
		}
		return fmt.Errorf("packaged variant set differs from plugin.json: the package carries %s; plugin.json declares %s; rebuild or select the matching package",
			strings.Join(packaged, ", "), strings.Join(authored, ", "))
	}
	for _, v := range m.Variants {
		if v.ID == "" || v.Platform == "" || v.Arch == "" || v.Runtime == "" || !want[v] {
			if a, ok := byID[v.ID]; ok {
				return fmt.Errorf("packaged variant %s differs from plugin.json's %s; rebuild or select the matching package", v, a)
			}
			return fmt.Errorf("packaged variant %s differs from plugin.json, which declares no variant %q; rebuild or select the matching package", v, v.ID)
		}
		delete(want, v)
	}
	return nil
}
