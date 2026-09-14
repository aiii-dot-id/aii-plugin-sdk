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
// .
// .
// .

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// .
type canonNumber string

// .
// .
func CanonicalizeV1(input []byte) ([]byte, error) {
	if !utf8.Valid(input) {
		return nil, fmt.Errorf("canonical JSON: input is not valid UTF-8")
	}
	if err := validateUnicodeEscapes(input); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	value, err := parseCanonValue(dec)
	if err != nil {
		return nil, err
	}
	if tok, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("canonical JSON: %v", err)
		}
		return nil, fmt.Errorf("canonical JSON: extra token after document: %v", tok)
	}
	var out bytes.Buffer
	if err := emitCanonValue(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// .
// .
func SHA256Prefixed(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func parseCanonValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %v", err)
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			obj := map[string]interface{}{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, fmt.Errorf("canonical JSON: %v", err)
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("canonical JSON: object key is not a string: %v", keyTok)
				}
				if _, dup := obj[key]; dup {
					return nil, fmt.Errorf("canonical JSON: duplicate object key %q", key)
				}
				value, err := parseCanonValue(dec)
				if err != nil {
					return nil, err
				}
				obj[key] = value
			}
			if end, err := dec.Token(); err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("canonical JSON: object not closed")
			}
			return obj, nil
		case '[':
			var arr []interface{}
			for dec.More() {
				value, err := parseCanonValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, value)
			}
			if end, err := dec.Token(); err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("canonical JSON: array not closed")
			}
			return arr, nil
		default:
			return nil, fmt.Errorf("canonical JSON: unexpected delimiter %q", v)
		}
	case string:
		if strings.ContainsRune(v, '\x00') {
			return nil, fmt.Errorf("canonical JSON: NUL code point is outside the canonical profile")
		}
		return v, nil
	case bool:
		return v, nil
	case nil:
		return nil, nil
	case json.Number:
		s := v.String()
		if !canonicalNumberOK(s) {
			return nil, fmt.Errorf("canonical JSON: non-canonical number %q", s)
		}
		return canonNumber(s), nil
	default:
		return nil, fmt.Errorf("canonical JSON: unexpected token %T", tok)
	}
}

// .
// .
// .
func canonicalNumberOK(s string) bool {
	if s == "" || strings.ContainsAny(s, "eE+") {
		return false
	}
	i := 0
	negative := false
	if s[i] == '-' {
		negative = true
		i++
		if i == len(s) {
			return false
		}
	}
	intStart := i
	switch {
	case s[i] == '0':
		i++
		if i < len(s) && s[i] >= '0' && s[i] <= '9' {
			return false
		}
	case s[i] >= '1' && s[i] <= '9':
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	default:
		return false
	}
	intPart := s[intStart:i]
	fractional := false
	if i < len(s) && s[i] == '.' {
		fractional = true
		i++
		fracStart := i
		if i == len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if s[i-1] == '0' && i > fracStart {
			return false
		}
	}
	if i != len(s) {
		return false
	}
	if negative && !fractional && intPart == "0" {
		return false
	}
	return true
}

func hexNibble(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, true
	}
	return 0, false
}

func parseU16(input []byte, offset int) (int, bool) {
	if offset+4 > len(input) {
		return 0, false
	}
	value := 0
	for i := 0; i < 4; i++ {
		n, ok := hexNibble(input[offset+i])
		if !ok {
			return 0, false
		}
		value = value<<4 | n
	}
	return value, true
}

// .
// .
// .
// .
func validateUnicodeEscapes(input []byte) error {
	inString, escaped := false, false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			if c != 'u' {
				continue
			}
			cp, ok := parseU16(input, i+1)
			if !ok {
				return fmt.Errorf("canonical JSON: invalid unicode escape")
			}
			switch {
			case cp >= 0xd800 && cp <= 0xdbff:
				if i+10 >= len(input) || input[i+5] != '\\' || input[i+6] != 'u' {
					return fmt.Errorf("canonical JSON: unpaired high surrogate")
				}
				low, ok := parseU16(input, i+7)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("canonical JSON: unpaired high surrogate")
				}
				i += 10
			case cp >= 0xdc00 && cp <= 0xdfff:
				return fmt.Errorf("canonical JSON: unpaired low surrogate")
			default:
				i += 4
			}
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '"':
			inString = false
		}
	}
	return nil
}

func emitCanonString(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func emitCanonValue(out *bytes.Buffer, value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			emitCanonString(out, key)
			out.WriteByte(':')
			if err := emitCanonValue(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []interface{}:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := emitCanonValue(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case string:
		emitCanonString(out, v)
	case canonNumber:
		out.WriteString(string(v))
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case nil:
		out.WriteString("null")
	default:
		return fmt.Errorf("canonical JSON: unsupported value %T", value)
	}
	return nil
}

// .
// .
// .
// .
func marshalCanonical(v interface{}) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return CanonicalizeV1(raw)
}
