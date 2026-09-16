package aiiospkg

// .
// .
// .
// .
// .
// .

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	tarBlockBytes = 512

	// .
	// .
	maxComponentBytes  = 255
	maxMemberPathBytes = 511

	// .
	maxSemanticMembers = 1024

	// .
	maxRegularPayloadBytes = int64(2147483648)

	// .
	paxHeaderPath        = "PaxHeaders/aiiospkg"
	paxMemberPlaceholder = "PaxPayload/aiiospkg"

	tarTypeRegular   = byte('0')
	tarTypeDirectory = byte('5')
	tarTypePAXLocal  = byte('x')

	tarModeDir        = int64(0o755)
	tarModeRegular    = int64(0o644)
	tarModeExecutable = int64(0o755)
)

// .
// .
// .
type Member struct {
	Path    string
	IsDir   bool
	Mode    int64
	Content []byte
}

// .
// .
// .
// .
// .
type Tree struct {
	Root  string
	Files map[string][]byte
	// .
	Exec map[string]bool
}

// .
func NewTree(root string) *Tree {
	return &Tree{Root: root, Files: map[string][]byte{}, Exec: map[string]bool{}}
}

// .
func (t *Tree) Add(rel string, content []byte) {
	t.Files[rel] = content
}

// .
// .
// .
// .
func (t *Tree) Members() ([]Member, error) {
	dirs := map[string]bool{t.Root: true}
	var files []Member
	rels := make([]string, 0, len(t.Files))
	for rel := range t.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		full := t.Root + "/" + rel
		for i := len(t.Root) + 1; i < len(full); i++ {
			if full[i] == '/' {
				dirs[full[:i]] = true
			}
		}
		mode := tarModeRegular
		if t.Exec[rel] {
			mode = tarModeExecutable
		}
		files = append(files, Member{Path: full, Mode: mode, Content: t.Files[rel]})
	}
	all := make([]Member, 0, len(dirs)+len(files))
	for d := range dirs {
		all = append(all, Member{Path: d, IsDir: true, Mode: tarModeDir})
	}
	all = append(all, files...)
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	if err := validateMembers(t.Root, all); err != nil {
		return nil, err
	}
	return all, nil
}

// .
// .
// .
// .
// .
func validateMembers(root string, ms []Member) error {
	if err := validatePath(root); err != nil {
		return fmt.Errorf("package root: %w", err)
	}
	if strings.Contains(root, "/") {
		return fmt.Errorf("package root %q must be a single component", root)
	}
	if len(ms) > maxSemanticMembers {
		return fmt.Errorf("package has %d members; the canonical ceiling is %d including the root", len(ms), maxSemanticMembers)
	}
	var payload int64
	for i, m := range ms {
		if err := validatePath(m.Path); err != nil {
			return err
		}
		if m.Path != root && !strings.HasPrefix(m.Path, root+"/") {
			return fmt.Errorf("member %q escapes the sole top-level directory %q", m.Path, root)
		}
		if !m.IsDir {
			if m.Mode != tarModeRegular && m.Mode != tarModeExecutable {
				return fmt.Errorf("member %q mode %04o is not canonical (0644 or 0755)", m.Path, m.Mode)
			}
			if int64(len(m.Content)) > maxRegularPayloadBytes-payload {
				return fmt.Errorf("regular payload exceeds the inclusive %d-byte sum ceiling", maxRegularPayloadBytes)
			}
			payload += int64(len(m.Content))
		}
		for _, prev := range ms[:i] {
			if prev.Path == m.Path {
				return fmt.Errorf("duplicate member %q", m.Path)
			}
			if casefoldSiblingCollision(prev.Path, m.Path) {
				return fmt.Errorf("members %q and %q collide under ASCII casefold — case-insensitive filesystems would merge them", prev.Path, m.Path)
			}
		}
	}
	return nil
}

// .
// .
func validatePath(path string) error {
	if len(path) == 0 || len(path) > maxMemberPathBytes {
		return fmt.Errorf("member path length %d is outside the canonical 1..%d bounds", len(path), maxMemberPathBytes)
	}
	for _, component := range strings.Split(path, "/") {
		if err := validateComponent(component); err != nil {
			return fmt.Errorf("member path %q: %w", path, err)
		}
	}
	return nil
}

func validateComponent(component string) error {
	if len(component) == 0 || len(component) > maxComponentBytes {
		return fmt.Errorf("component length %d is outside 1..%d", len(component), maxComponentBytes)
	}
	if component == "." || component == ".." {
		return fmt.Errorf("component %q is forbidden", component)
	}
	if component[len(component)-1] == '.' {
		return fmt.Errorf("component %q ends in a dot, which Windows cannot create", component)
	}
	if windowsDeviceStem(component) {
		return fmt.Errorf("component %q has a Windows device-name stem", component)
	}
	for i := 0; i < len(component); i++ {
		c := component[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '+' || c == '-') {
			return fmt.Errorf("component %q may use only [A-Za-z0-9._+-]", component)
		}
	}
	return nil
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func windowsDeviceStem(component string) bool {
	stem := component
	if i := strings.IndexByte(component, '.'); i >= 0 {
		stem = component[:i]
	}
	lower := make([]byte, len(stem))
	for i := 0; i < len(stem); i++ {
		lower[i] = asciiLower(stem[i])
	}
	switch string(lower) {
	case "con", "prn", "aux", "nul":
		return true
	}
	return len(lower) == 4 && (string(lower[:3]) == "com" || string(lower[:3]) == "lpt") &&
		lower[3] >= '1' && lower[3] <= '9'
}

// .
// .
// .
func casefoldSiblingCollision(left, right string) bool {
	for {
		li := strings.IndexByte(left, '/')
		ri := strings.IndexByte(right, '/')
		lc, rc := left, right
		if li >= 0 {
			lc = left[:li]
		}
		if ri >= 0 {
			rc = right[:ri]
		}
		if len(lc) != len(rc) {
			return false
		}
		exact := true
		for i := 0; i < len(lc); i++ {
			if asciiLower(lc[i]) != asciiLower(rc[i]) {
				return false
			}
			if lc[i] != rc[i] {
				exact = false
			}
		}
		if !exact {
			return true
		}
		if li < 0 || ri < 0 {
			return false
		}
		left, right = left[li+1:], right[ri+1:]
	}
}

// .
// .
// .
// .
func ReadTree(dir string) (*Tree, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := filepath.Base(abs)
	t := NewTree(root)
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == abs {
			return nil
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type().IsDir():
			return nil
		case d.Type().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// .
			// .
			// .
			// .
			// .
			t.Add(rel, content)
			return nil
		default:
			return fmt.Errorf("member %q is not a regular file or directory (mode %v) — links and specials are outside the package grammar", rel, d.Type())
		}
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// .
// .
func (t *Tree) InstallFiles() map[string][]byte {
	out := map[string][]byte{}
	for rel, content := range t.Files {
		if strings.HasPrefix(rel, "install-root/") {
			out[strings.TrimPrefix(rel, "install-root/")] = content
		}
	}
	return out
}
