package aiiospkg

import (
	"archive/tar"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

var gzipHeaderCanonical = [10]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff}

const maxTarBytes = maxRegularPayloadBytes + 2*maxSemanticMembers*tarBlockBytes +
	maxSemanticMembers*1024 + (maxSemanticMembers-1)*(tarBlockBytes-1) + 2*tarBlockBytes

const MaxBundleBytes = 18 + 128 + maxTarBytes*110/100

func ReadBundle(data []byte) (*Tree, error) {
	if int64(len(data)) > MaxBundleBytes {
		return nil, fmt.Errorf("package exceeds its canonical compressed-input ceiling")
	}
	if len(data) < len(gzipHeaderCanonical) || !bytes.Equal(data[:len(gzipHeaderCanonical)], gzipHeaderCanonical[:]) {
		return nil, fmt.Errorf("package gzip header is not canonical")
	}
	compressed := bytes.NewReader(data)
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("package gzip: %w", err)
	}
	defer z.Close()
	z.Multistream(false)

	limited := &io.LimitedReader{R: z, N: maxTarBytes + 1}
	rawHash := sha256.New()
	r := tar.NewReader(io.TeeReader(limited, rawHash))
	var tree *Tree
	seen := map[string]bool{}
	var payload int64
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("package tar: %w", err)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if seen[name] || len(seen) >= maxSemanticMembers {
			return nil, fmt.Errorf("duplicate or excessive package member %q", name)
		}
		seen[name] = true
		if tree == nil {
			if h.Typeflag != tar.TypeDir || strings.Contains(name, "/") || name == "" {
				return nil, fmt.Errorf("package must begin with its sole root directory")
			}
			tree = NewTree(name)
		}
		if err := validatePath(name); err != nil {
			return nil, err
		}
		if name != tree.Root && !strings.HasPrefix(name, tree.Root+"/") {
			return nil, fmt.Errorf("member %q escapes the package root", name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Mode != tarModeDir || h.Size != 0 {
				return nil, fmt.Errorf("directory %q has noncanonical mode or size", name)
			}
			if name != tree.Root {
				tree.Dirs[strings.TrimPrefix(name, tree.Root+"/")] = true
			}
		case tar.TypeReg:
			if h.Mode != tarModeRegular && h.Mode != tarModeExecutable {
				return nil, fmt.Errorf("member %q has noncanonical mode", name)
			}
			if h.Size < 0 || h.Size > maxRegularPayloadBytes-payload {
				return nil, fmt.Errorf("package regular payload exceeds its canonical ceiling")
			}

			if (name == tree.Root+"/manifest.json" || strings.HasPrefix(name, tree.Root+"/signatures/")) && h.Size > 1<<20 {
				return nil, fmt.Errorf("JSON member %q exceeds the host's 1 MiB ceiling", name)
			}
			payload += h.Size
			content, err := io.ReadAll(r)
			if err != nil {
				return nil, fmt.Errorf("member %q: %w", name, err)
			}
			rel := strings.TrimPrefix(name, tree.Root+"/")
			tree.Add(rel, content)
			tree.Exec[rel] = h.Mode == tarModeExecutable
		default:
			return nil, fmt.Errorf("member %q is not a regular file or directory", name)
		}
	}
	if _, err := io.Copy(rawHash, limited); err != nil {
		return nil, fmt.Errorf("package trailer: %w", err)
	}
	if limited.N <= 0 || compressed.Len() != 0 {
		return nil, fmt.Errorf("package exceeds its tar ceiling or has trailing data")
	}
	if tree == nil {
		return nil, fmt.Errorf("package is empty")
	}
	members, err := tree.Members()
	if err != nil {
		return nil, err
	}
	wantHash := sha256.New()
	if err := writeCanonicalTarTo(wantHash, members); err != nil {
		return nil, err
	}
	if !bytes.Equal(rawHash.Sum(nil), wantHash.Sum(nil)) {
		return nil, fmt.Errorf("package tar differs from the canonical member stream")
	}
	return tree, nil
}

func WriteBundle(t *Tree) ([]byte, error) {
	ms, err := t.Members()
	if err != nil {
		return nil, err
	}
	tarBytes, err := writeCanonicalTar(ms)
	if err != nil {
		return nil, err
	}
	return gzipWrap(tarBytes)
}

func writeCanonicalTar(ms []Member) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonicalTarTo(&buf, ms); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonicalTarTo(out io.Writer, ms []Member) error {
	for _, m := range ms {
		typ, mode := tarTypeRegular, m.Mode
		if m.IsDir {
			typ, mode = tarTypeDirectory, tarModeDir
		}
		stored := m.Path
		if m.IsDir {
			stored += "/"
		}
		if pathFitsUSTAR(stored) {
			hdr, err := buildTarHeader(stored, int64(len(m.Content)), mode, typ)
			if err != nil {
				return fmt.Errorf("member %q: %w", m.Path, err)
			}
			if _, err := out.Write(hdr[:]); err != nil {
				return err
			}
		} else {

			record, err := paxBuildPathRecord(m.Path)
			if err != nil {
				return fmt.Errorf("member %q: %w", m.Path, err)
			}
			paxHdr, err := buildTarHeader(paxHeaderPath, int64(len(record)), tarModeRegular, tarTypePAXLocal)
			if err != nil {
				return fmt.Errorf("member %q PAX header: %w", m.Path, err)
			}
			if _, err := out.Write(paxHdr[:]); err != nil {
				return err
			}
			if _, err := io.WriteString(out, record); err != nil {
				return err
			}
			if err := writeBlockPadding(out, len(record)); err != nil {
				return err
			}
			hdr, err := buildTarHeader(paxMemberPlaceholder, int64(len(m.Content)), mode, typ)
			if err != nil {
				return fmt.Errorf("member %q: %w", m.Path, err)
			}
			if _, err := out.Write(hdr[:]); err != nil {
				return err
			}
		}
		if !m.IsDir {
			if _, err := out.Write(m.Content); err != nil {
				return err
			}
			if err := writeBlockPadding(out, len(m.Content)); err != nil {
				return err
			}
		}
	}
	_, err := out.Write(make([]byte, 2*tarBlockBytes))
	return err
}

func writeBlockPadding(out io.Writer, contentLen int) error {
	if pad := contentLen % tarBlockBytes; pad != 0 {
		_, err := out.Write(make([]byte, tarBlockBytes-pad))
		return err
	}
	return nil
}

func gzipWrap(tarBytes []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(gzipHeaderCanonical[:])
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(tarBytes); err != nil {
		return nil, err
	}
	if err := fw.Close(); err != nil {
		return nil, err
	}
	var trailer [8]byte
	binary.LittleEndian.PutUint32(trailer[0:4], crc32.ChecksumIEEE(tarBytes))
	binary.LittleEndian.PutUint32(trailer[4:8], uint32(len(tarBytes)))
	buf.Write(trailer[:])
	return buf.Bytes(), nil
}

func tarSplitPath(path string) (name, prefix string, err error) {
	if len(path) == 0 || len(path) > maxMemberPathBytes {
		return "", "", fmt.Errorf("stored path length %d outside canonical bounds", len(path))
	}
	if len(path) <= 100 {
		return path, "", nil
	}
	for split := len(path) - 1; split > 0; split-- {
		if path[split] != '/' {
			continue
		}
		prefixLen := split
		nameLen := len(path) - split - 1
		if prefixLen <= 155 && nameLen > 0 && nameLen <= 100 {
			return path[split+1:], path[:split], nil
		}
	}
	return "", "", fmt.Errorf("stored path does not fit USTAR name/prefix")
}

func pathFitsUSTAR(stored string) bool {
	_, _, err := tarSplitPath(stored)
	return err == nil
}

func writeOctal(field []byte, value int64) error {
	width := len(field) - 1
	s := strconv.FormatInt(value, 8)
	if len(s) > width {
		return fmt.Errorf("octal value needs %d digits, field holds %d", len(s), width)
	}
	for i := 0; i < width-len(s); i++ {
		field[i] = '0'
	}
	copy(field[width-len(s):], s)
	field[width] = 0
	return nil
}

func buildTarHeader(storedPath string, size, mode int64, typ byte) ([tarBlockBytes]byte, error) {
	var h [tarBlockBytes]byte
	name, prefix, err := tarSplitPath(storedPath)
	if err != nil {
		return h, err
	}
	copy(h[0:100], name)
	copy(h[345:500], prefix)
	if err := writeOctal(h[100:108], mode&0o777); err != nil {
		return h, err
	}
	if err := writeOctal(h[124:136], size); err != nil {
		return h, err
	}
	if err := writeOctal(h[108:116], 0); err != nil {
		return h, err
	}
	if err := writeOctal(h[116:124], 0); err != nil {
		return h, err
	}
	if err := writeOctal(h[136:148], 0); err != nil {
		return h, err
	}
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	h[156] = typ
	copy(h[257:263], "ustar\x00")
	copy(h[263:265], "00")
	var sum int64
	for _, b := range h {
		sum += int64(b)
	}
	chk := strconv.FormatInt(sum, 8)
	if len(chk) > 6 {
		return h, fmt.Errorf("checksum overflows canonical field")
	}
	for i := 0; i < 6-len(chk); i++ {
		h[148+i] = '0'
	}
	copy(h[148+6-len(chk):154], chk)
	h[154] = 0
	h[155] = ' '
	return h, nil
}

func paxBuildPathRecord(path string) (string, error) {
	if len(path) == 0 || len(path) > maxMemberPathBytes {
		return "", fmt.Errorf("PAX path length %d outside canonical bounds", len(path))
	}
	digits := 1
	total := len(path) + 7 + digits
	for {
		nextDigits := len(strconv.Itoa(total))
		if nextDigits == digits {
			break
		}
		digits = nextDigits
		total = len(path) + 7 + digits
	}
	record := fmt.Sprintf("%d path=%s\n", total, path)
	if len(record) != total {
		return "", fmt.Errorf("PAX record length self-consistency failed")
	}
	return record, nil
}
