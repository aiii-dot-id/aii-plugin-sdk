package aiiosdk

// .
// .
// .
// .
// .

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type vectorFile struct {
	Suite       string   `json:"suite"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	Vectors     []vector `json:"vectors"`
}

type vector struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	PayloadUTF8   *string  `json:"payload_utf8"`
	PayloadHex    string   `json:"payload_hex"`
	PayloadFill   string   `json:"payload_fill"`
	PayloadLen    int      `json:"payload_len"`
	FrameHex      string   `json:"frame_hex"`
	PayloadsUTF8  []string `json:"payloads_utf8"`
	MaxFrameBytes int      `json:"max_frame_bytes"`
	Error         string   `json:"error"`
}

func loadVectorFile(t *testing.T, name string) vectorFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "vectors", name))
	if err != nil {
		t.Fatalf("read vector file: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatalf("parse vector file %s: %v", name, err)
	}
	if len(vf.Vectors) == 0 {
		t.Fatalf("vector file %s has no vectors", name)
	}
	return vf
}

// .
func (v vector) payloadBytes(t *testing.T) []byte {
	t.Helper()
	switch {
	case v.PayloadUTF8 != nil:
		return []byte(*v.PayloadUTF8)
	case v.PayloadHex != "":
		b, err := hex.DecodeString(v.PayloadHex)
		if err != nil {
			t.Fatalf("%s: bad payload_hex: %v", v.Name, err)
		}
		return b
	case v.PayloadFill != "":
		fill, err := hex.DecodeString(v.PayloadFill)
		if err != nil || len(fill) != 1 {
			t.Fatalf("%s: bad payload_fill", v.Name)
		}
		return bytes.Repeat(fill, v.PayloadLen)
	default:
		t.Fatalf("%s: no payload", v.Name)
		return nil
	}
}

func (v vector) maxFrame() int {
	if v.MaxFrameBytes > 0 {
		return v.MaxFrameBytes
	}
	return MaxControlFrameBytes
}

// .
func mapFramingError(t *testing.T, name, want string, err error) {
	t.Helper()
	switch want {
	case "frame_too_large":
		if !errors.Is(err, ErrFrameTooLarge) {
			t.Fatalf("%s: want ErrFrameTooLarge, got %v", name, err)
		}
	case "empty_payload":
		if !errors.Is(err, ErrEmptyPayload) {
			t.Fatalf("%s: want ErrEmptyPayload, got %v", name, err)
		}
	case "truncated":
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("%s: want io.ErrUnexpectedEOF, got %v", name, err)
		}
	default:
		t.Fatalf("%s: unknown error name %q in vector file", name, want)
	}
}

func TestFramingVectors(t *testing.T) {
	vf := loadVectorFile(t, "framing.json")
	if vf.Suite != "framing" {
		t.Fatalf("unexpected suite %q", vf.Suite)
	}
	for _, v := range vf.Vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			max := v.maxFrame()
			switch v.Kind {
			case "roundtrip":
				payload := v.payloadBytes(t)
				frame, err := hex.DecodeString(v.FrameHex)
				if err != nil {
					t.Fatalf("bad frame_hex: %v", err)
				}
				var buf bytes.Buffer
				if err := WriteFrame(&buf, payload, max); err != nil {
					t.Fatalf("encode: %v", err)
				}
				if !bytes.Equal(buf.Bytes(), frame) {
					t.Fatalf("encode mismatch:\n got %x\nwant %x", buf.Bytes(), frame)
				}
				got, err := ReadFrame(bytes.NewReader(frame), max)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !bytes.Equal(got, payload) {
					t.Fatalf("decode mismatch: got %q", got)
				}
			case "synthetic_roundtrip":
				payload := v.payloadBytes(t)
				var buf bytes.Buffer
				if err := WriteFrame(&buf, payload, max); err != nil {
					t.Fatalf("encode: %v", err)
				}
				got, err := ReadFrame(&buf, max)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !bytes.Equal(got, payload) {
					t.Fatalf("synthetic roundtrip mismatch (len got %d want %d)", len(got), len(payload))
				}
			case "decode":
				frame, err := hex.DecodeString(v.FrameHex)
				if err != nil {
					t.Fatalf("bad frame_hex: %v", err)
				}
				got, err := ReadFrame(bytes.NewReader(frame), max)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !bytes.Equal(got, v.payloadBytes(t)) {
					t.Fatalf("decode mismatch: got %q", got)
				}
			case "decode_sequence":
				frame, err := hex.DecodeString(v.FrameHex)
				if err != nil {
					t.Fatalf("bad frame_hex: %v", err)
				}
				r := bytes.NewReader(frame)
				for i, want := range v.PayloadsUTF8 {
					got, err := ReadFrame(r, max)
					if err != nil {
						t.Fatalf("frame %d: %v", i, err)
					}
					if string(got) != want {
						t.Fatalf("frame %d: got %q want %q", i, got, want)
					}
				}
				if _, err := ReadFrame(r, max); !errors.Is(err, io.EOF) {
					t.Fatalf("stream should end cleanly, got %v", err)
				}
			case "decode_error":
				frame, err := hex.DecodeString(v.FrameHex)
				if err != nil {
					t.Fatalf("bad frame_hex: %v", err)
				}
				_, derr := ReadFrame(bytes.NewReader(frame), max)
				mapFramingError(t, v.Name, v.Error, derr)
			case "encode_error":
				payload := v.payloadBytes(t)
				var buf bytes.Buffer
				werr := WriteFrame(&buf, payload, max)
				mapFramingError(t, v.Name, v.Error, werr)
				if buf.Len() != 0 {
					// .
					// .
					t.Fatalf("encode error leaked %d bytes to the stream", buf.Len())
				}
			default:
				t.Fatalf("unknown framing kind %q", v.Kind)
			}
		})
	}
}

func TestJSONDomainVectors(t *testing.T) {
	vf := loadVectorFile(t, "json_domain.json")
	if vf.Suite != "json_domain" {
		t.Fatalf("unexpected suite %q", vf.Suite)
	}
	for _, v := range vf.Vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			payload := v.payloadBytes(t)
			err := ValidateStrict(payload)
			switch v.Kind {
			case "accept":
				if err != nil {
					t.Fatalf("must accept, got %v", err)
				}
			case "reject":
				if err == nil {
					t.Fatalf("must reject")
				}
			default:
				t.Fatalf("unknown json_domain kind %q", v.Kind)
			}
		})
	}
}
