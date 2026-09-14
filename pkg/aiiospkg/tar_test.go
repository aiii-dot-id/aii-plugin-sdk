package aiiospkg

// .
// .
// .
// .
// .
// .

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"
)

func mustBundle(t *testing.T, tree *Tree) []byte {
	t.Helper()
	b, err := WriteBundle(tree)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	return b
}

// .
func unwrapGzip(t *testing.T, bundle []byte) []byte {
	t.Helper()
	if len(bundle) < 18 {
		t.Fatalf("bundle too short: %d bytes", len(bundle))
	}
	if !bytes.Equal(bundle[:10], gzipHeaderCanonical[:]) {
		t.Fatalf("gzip header not canonical: % x", bundle[:10])
	}
	zr, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("gzip open: %v", err)
	}
	zr.Multistream(false)
	tarBytes, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read (CRC32/ISIZE checked here): %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return tarBytes
}

func walkTar(t *testing.T, tarBytes []byte) []*tar.Header {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	var hdrs []*tar.Header
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar walk: %v", err)
		}
		hdrs = append(hdrs, h)
		if _, err := io.Copy(io.Discard, tr); err != nil {
			t.Fatalf("tar payload: %v", err)
		}
	}
	return hdrs
}

// .
// .
func TestGoldenDirectoryHeader(t *testing.T) {
	tree := NewTree("root")
	tree.Add("a.txt", []byte("hi"))
	bundle := mustBundle(t, tree)
	tarBytes := unwrapGzip(t, bundle)

	var want [512]byte
	copy(want[0:], "root/")
	copy(want[100:], "0000755\x00")
	copy(want[108:], "0000000\x00")
	copy(want[116:], "0000000\x00")
	copy(want[124:], "00000000000\x00")
	copy(want[136:], "00000000000\x00")
	copy(want[148:], "        ")
	want[156] = '5'
	copy(want[257:], "ustar\x00")
	copy(want[263:], "00")
	var sum int64
	for _, b := range want {
		sum += int64(b)
	}
	copy(want[148:], fmt.Sprintf("%06o", sum))
	want[154] = 0
	want[155] = ' '

	if !bytes.Equal(tarBytes[:512], want[:]) {
		t.Fatalf("root directory header deviates from the hand-built canonical form:\n got % x\nwant % x", tarBytes[:512], want[:])
	}
}

func TestMemberOrderAndLayout(t *testing.T) {
	tree := NewTree("root")
	// .
	// .
	tree.Add("a/b", []byte("1"))
	tree.Add("a-c", []byte("2"))
	tree.Add("a.d", []byte("3"))
	bundle := mustBundle(t, tree)
	hdrs := walkTar(t, unwrapGzip(t, bundle))

	var names []string
	for _, h := range hdrs {
		names = append(names, h.Name)
	}
	// .
	// .
	// .
	// .
	want := []string{"root/", "root/a/", "root/a-c", "root/a.d", "root/a/b"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("member order/layout: got %v want %v", names, want)
	}
	for _, h := range hdrs {
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Fatalf("non-canonical ownership fields in %q", h.Name)
		}
		if !h.ModTime.IsZero() && h.ModTime.Unix() != 0 {
			t.Fatalf("member %q mtime %v is not the canonical zero", h.Name, h.ModTime)
		}
		wantMode := int64(0o644)
		if strings.HasSuffix(h.Name, "/") {
			wantMode = 0o755
		}
		if h.Mode != wantMode {
			t.Fatalf("member %q mode %04o, want %04o", h.Name, h.Mode, wantMode)
		}
	}
	// .
	tarBytes := unwrapGzip(t, bundle)
	end := tarBytes[len(tarBytes)-1024:]
	if !bytes.Equal(end, make([]byte, 1024)) {
		t.Fatalf("archive does not end with two zero blocks")
	}
}

func TestUSTARPrefixSplit(t *testing.T) {
	// .
	// .
	deep := strings.Repeat("d", 90)
	leaf := strings.Repeat("f", 60) + ".bin"
	tree := NewTree("root")
	tree.Add(deep+"/"+leaf, []byte("x"))
	tarBytes := unwrapGzip(t, mustBundle(t, tree))

	// .
	for off := 0; off+512 <= len(tarBytes); off += 512 {
		name := string(bytes.TrimRight(tarBytes[off:off+100], "\x00"))
		if name == paxHeaderPath || name == paxMemberPlaceholder {
			t.Fatalf("PAX used for a USTAR-fit path")
		}
		if name == "" {
			break
		}
		// .
		size, err := parseOctalField(tarBytes[off+124 : off+136])
		if err != nil {
			t.Fatalf("size field: %v", err)
		}
		off += int(((size + 511) / 512) * 512)
	}
	hdrs := walkTar(t, tarBytes)
	got := hdrs[len(hdrs)-1].Name
	want := "root/" + deep + "/" + leaf
	if got != want {
		t.Fatalf("split path reassembles to %q, want %q", got, want)
	}
}

func parseOctalField(field []byte) (int64, error) {
	var v int64
	for _, c := range field {
		if c == 0 || c == ' ' {
			continue
		}
		if c < '0' || c > '7' {
			return 0, fmt.Errorf("non-octal byte %q", c)
		}
		v = v*8 + int64(c-'0')
	}
	return v, nil
}

func TestPAXLaneForUnsplittablePath(t *testing.T) {
	// .
	// .
	long := strings.Repeat("x", 150)
	tree := NewTree("root")
	tree.Add(long, []byte("payload"))
	tarBytes := unwrapGzip(t, mustBundle(t, tree))

	// .
	paxHdr := tarBytes[512:1024]
	name := string(bytes.TrimRight(paxHdr[:100], "\x00"))
	if name != paxHeaderPath {
		t.Fatalf("second member is %q, want the PAX header %q", name, paxHeaderPath)
	}
	if paxHdr[156] != 'x' {
		t.Fatalf("PAX header typeflag %q", paxHdr[156])
	}
	// .
	// .
	recLen, err := parseOctalField(paxHdr[124:136])
	if err != nil {
		t.Fatal(err)
	}
	record := string(tarBytes[1024 : 1024+recLen])
	wantRecord := fmt.Sprintf("%d path=root/%s\n", recLen, long)
	if record != wantRecord {
		t.Fatalf("PAX record %q, want %q", record, wantRecord)
	}
	for _, b := range tarBytes[1024+recLen : 1536] {
		if b != 0 {
			t.Fatalf("PAX record padding is not zero")
		}
	}
	// .
	memberHdr := tarBytes[1536:2048]
	if got := string(bytes.TrimRight(memberHdr[:100], "\x00")); got != paxMemberPlaceholder {
		t.Fatalf("PAX member stored as %q, want %q", got, paxMemberPlaceholder)
	}
	// .
	hdrs := walkTar(t, tarBytes)
	if got := hdrs[len(hdrs)-1].Name; got != "root/"+long {
		t.Fatalf("PAX path reassembles to %q", got)
	}
}

func TestWriterRejects(t *testing.T) {
	cases := []struct {
		name  string
		build func() *Tree
		want  string
	}{
		{"casefold_sibling_collision", func() *Tree {
			tr := NewTree("root")
			tr.Add("Readme.md", []byte("a"))
			tr.Add("readme.md", []byte("b"))
			return tr
		}, "casefold"},
		{"space_in_component", func() *Tree {
			tr := NewTree("root")
			tr.Add("bad name.txt", []byte("a"))
			return tr
		}, "[A-Za-z0-9._+-]"},
		{"windows_device_stem", func() *Tree {
			tr := NewTree("root")
			tr.Add("con.txt", []byte("a"))
			return tr
		}, "device"},
		{"trailing_dot", func() *Tree {
			tr := NewTree("root")
			tr.Add("trailing./f", []byte("a"))
			return tr
		}, "dot"},
		{"dotdot_component", func() *Tree {
			tr := NewTree("root")
			tr.Add("../escape", []byte("a"))
			return tr
		}, "forbidden"},
		{"component_too_long", func() *Tree {
			tr := NewTree("root")
			tr.Add(strings.Repeat("c", 256), []byte("a"))
			return tr
		}, "1..255"},
		{"path_too_long", func() *Tree {
			// .
			seg := strings.Repeat("s", 100)
			tr := NewTree("root")
			tr.Add(strings.Join([]string{seg, seg, seg, seg, seg, seg, "f"}, "/"), []byte("a"))
			return tr
		}, "canonical"},
		{"member_ceiling", func() *Tree {
			tr := NewTree("root")
			for i := 0; i < 1030; i++ {
				tr.Add(fmt.Sprintf("f%04d", i), []byte("x"))
			}
			return tr
		}, "1024"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := WriteBundle(tc.build())
			if err == nil {
				t.Fatalf("writer accepted a non-canonical tree")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the rule (%q)", err, tc.want)
			}
		})
	}
}

func TestGzipISIZEAndCRC(t *testing.T) {
	tree := NewTree("root")
	tree.Add("f", bytes.Repeat([]byte("data"), 1000))
	bundle := mustBundle(t, tree)
	tarBytes := unwrapGzip(t, bundle)
	// .
	isize := uint32(bundle[len(bundle)-4]) | uint32(bundle[len(bundle)-3])<<8 |
		uint32(bundle[len(bundle)-2])<<16 | uint32(bundle[len(bundle)-1])<<24
	if int(isize) != len(tarBytes) {
		t.Fatalf("ISIZE %d != decompressed length %d", isize, len(tarBytes))
	}
	// .
	zr, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(zr); err != nil {
		t.Fatal(err)
	}
	if _, err := zr.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("bytes follow the single gzip member: %v", err)
	}
}
