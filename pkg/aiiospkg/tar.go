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
// .
// .
// .

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
)

// .
var gzipHeaderCanonical = [10]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff}

// .
// .
// .
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
				return nil, fmt.Errorf("member %q: %w", m.Path, err)
			}
			buf.Write(hdr[:])
		} else {
			// .
			// .
			// .
			// .
			// .
			// .
			record, err := paxBuildPathRecord(m.Path)
			if err != nil {
				return nil, fmt.Errorf("member %q: %w", m.Path, err)
			}
			paxHdr, err := buildTarHeader(paxHeaderPath, int64(len(record)), tarModeRegular, tarTypePAXLocal)
			if err != nil {
				return nil, fmt.Errorf("member %q PAX header: %w", m.Path, err)
			}
			buf.Write(paxHdr[:])
			buf.WriteString(record)
			writeBlockPadding(&buf, len(record))
			hdr, err := buildTarHeader(paxMemberPlaceholder, int64(len(m.Content)), mode, typ)
			if err != nil {
				return nil, fmt.Errorf("member %q: %w", m.Path, err)
			}
			buf.Write(hdr[:])
		}
		if !m.IsDir {
			buf.Write(m.Content)
			writeBlockPadding(&buf, len(m.Content))
		}
	}
	buf.Write(make([]byte, 2*tarBlockBytes))
	return buf.Bytes(), nil
}

func writeBlockPadding(buf *bytes.Buffer, contentLen int) {
	if pad := contentLen % tarBlockBytes; pad != 0 {
		buf.Write(make([]byte, tarBlockBytes-pad))
	}
}

// .
// .
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

// .
// .
// .
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

// .
// .
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

// .
// .
// .
// .
// .
// .
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

// .
// .
// .
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
