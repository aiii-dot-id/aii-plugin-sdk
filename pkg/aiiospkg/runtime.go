package aiiospkg

// .
// .
// .
// .
// .
// .
// .
// .

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	RuntimesFile  = "runtime.json"
	InventoryFile = "inventory.json"
	MaxRuntimes   = 8
)

// .
// .
// .
// .
// .
type TreeLimits struct {
	MaxInstalledBytes  int64
	MaxFiles           int
	MaxFileBytes       int64
	MaxCompressedBytes int64
	MaxDepth           int
	MaxInventoryBytes  int64
}

// .
// .
// .
// .
// .
// .
var RuntimeLimits = TreeLimits{MaxInstalledBytes: 1 << 30, MaxFiles: 32768, MaxFileBytes: 512 << 20, MaxCompressedBytes: 512 << 20, MaxDepth: 24, MaxInventoryBytes: 16 << 20}

// .
func (l TreeLimits) filled() TreeLimits {
	d := RuntimeLimits
	if l.MaxInstalledBytes <= 0 {
		l.MaxInstalledBytes = d.MaxInstalledBytes
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = d.MaxFiles
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxCompressedBytes <= 0 {
		l.MaxCompressedBytes = d.MaxCompressedBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxInventoryBytes <= 0 {
		l.MaxInventoryBytes = d.MaxInventoryBytes
	}
	return l
}

// .
const (
	CeilingInstalledBytes  = "plugins.runtime.max_installed_bytes"
	CeilingFiles           = "plugins.runtime.max_files"
	CeilingFileBytes       = "plugins.runtime.max_file_bytes"
	CeilingCompressedBytes = "plugins.runtime.max_compressed_bytes"
	CeilingDepth           = "plugins.runtime.max_depth"
)

// .
func FormatCeilings(req map[string]int64) string {
	keys := make([]string, 0, len(req))
	for k := range req {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s >= %d", k, req[k]))
	}
	return strings.Join(parts, ", ")
}

// .
type RuntimeDecl struct {
	VariantID       string `json:"variant_id"`
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	InstalledBytes  int64  `json:"installed_bytes"`
	Files           int    `json:"files"`
	InventorySHA256 string `json:"inventory_sha256"`
}

// .
func ValidateRuntimes(decls []RuntimeDecl, variants []AuthorVariant) error {
	if len(decls) > MaxRuntimes {
		return fmt.Errorf("%d runtimes; at most %d", len(decls), MaxRuntimes)
	}
	known := map[string]bool{}
	for _, v := range variants {
		known[v.VariantID] = true
	}
	seen := map[string]bool{}
	for i, d := range decls {
		if !known[d.VariantID] {
			return fmt.Errorf("runtime %d: variant %q is not declared", i, d.VariantID)
		}
		if seen[d.VariantID] {
			return fmt.Errorf("runtime %d: variant %s declared twice", i, d.VariantID)
		}
		seen[d.VariantID] = true
		if !strings.HasPrefix(d.URL, "https://") {
			return fmt.Errorf("runtime %d: url must be https", i)
		}
		if !reSHA256.MatchString(d.SHA256) || !reSHA256.MatchString(d.InventorySHA256) {
			return fmt.Errorf("runtime %d: sha256 and inventory_sha256 are 64 hex digits", i)
		}
		if d.Size <= 0 || d.InstalledBytes <= 0 || d.Files <= 0 {
			return fmt.Errorf("runtime %d: size, installed_bytes and files are positive", i)
		}
	}
	return nil
}

// .
// .
// .
// .
// .
// .
func DeclaredCeilings(d RuntimeDecl) map[string]int64 {
	req := map[string]int64{}
	if d.InstalledBytes > RuntimeLimits.MaxInstalledBytes {
		req[CeilingInstalledBytes] = d.InstalledBytes
	}
	if d.Files > RuntimeLimits.MaxFiles {
		req[CeilingFiles] = int64(d.Files)
	}
	if d.Size > RuntimeLimits.MaxCompressedBytes {
		req[CeilingCompressedBytes] = d.Size
	}
	return req
}

// .
func RuntimesJSON(decls []RuntimeDecl) ([]byte, error) {
	return marshalCanonical(struct {
		Runtimes []RuntimeDecl `json:"runtimes"`
	}{decls})
}

// .
type InventoryEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode"`
}

type Inventory struct {
	Files          []InventoryEntry `json:"files"`
	InstalledBytes int64            `json:"installed_bytes"`
}

// .
// .
type RuntimeArchive struct {
	SHA256          string
	Size            int64
	InstalledBytes  int64
	Files           int
	InventorySHA256 string
	// .
	// .
	LargestFileBytes int64
	Depth            int
}

// .
// .
// .
func (a *RuntimeArchive) RequiredCeilings() map[string]int64 {
	d := RuntimeLimits
	req := map[string]int64{}
	if a.InstalledBytes > d.MaxInstalledBytes {
		req[CeilingInstalledBytes] = a.InstalledBytes
	}
	if a.Files > d.MaxFiles {
		req[CeilingFiles] = int64(a.Files)
	}
	if a.LargestFileBytes > d.MaxFileBytes {
		req[CeilingFileBytes] = a.LargestFileBytes
	}
	if a.Size > d.MaxCompressedBytes {
		req[CeilingCompressedBytes] = a.Size
	}
	if a.Depth > d.MaxDepth {
		req[CeilingDepth] = int64(a.Depth)
	}
	return req
}

// .
// .
// .
// .
// .
func WriteRuntimeTree(w io.Writer, dir, root string) (*RuntimeArchive, error) {
	return writeRuntimeTree(w, dir, root, RuntimeLimits)
}

// .
// .
// .
// .
func WriteRuntimeTreeWithin(w io.Writer, dir, root string, budget TreeLimits) (*RuntimeArchive, error) {
	return writeRuntimeTree(w, dir, root, budget)
}

// .
// .
const maxTreePathBytes = 511

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
func treeSegmentForbidden(seg string) bool {
	if len(seg) == 0 || len(seg) > 255 || seg == "." || seg == ".." || strings.HasSuffix(seg, ".") || reWindowsDevice.MatchString(seg) {
		return true
	}
	if seg[0] == ' ' || seg[len(seg)-1] == ' ' {
		return true
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '+' || c == '-' || c == ' ' || c == '(' || c == ')' {
			continue
		}
		return true
	}
	return false
}

func writeRuntimeTree(w io.Writer, dir, root string, budget TreeLimits) (*RuntimeArchive, error) {
	budget = budget.filled()
	if root == "" || strings.Contains(root, "/") || treeSegmentForbidden(root) {
		return nil, fmt.Errorf("root %q must be one admitted path component", root)
	}
	type source struct {
		rel, abs, sha, mode string
		size                int64
	}
	var files []source
	var total, largest int64
	depth := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; a runtime tree carries no links", rel)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		if rel == InventoryFile {
			return fmt.Errorf("%s is the archive's own member; the tree must not carry one", InventoryFile)
		}
		segs := strings.Split(rel, "/")
		if len(segs) > budget.MaxDepth {
			return fmt.Errorf("%s is deeper than %d segments (-max-depth)", rel, budget.MaxDepth)
		}
		if len(segs) > depth {
			depth = len(segs)
		}
		for _, seg := range segs {
			if treeSegmentForbidden(seg) {
				return fmt.Errorf("%s: segment %q is not admitted by the grammar", rel, seg)
			}
		}
		if len(root)+1+len(rel) > maxTreePathBytes {
			return fmt.Errorf("%s: the member path exceeds %d bytes", rel, maxTreePathBytes)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.Size() > budget.MaxFileBytes {
			return fmt.Errorf("%s is %d bytes; the budget is %d per file (-max-file-bytes)", rel, info.Size(), budget.MaxFileBytes)
		}
		if info.Size() > largest {
			largest = info.Size()
		}
		sum, herr := hashFileHex(path)
		if herr != nil {
			return herr
		}
		mode := "file"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "exec"
		}
		files = append(files, source{rel: rel, abs: path, size: info.Size(), sha: "sha256:" + sum, mode: mode})
		total += info.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s holds no files", dir)
	}
	if len(files) > budget.MaxFiles {
		return nil, fmt.Errorf("%d files exceed the budget of %d (-max-files); a tree past the host's defaults needs the operator's ceilings, which the report names", len(files), budget.MaxFiles)
	}
	if total > budget.MaxInstalledBytes {
		return nil, fmt.Errorf("%d installed bytes exceed the budget of %d (-max-installed-bytes); a tree past the host's defaults needs the operator's ceilings, which the report names", total, budget.MaxInstalledBytes)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	inv := Inventory{InstalledBytes: total}
	for _, f := range files {
		inv.Files = append(inv.Files, InventoryEntry{Path: f.rel, Size: f.size, SHA256: f.sha, Mode: f.mode})
	}
	inventory, err := json.Marshal(inv)
	if err != nil {
		return nil, err
	}
	if int64(len(inventory)) > budget.MaxInventoryBytes {
		return nil, fmt.Errorf("the inventory is %d bytes; at most %d", len(inventory), budget.MaxInventoryBytes)
	}
	type member struct {
		path  string
		isDir bool
		src   *source
	}
	members := []member{{path: root, isDir: true}, {path: root + "/" + InventoryFile}}
	var rest []member
	seenDir := map[string]bool{}
	for i := range files {
		f := &files[i]
		parts := strings.Split(f.rel, "/")
		for j := 1; j < len(parts); j++ {
			d := strings.Join(parts[:j], "/")
			if !seenDir[d] {
				seenDir[d] = true
				rest = append(rest, member{path: root + "/" + d, isDir: true})
			}
		}
		rest = append(rest, member{path: root + "/" + f.rel, src: f})
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].path < rest[j].path })
	members = append(members, rest...)

	hw := sha256.New()
	cw := &countingWriter{w: io.MultiWriter(w, hw)}
	zw, err := gzip.NewWriterLevel(cw, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zw.Header.OS = 255
	for _, m := range members {
		typ := byte(tarTypeRegular)
		mode := int64(tarModeRegular)
		var size int64
		switch {
		case m.isDir:
			typ, mode = tarTypeDirectory, tarModeDir
		case m.src != nil:
			size = m.src.size
			if m.src.mode == "exec" {
				mode = tarModeExecutable
			}
		default:
			size = int64(len(inventory))
		}
		if err := writeMemberHeader(zw, m.path, size, mode, typ); err != nil {
			return nil, err
		}
		if m.isDir {
			continue
		}
		if m.src == nil {
			if _, err := zw.Write(inventory); err != nil {
				return nil, err
			}
		} else {
			f, err := os.Open(m.src.abs)
			if err != nil {
				return nil, err
			}
			n, cerr := io.Copy(zw, f)
			f.Close()
			if cerr != nil {
				return nil, cerr
			}
			if n != m.src.size {
				return nil, fmt.Errorf("%s changed size while packing", m.src.rel)
			}
		}
		if pad := size % tarBlockBytes; pad != 0 {
			if _, err := zw.Write(make([]byte, tarBlockBytes-pad)); err != nil {
				return nil, err
			}
		}
	}
	if _, err := zw.Write(make([]byte, 2*tarBlockBytes)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if cw.n > budget.MaxCompressedBytes {
		return nil, fmt.Errorf("the archive is %d bytes compressed; the budget is %d (-max-compressed-bytes)", cw.n, budget.MaxCompressedBytes)
	}
	invSum := sha256.Sum256(inventory)
	return &RuntimeArchive{
		SHA256: hex.EncodeToString(hw.Sum(nil)), Size: cw.n, InstalledBytes: total, Files: len(files),
		InventorySHA256: hex.EncodeToString(invSum[:]), LargestFileBytes: largest, Depth: depth,
	}, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func hashFileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// .
// .
// .
func writeMemberHeader(w io.Writer, path string, size, mode int64, typ byte) error {
	stored := path
	if typ == tarTypeDirectory {
		stored = path + "/"
	}
	if pathFitsUSTAR(stored) {
		hdr, err := buildTarHeader(stored, size, mode, typ)
		if err != nil {
			return fmt.Errorf("header %q: %v", path, err)
		}
		_, err = w.Write(hdr[:])
		return err
	}
	record, err := paxBuildPathRecord(path)
	if err != nil {
		return fmt.Errorf("pax record %q: %v", path, err)
	}
	paxHdr, err := buildTarHeader(paxHeaderPath, int64(len(record)), tarModeRegular, tarTypePAXLocal)
	if err != nil {
		return err
	}
	if _, err := w.Write(paxHdr[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(w, record); err != nil {
		return err
	}
	if pad := len(record) % tarBlockBytes; pad != 0 {
		if _, err := w.Write(make([]byte, tarBlockBytes-pad)); err != nil {
			return err
		}
	}
	hdr, err := buildTarHeader(paxMemberPlaceholder, size, mode, typ)
	if err != nil {
		return err
	}
	_, err = w.Write(hdr[:])
	return err
}
