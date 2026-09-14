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
// .
// .
// .
// .

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
)

// .
// .
// .
const maxMarshalDepth = 64

var errMarshalUnsupported = errors.New(
	"aiiosdk: unsupported result type — return map[string]any, []any, primitives, json.RawMessage, or a value with MarshalJSON")

var errMarshalDepth = errors.New("aiiosdk: result nesting exceeds 64 levels (cycle?)")

var errMarshalInteger = errors.New(
	"aiiosdk: integer result outside ±(2^53−1), the BBB safe-integer domain — send large exacts as strings")

var errMarshalFloat = errors.New("aiiosdk: non-finite float result cannot travel as JSON")

// .
func marshalValue(v any) ([]byte, error) {
	return appendValue(make([]byte, 0, 64), v, 0)
}

func appendValue(out []byte, v any, depth int) ([]byte, error) {
	if depth > maxMarshalDepth {
		return nil, errMarshalDepth
	}
	switch x := v.(type) {
	case nil:
		return append(out, "null"...), nil
	case json.RawMessage:
		if len(x) == 0 {
			return append(out, "null"...), nil
		}
		return append(out, x...), nil
	case bool:
		if x {
			return append(out, "true"...), nil
		}
		return append(out, "false"...), nil
	case string:
		return appendJSONString(out, x), nil
	case int:
		return appendSafeInt(out, int64(x))
	case int8:
		return appendSafeInt(out, int64(x))
	case int16:
		return appendSafeInt(out, int64(x))
	case int32:
		return appendSafeInt(out, int64(x))
	case int64:
		return appendSafeInt(out, x)
	case uint:
		return appendSafeUint(out, uint64(x))
	case uint8:
		return appendSafeUint(out, uint64(x))
	case uint16:
		return appendSafeUint(out, uint64(x))
	case uint32:
		return appendSafeUint(out, uint64(x))
	case uint64:
		return appendSafeUint(out, x)
	case float32:
		return appendFloat(out, float64(x))
	case float64:
		return appendFloat(out, x)
	case map[string]any:
		return appendMap(out, x, depth, func(v any) any { return v })
	case map[string]string:
		return appendMap(out, x, depth, func(v string) any { return v })
	case map[string]int:
		return appendMap(out, x, depth, func(v int) any { return v })
	case []any:
		return appendSlice(out, x, depth, func(v any) any { return v })
	case []string:
		return appendSlice(out, x, depth, func(v string) any { return v })
	case []int:
		return appendSlice(out, x, depth, func(v int) any { return v })
	case []int64:
		return appendSlice(out, x, depth, func(v int64) any { return v })
	case []float64:
		return appendSlice(out, x, depth, func(v float64) any { return v })
	case []bool:
		return appendSlice(out, x, depth, func(v bool) any { return v })
	default:
		// .
		// .
		// .
		if m, ok := v.(interface{ MarshalJSON() ([]byte, error) }); ok {
			raw, err := m.MarshalJSON()
			if err != nil {
				return nil, err
			}
			return append(out, raw...), nil
		}
		return nil, errMarshalUnsupported
	}
}

// .
// .
// .
// .
func appendSafeInt(out []byte, v int64) ([]byte, error) {
	if v > 9007199254740991 || v < -9007199254740991 {
		return nil, errMarshalInteger
	}
	return strconv.AppendInt(out, v, 10), nil
}

func appendSafeUint(out []byte, v uint64) ([]byte, error) {
	if v > 9007199254740991 {
		return nil, errMarshalInteger
	}
	return strconv.AppendUint(out, v, 10), nil
}

func appendFloat(out []byte, f float64) ([]byte, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, errMarshalFloat
	}
	if f == math.Trunc(f) && (f > 9007199254740991 || f < -9007199254740991) {
		// .
		// .
		return nil, errMarshalInteger
	}
	// .
	// .
	// .
	// .
	// .
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	out = strconv.AppendFloat(out, f, format, -1, 64)
	if format == 'e' {
		if n := len(out); n >= 4 && out[n-4] == 'e' && out[n-3] == '-' && out[n-2] == '0' {
			out[n-2] = out[n-1]
			out = out[:n-1]
		}
	}
	return out, nil
}

// .
// .
func appendMap[V any](out []byte, m map[string]V, depth int, lift func(V) any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out = append(out, '{')
	for i, k := range keys {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendJSONString(out, k)
		out = append(out, ':')
		var err error
		out, err = appendValue(out, lift(m[k]), depth+1)
		if err != nil {
			return nil, err
		}
	}
	return append(out, '}'), nil
}

func appendSlice[V any](out []byte, s []V, depth int, lift func(V) any) ([]byte, error) {
	out = append(out, '[')
	for i, v := range s {
		if i > 0 {
			out = append(out, ',')
		}
		var err error
		out, err = appendValue(out, lift(v), depth+1)
		if err != nil {
			return nil, err
		}
	}
	return append(out, ']'), nil
}
