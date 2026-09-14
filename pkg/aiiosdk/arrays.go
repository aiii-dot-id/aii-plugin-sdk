package aiiosdk

// .
// .
// .
// .

import (
	"strconv"
)

// .
func arrayElements(raw []byte) ([][]byte, bool) {
	s := &scanner{b: raw}
	s.ws()
	if s.i >= len(s.b) || s.b[s.i] != '[' {
		return nil, false
	}
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == ']' {
		return [][]byte{}, true
	}
	var out [][]byte
	for {
		s.ws()
		from := s.i
		if !s.value() {
			return nil, false
		}
		out = append(out, s.b[from:s.i])
		s.ws()
		if s.i >= len(s.b) {
			return nil, false
		}
		switch s.b[s.i] {
		case ',':
			s.i++
		case ']':
			return out, true
		default:
			return nil, false
		}
	}
}

// .
// .
func (o Object) StringArray(key string) ([]string, bool) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false
	}
	elems, ok := arrayElements(raw)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		s, ok := decodeJSONString(e)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// .
func (o Object) FloatArray(key string) ([]float32, bool) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false
	}
	return decodeFloatArray(raw)
}

// .
// .
func (o Object) FloatMatrix(key string) ([][]float32, bool) {
	raw := o.Raw(key)
	if raw == nil {
		return nil, false
	}
	rows, ok := arrayElements(raw)
	if !ok {
		return nil, false
	}
	out := make([][]float32, 0, len(rows))
	for _, r := range rows {
		v, ok := decodeFloatArray(r)
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

func decodeFloatArray(raw []byte) ([]float32, bool) {
	elems, ok := arrayElements(raw)
	if !ok {
		return nil, false
	}
	out := make([]float32, 0, len(elems))
	for _, e := range elems {
		f, err := strconv.ParseFloat(string(e), 32)
		if err != nil {
			return nil, false
		}
		out = append(out, float32(f))
	}
	return out, true
}

// .
// .
// .
func appendFloatArray(out []byte, v []float32) []byte {
	out = append(out, '[')
	for i, f := range v {
		if i > 0 {
			out = append(out, ',')
		}
		out = strconv.AppendFloat(out, float64(f), 'g', -1, 32)
	}
	return append(out, ']')
}

// .
// .
func ObjectArray(raw []byte) ([]Object, bool) {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '[' {
		return nil, false
	}
	i++
	var out []Object
	for {
		i = skipSpace(raw, i)
		if i >= len(raw) {
			return nil, false
		}
		if raw[i] == ']' {
			return out, true
		}
		if raw[i] != '{' {
			return nil, false
		}
		start, depth, inString, escaped := i, 0, false, false
		for ; i < len(raw); i++ {
			c := raw[i]
			switch {
			case inString:
				if escaped {
					escaped = false
				} else if c == '\\' {
					escaped = true
				} else if c == '"' {
					inString = false
				}
			case c == '"':
				inString = true
			case c == '{':
				depth++
			case c == '}':
				depth--
			}
			if depth == 0 && !inString {
				i++
				break
			}
		}
		if depth != 0 {
			return nil, false
		}
		out = append(out, Object(raw[start:i]))
		i = skipSpace(raw, i)
		if i < len(raw) && raw[i] == ',' {
			i++
		}
	}
}

func skipSpace(raw []byte, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	return i
}
