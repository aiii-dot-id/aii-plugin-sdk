package aiiospkg

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
	"fmt"
	"regexp"
	"strings"
)

const (
	AcceleratorFile = "accelerator.json"
	ModelsFile      = "models.json"
	// .
	// .
	// .
	MaxModels        = 256
	MaxProfileModels = 128
	// .
	// .
	// .
	MaxStartupMS      = 3600000
	MaxModelBytes     = 16 << 30
	maxModelPathBytes = 255
	maxModelPathDepth = 8
)

var (
	reToken        = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)
	reModelName    = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,127}$`)
	reModelSegment = regexp.MustCompile(`^[A-Za-z0-9.][A-Za-z0-9._+-]{0,127}$`)
	reSHA256       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// .
	// .
	reWindowsDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)
)

// .
type AcceleratorProfile struct {
	OS               string   `json:"os"`
	Arch             string   `json:"arch"`
	Backend          string   `json:"backend"`
	Operators        []string `json:"operators,omitempty"`
	RuntimeLibraries []string `json:"runtime_libraries,omitempty"`
	Precision        string   `json:"precision"`
	Models           []string `json:"models"`
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	MemoryBytes int64 `json:"memory_bytes"`
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	DeviceMemoryBytes *int64 `json:"device_memory_bytes,omitempty"`
	// .
	// .
	// .
	// .
	// .
	// .
	StartupMS    *int64 `json:"startup_ms,omitempty"`
	SessionLimit int    `json:"session_limit"`
	Fallback     string `json:"fallback"`
}

// .
// .
// .
// .
type ModelDecl struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// .
func (d ModelDecl) Dest() string {
	if d.Path != "" {
		return d.Path
	}
	return d.Name
}

// .
// .
// .
// .
// .
// .
// .
// .
func validateModelPath(dest string) error {
	if len(dest) > maxModelPathBytes {
		return fmt.Errorf("longer than %d bytes", maxModelPathBytes)
	}
	segs := strings.Split(dest, "/")
	if len(segs) > maxModelPathDepth {
		return fmt.Errorf("deeper than %d segments", maxModelPathDepth)
	}
	for _, s := range segs {
		if !reModelSegment.MatchString(s) || strings.HasSuffix(s, ".") {
			return fmt.Errorf("segment %q is not a portable file name (letters, digits, dots, underscores, plus and dashes, up to 128 bytes, not ending in a dot; a leading dot is admitted)", s)
		}
		if reWindowsDevice.MatchString(s) {
			return fmt.Errorf("segment %q is a reserved device name on Windows", s)
		}
	}
	if strings.HasSuffix(dest, ".partial") {
		return fmt.Errorf("the .partial suffix is the host's")
	}
	return nil
}

// .
func ValidateAcceleratorProfile(variantID string, p *AcceleratorProfile) error {
	if p == nil {
		return nil
	}
	if !reToken.MatchString(p.OS) || !reToken.MatchString(p.Arch) || !reToken.MatchString(p.Backend) {
		return fmt.Errorf("variant %s: accelerator os, arch and backend are required tokens", variantID)
	}
	if !reToken.MatchString(p.Precision) {
		return fmt.Errorf("variant %s: accelerator precision is a required token (e.g. int8, fp16)", variantID)
	}
	if len(p.Operators) > 64 || len(p.RuntimeLibraries) > 32 || len(p.Models) > MaxProfileModels || len(p.Models) == 0 {
		return fmt.Errorf("variant %s: accelerator declares at most 64 operators, 32 runtime libraries and %d models, and at least one model", variantID, MaxProfileModels)
	}
	for _, list := range [][]string{p.Operators, p.RuntimeLibraries, p.Models} {
		for _, s := range list {
			if s == "" || len(s) > 128 || strings.ContainsAny(s, "\x00\r\n") {
				return fmt.Errorf("variant %s: accelerator list entries are short names", variantID)
			}
		}
	}
	if p.MemoryBytes <= 0 || p.SessionLimit <= 0 {
		return fmt.Errorf("variant %s: accelerator memory_bytes and session_limit are declared, positive numbers", variantID)
	}
	// .
	// .
	// .
	if p.DeviceMemoryBytes != nil && *p.DeviceMemoryBytes < 0 {
		return fmt.Errorf("variant %s: accelerator device_memory_bytes is a reservation in bytes (omit it where there is nothing to declare; 0 declares no device allocation)", variantID)
	}
	if p.StartupMS != nil && (*p.StartupMS <= 0 || *p.StartupMS > MaxStartupMS) {
		return fmt.Errorf("variant %s: accelerator startup_ms is an allowance of 1..%d milliseconds (omit it to take the host's default)", variantID, MaxStartupMS)
	}
	if p.Fallback != "none" && p.Fallback != "reported" {
		return fmt.Errorf("variant %s: accelerator fallback is \"none\" or \"reported\" — never taken silently", variantID)
	}
	return nil
}

// .
func ValidateModels(decls []ModelDecl) error {
	if len(decls) > MaxModels {
		return fmt.Errorf("%d models; at most %d", len(decls), MaxModels)
	}
	seen := map[string]bool{}
	dests := map[string]ModelDecl{}
	for i, d := range decls {
		if !reModelName.MatchString(d.Name) || strings.Contains(d.Name, "..") {
			return fmt.Errorf("model %d: name %q is not a file name of lowercase letters, digits, dots, plus and dashes", i, d.Name)
		}
		if seen[d.Name] {
			return fmt.Errorf("model %q declared twice", d.Name)
		}
		seen[d.Name] = true
		if err := validateModelPath(d.Dest()); err != nil {
			return fmt.Errorf("model %q: path %q: %v", d.Name, d.Dest(), err)
		}
		// .
		// .
		if other, dup := dests[strings.ToLower(d.Dest())]; dup {
			return fmt.Errorf("model %q: path %q collides with model %q's %q on a case-insensitive filesystem", d.Name, d.Dest(), other.Name, other.Dest())
		}
		dests[strings.ToLower(d.Dest())] = d
		if !strings.HasPrefix(d.URL, "https://") || len(d.URL) > 1024 {
			return fmt.Errorf("model %q: url must be https and short", d.Name)
		}
		if !reSHA256.MatchString(d.SHA256) {
			return fmt.Errorf("model %q: sha256 must be 64 hex digits", d.Name)
		}
		if d.Size <= 0 || d.Size > MaxModelBytes {
			return fmt.Errorf("model %q: size must be 1..%d bytes", d.Name, int64(MaxModelBytes))
		}
	}
	// .
	// .
	for a, da := range dests {
		for b, db := range dests {
			if strings.HasPrefix(b, a+"/") {
				return fmt.Errorf("model %q's path %q is a directory of model %q's %q", da.Name, da.Dest(), db.Name, db.Dest())
			}
		}
	}
	return nil
}

// .
// .
func AcceleratorJSON(variants []AuthorVariant) ([]byte, bool, error) {
	out := map[string]interface{}{}
	for _, v := range variants {
		if v.Accelerator == nil {
			continue
		}
		p := v.Accelerator
		entry := map[string]interface{}{"os": p.OS, "arch": p.Arch, "backend": p.Backend, "precision": p.Precision, "models": stringList(p.Models), "memory_bytes": p.MemoryBytes, "session_limit": p.SessionLimit, "fallback": p.Fallback}
		if p.DeviceMemoryBytes != nil {
			entry["device_memory_bytes"] = *p.DeviceMemoryBytes
		}
		if p.StartupMS != nil {
			entry["startup_ms"] = *p.StartupMS
		}
		if len(p.Operators) > 0 {
			entry["operators"] = stringList(p.Operators)
		}
		if len(p.RuntimeLibraries) > 0 {
			entry["runtime_libraries"] = stringList(p.RuntimeLibraries)
		}
		out[v.VariantID] = entry
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	b, err := marshalCanonical(out)
	return b, true, err
}

// .
func ModelsJSON(decls []ModelDecl) ([]byte, error) {
	if err := ValidateModels(decls); err != nil {
		return nil, err
	}
	list := make([]interface{}, 0, len(decls))
	for _, d := range decls {
		entry := map[string]interface{}{"name": d.Name, "url": d.URL, "sha256": d.SHA256, "size": d.Size}
		if d.Path != "" {
			entry["path"] = d.Path
		}
		list = append(list, entry)
	}
	return marshalCanonical(list)
}

func stringList(in []string) []interface{} {
	out := make([]interface{}, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}
