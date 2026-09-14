package aiiosdk

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
	"errors"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// .
// .
// .
// .
var ErrJSONDomain = errors.New("aiiosdk: payload outside the BBB strict JSON domain")

// .
// .
const safeIntegerDigits = "9007199254740991"

// .
// .
// .
func ValidateStrict(payload []byte) error {
	if len(payload) == 0 {
		// .
		// .
		return ErrJSONDomain
	}
	if !utf8.Valid(payload) {
		// .
		// .
		return ErrJSONDomain
	}
	s := &scanner{b: payload}
	s.ws()
	if !s.value() {
		return ErrJSONDomain
	}
	s.ws()
	if s.i != len(s.b) {
		// .
		return ErrJSONDomain
	}
	return nil
}

// .
type scanner struct {
	b []byte
	i int
}

func (s *scanner) ws() {
	for s.i < len(s.b) {
		switch s.b[s.i] {
		case ' ', '\t', '\n', '\r':
			s.i++
		default:
			return
		}
	}
}

func (s *scanner) value() bool {
	if s.i >= len(s.b) {
		return false
	}
	switch c := s.b[s.i]; {
	case c == '{':
		return s.object()
	case c == '[':
		return s.array()
	case c == '"':
		_, ok := s.string_()
		return ok
	case c == '-' || (c >= '0' && c <= '9'):
		return s.number()
	case c == 't':
		return s.literal("true")
	case c == 'f':
		return s.literal("false")
	case c == 'n':
		return s.literal("null")
	default:
		return false
	}
}

func (s *scanner) literal(lit string) bool {
	if len(s.b)-s.i < len(lit) || string(s.b[s.i:s.i+len(lit)]) != lit {
		return false
	}
	s.i += len(lit)
	return true
}

func (s *scanner) object() bool {
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == '}' {
		s.i++
		return true
	}
	// .
	// .
	seen := map[string]bool{}
	for {
		s.ws()
		key, ok := s.string_()
		if !ok {
			return false
		}
		if seen[key] {
			return false
		}
		seen[key] = true
		s.ws()
		if s.i >= len(s.b) || s.b[s.i] != ':' {
			return false
		}
		s.i++
		s.ws()
		if !s.value() {
			return false
		}
		s.ws()
		if s.i >= len(s.b) {
			return false
		}
		switch s.b[s.i] {
		case ',':
			s.i++
		case '}':
			s.i++
			return true
		default:
			return false
		}
	}
}

func (s *scanner) array() bool {
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == ']' {
		s.i++
		return true
	}
	for {
		s.ws()
		if !s.value() {
			return false
		}
		s.ws()
		if s.i >= len(s.b) {
			return false
		}
		switch s.b[s.i] {
		case ',':
			s.i++
		case ']':
			s.i++
			return true
		default:
			return false
		}
	}
}

// .
// .
// .
func (s *scanner) string_() (string, bool) {
	if s.i >= len(s.b) || s.b[s.i] != '"' {
		return "", false
	}
	s.i++
	var out []byte
	for s.i < len(s.b) {
		c := s.b[s.i]
		switch {
		case c == '"':
			s.i++
			return string(out), true
		case c == '\\':
			r, ok := s.escape()
			if !ok {
				return "", false
			}
			out = utf8.AppendRune(out, r)
		case c < 0x20:
			// .
			// .
			return "", false
		default:
			out = append(out, c)
			s.i++
		}
	}
	return "", false
}

// .
func (s *scanner) escape() (rune, bool) {
	if s.i+1 >= len(s.b) {
		return 0, false
	}
	switch s.b[s.i+1] {
	case '"':
		s.i += 2
		return '"', true
	case '\\':
		s.i += 2
		return '\\', true
	case '/':
		s.i += 2
		return '/', true
	case 'b':
		s.i += 2
		return '\b', true
	case 'f':
		s.i += 2
		return '\f', true
	case 'n':
		s.i += 2
		return '\n', true
	case 'r':
		s.i += 2
		return '\r', true
	case 't':
		s.i += 2
		return '\t', true
	case 'u':
		u, ok := s.hex4(s.i + 2)
		if !ok {
			return 0, false
		}
		if u == 0 {
			// .
			return 0, false
		}
		switch {
		case utf16.IsSurrogate(rune(u)) && u >= 0xDC00:
			// .
			return 0, false
		case utf16.IsSurrogate(rune(u)):
			// .
			// .
			if s.i+11 >= len(s.b) || s.b[s.i+6] != '\\' || s.b[s.i+7] != 'u' {
				return 0, false
			}
			lo, ok := s.hex4(s.i + 8)
			if !ok || lo < 0xDC00 || lo > 0xDFFF {
				return 0, false
			}
			r := utf16.DecodeRune(rune(u), rune(lo))
			s.i += 12
			return r, true
		default:
			s.i += 6
			return rune(u), true
		}
	default:
		// .
		return 0, false
	}
}

func (s *scanner) hex4(at int) (uint16, bool) {
	if at+4 > len(s.b) {
		return 0, false
	}
	var v uint16
	for _, c := range s.b[at : at+4] {
		var n byte
		switch {
		case c >= '0' && c <= '9':
			n = c - '0'
		case c >= 'a' && c <= 'f':
			n = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			n = c - 'A' + 10
		default:
			return 0, false
		}
		v = v*16 + uint16(n)
	}
	return v, true
}

// .
// .
func (s *scanner) number() bool {
	start := s.i
	if s.i < len(s.b) && s.b[s.i] == '-' {
		s.i++
	}
	// .
	// .
	switch {
	case s.i < len(s.b) && s.b[s.i] == '0':
		s.i++
		if s.i < len(s.b) && s.b[s.i] >= '0' && s.b[s.i] <= '9' {
			return false
		}
	case s.i < len(s.b) && s.b[s.i] >= '1' && s.b[s.i] <= '9':
		for s.i < len(s.b) && s.b[s.i] >= '0' && s.b[s.i] <= '9' {
			s.i++
		}
	default:
		return false
	}
	if s.i < len(s.b) && s.b[s.i] == '.' {
		s.i++
		if s.i >= len(s.b) || s.b[s.i] < '0' || s.b[s.i] > '9' {
			return false
		}
		for s.i < len(s.b) && s.b[s.i] >= '0' && s.b[s.i] <= '9' {
			s.i++
		}
	}
	if s.i < len(s.b) && (s.b[s.i] == 'e' || s.b[s.i] == 'E') {
		s.i++
		if s.i < len(s.b) && (s.b[s.i] == '+' || s.b[s.i] == '-') {
			s.i++
		}
		if s.i >= len(s.b) || s.b[s.i] < '0' || s.b[s.i] > '9' {
			return false
		}
		for s.i < len(s.b) && s.b[s.i] >= '0' && s.b[s.i] <= '9' {
			s.i++
		}
	}
	return numberDomainOK(string(s.b[start:s.i]))
}

// .
// .
// .
// .
// .
// .
func numberDomainOK(tok string) bool {
	f, err := strconv.ParseFloat(tok, 64)
	if math.IsInf(f, 0) || math.IsNaN(f) {
		// .
		// .
		return false
	}
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return false
		}
		// .
		// .
		// .
		// .
	}
	intDigits, fracDigits, exp, ok := splitNumber(tok)
	if !ok {
		return false
	}
	digits, pointExp := normalizeDecimal(intDigits+fracDigits, exp-len(fracDigits))
	if digits == "" {
		return true
	}
	if pointExp < 0 {
		return true
	}
	// .
	// .
	if len(digits)+pointExp > len(safeIntegerDigits) {
		return false
	}
	mag := digits
	for i := 0; i < pointExp; i++ {
		mag += "0"
	}
	if len(mag) < len(safeIntegerDigits) {
		return true
	}
	return mag <= safeIntegerDigits
}

// .
// .
// .
func splitNumber(tok string) (intDigits, fracDigits string, exp int, ok bool) {
	i := 0
	if i < len(tok) && tok[i] == '-' {
		i++
	}
	j := i
	for j < len(tok) && tok[j] >= '0' && tok[j] <= '9' {
		j++
	}
	intDigits = tok[i:j]
	i = j
	if i < len(tok) && tok[i] == '.' {
		i++
		j = i
		for j < len(tok) && tok[j] >= '0' && tok[j] <= '9' {
			j++
		}
		fracDigits = tok[i:j]
		i = j
	}
	if i < len(tok) && (tok[i] == 'e' || tok[i] == 'E') {
		i++
		e, err := strconv.Atoi(tok[i:])
		if err != nil {
			return "", "", 0, false
		}
		exp = e
		i = len(tok)
	}
	return intDigits, fracDigits, exp, i == len(tok)
}

// .
// .
// .
func normalizeDecimal(digits string, pointExp int) (string, int) {
	start := 0
	for start < len(digits) && digits[start] == '0' {
		start++
	}
	digits = digits[start:]
	end := len(digits)
	for end > 0 && digits[end-1] == '0' {
		end--
		pointExp++
	}
	return digits[:end], pointExp
}

// .

// .
// .
// .
// .
type member struct {
	key string
	raw []byte
}

// .
// .
// .
func objectMembers(payload []byte) ([]member, bool) {
	s := &scanner{b: payload}
	s.ws()
	if s.i >= len(s.b) || s.b[s.i] != '{' {
		return nil, false
	}
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == '}' {
		return nil, true
	}
	var out []member
	for {
		s.ws()
		key, ok := s.string_()
		if !ok {
			return nil, false
		}
		s.ws()
		if s.i >= len(s.b) || s.b[s.i] != ':' {
			return nil, false
		}
		s.i++
		s.ws()
		from := s.i
		if !s.value() {
			return nil, false
		}
		out = append(out, member{key: key, raw: s.b[from:s.i]})
		s.ws()
		if s.i >= len(s.b) {
			return nil, false
		}
		switch s.b[s.i] {
		case ',':
			s.i++
		case '}':
			return out, true
		default:
			return nil, false
		}
	}
}

// .
func memberByKey(members []member, key string) []byte {
	for _, m := range members {
		if m.key == key {
			return m.raw
		}
	}
	return nil
}

// .
// .
func decodeJSONString(raw []byte) (string, bool) {
	s := &scanner{b: raw}
	str, ok := s.string_()
	if !ok || s.i != len(raw) {
		return "", false
	}
	return str, true
}
