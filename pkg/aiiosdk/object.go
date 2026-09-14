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

import (
	"encoding/json"
	"strconv"
)

// .
// .
type Object []byte

// .
func (o Object) Raw(key string) json.RawMessage {
	if len(o) == 0 {
		return nil
	}
	members, ok := objectMembers(o)
	if !ok {
		return nil
	}
	return memberByKey(members, key)
}

// .
func (o Object) Has(key string) bool { return o.Raw(key) != nil }

// .
func (o Object) String(key string) (string, bool) {
	return decodeJSONString(o.Raw(key))
}

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
func (o Object) Int(key string) (int64, bool) {
	raw := o.Raw(key)
	if raw == nil {
		return 0, false
	}
	// .
	if v, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		return v, true
	}
	// .
	if s, ok := decodeJSONString(raw); ok {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v, true
		}
	}
	// .
	f, ok := o.Float(key)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// .
func (o Object) Float(key string) (float64, bool) {
	raw := o.Raw(key)
	if raw == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// .
func (o Object) Bool(key string) (bool, bool) {
	switch string(o.Raw(key)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// .
func (o Object) Object(key string) Object {
	raw := o.Raw(key)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	return Object(raw)
}
