package aiiospkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const ValidationFile = "validation.json"

const (
	LaneOperation  = "operation"
	LaneEmbeddings = "embeddings"
)

const (
	AllSets = "*"

	MaxChecks = 32

	MaxCheckNameChars = 120

	MaxCheckWithin = 10 * time.Minute
)

type Validation struct {
	Checks []Check
}

type Check struct {
	Name       string
	Sets       []string
	Lane       string
	Operation  string
	Arguments  map[string]interface{}
	Kind       string
	Text       string
	Audio      string
	WAV        []byte
	Recording  time.Duration
	Expect     Expect
	Within     time.Duration
	Unresolved string
}

func (c Check) For(variant string) bool {
	for _, s := range c.Sets {
		if s == AllSets || s == variant {
			return true
		}
	}
	return false
}

type Expect struct {
	Contains []string
	JSON     *JSONExpect
	Cosine   *CosineExpect
	Events   *EventsExpect
}

type JSONExpect struct {
	Path    string
	Equals  interface{}
	AtLeast *float64
}

func (j *JSONExpect) HasEquals() bool { return j.AtLeast == nil }

type CosineExpect struct {
	Reference []float64
	Min       float64
}

type ValidationError struct {
	Check  int
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Check == 0 {
		return ValidationFile + ": " + e.Detail
	}
	return fmt.Sprintf("%s: check %d: %s", ValidationFile, e.Check, e.Detail)
}

type MemberReader func(rel string) (raw []byte, present bool, err error)

var (
	checkMembers  = map[string]bool{"name": true, "sets": true, "lane": true, "input": true, "call": true, "expect": true, "within_ms": true}
	callMembers   = map[string]bool{"operation": true, "arguments": true}
	expectMembers = map[string]bool{"result_contains": true, "json": true, "vector_cosine_at_least": true, "events": true}
	jsonMembers   = map[string]bool{"path": true, "equals": true, "at_least": true}
	cosineMembers = map[string]bool{"reference": true, "min": true}
)

func ParseValidation(raw []byte, member MemberReader) (*Validation, error) {
	doc, err := decodeStrict(raw)
	if err != nil {
		return nil, &ValidationError{Detail: `not an object whose one member is "checks", a list of checks: ` + err.Error()}
	}
	top, ok := doc.(map[string]interface{})
	if !ok || !onlyMembers(top, map[string]bool{"checks": true}) {
		return nil, &ValidationError{Detail: `not an object whose one member is "checks", a list of checks`}
	}
	list, _ := top["checks"].([]interface{})
	if len(list) < 1 || len(list) > MaxChecks {
		return nil, &ValidationError{Detail: fmt.Sprintf("checks must hold 1..%d entries", MaxChecks)}
	}
	r := &resolver{member: member, docs: map[string]interface{}{}, bad: map[string]string{}, raws: map[string]memberBytes{}, recordings: map[string]int{}}
	out := &Validation{}
	names := map[string]bool{}
	for i, item := range list {
		c, err := r.check(i+1, item)
		if err != nil {
			return nil, err
		}
		if names[c.Name] {
			return nil, &ValidationError{Check: i + 1, Detail: fmt.Sprintf("the name %q is another check's", c.Name)}
		}
		names[c.Name] = true
		out.Checks = append(out.Checks, c)
	}
	total := 0
	for _, n := range r.recordings {
		total += n
	}
	if total > MaxCheckAudioBytes {
		return nil, &ValidationError{Detail: fmt.Sprintf("the session checks' recordings come to %d bytes; a package carries at most %d", total, MaxCheckAudioBytes)}
	}
	return out, nil
}

func onlyMembers(m map[string]interface{}, known map[string]bool) bool {
	for k := range m {
		if !known[k] {
			return false
		}
	}
	return true
}

func object(m map[string]interface{}, key string, known map[string]bool) (map[string]interface{}, bool, error) {
	v, present := m[key]
	if !present || v == nil {
		return nil, false, nil
	}
	o, ok := v.(map[string]interface{})
	if !ok || !onlyMembers(o, known) {
		return nil, false, fmt.Errorf("%s is not an object of the members the grammar knows", key)
	}
	return o, true, nil
}

func raw(m map[string]interface{}, key string) (interface{}, bool) {
	v, present := m[key]
	return v, present
}

type unresolved struct{ why string }

func (u *unresolved) Error() string { return u.why }

var errRefForm = errors.New(`holds a "$ref" that is not an object of one member, a string`)

type resolver struct {
	member     MemberReader
	docs       map[string]interface{}
	bad        map[string]string
	raws       map[string]memberBytes
	recordings map[string]int
}

type memberBytes struct {
	raw     []byte
	present bool
}

func (r *resolver) read(rel string) ([]byte, bool, error) {
	if m, ok := r.raws[rel]; ok {
		return m.raw, m.present, nil
	}
	raw, present, err := r.member(rel)
	if err != nil {
		return nil, false, err
	}
	r.raws[rel] = memberBytes{raw: raw, present: present}
	return raw, present, nil
}

func (r *resolver) recording(c *Check) (string, error) {
	rel := c.Audio
	switch {
	case rel == ValidationFile:
		return fmt.Sprintf("%q is validation.json itself; a recording is another member of the package", rel), nil
	case !insideRoot(rel):
		return fmt.Sprintf("%q leaves the install-root", rel), nil
	}
	raw, present, err := r.read(rel)
	if err != nil {
		return "", err
	}
	if !present {
		return fmt.Sprintf("names %s, which the package does not carry", rel), nil
	}
	r.recordings[rel] = len(raw)
	rec, why := ReadRecording(raw)
	if why != "" {
		return rel + " " + why, nil
	}
	c.WAV, c.Recording = raw, rec.Length
	return "", nil
}

func (r *resolver) check(n int, item interface{}) (Check, error) {
	refuse := func(format string, args ...interface{}) (Check, error) {
		return Check{}, &ValidationError{Check: n, Detail: fmt.Sprintf(format, args...)}
	}
	m, ok := item.(map[string]interface{})
	if !ok {
		return refuse("not an object of the members a check has")
	}

	if l, isString := m["lane"].(string); isString && KnownLane(l) && !onlyMembers(m, checkMembers) {
		return refuse("not an object of the members a check has (name, sets, lane, input, call, expect, within_ms)")
	}
	var c Check
	name, present := m["name"]
	if !present || name == nil {
		return refuse("name is missing")
	}
	s, isString := name.(string)
	if !isString {
		return refuse("name must be a string")
	}
	if !validCheckName(s) {
		return refuse("name must be 1..%d characters of text, not white space only", MaxCheckNameChars)
	}
	c.Name = s
	var sets []string
	if v, present := m["sets"]; present && v != nil {
		items, isList := v.([]interface{})
		if !isList {
			return refuse("sets must be a list of strings")
		}
		for _, it := range items {
			s, isString := it.(string)
			if !isString {
				return refuse("sets must be a list of strings")
			}
			sets = append(sets, s)
		}
	}
	if len(sets) == 0 {
		return refuse("sets must name %q or the component sets the check is for", AllSets)
	}
	seen := map[string]bool{}
	for _, s := range sets {
		switch {
		case s == AllSets && len(sets) != 1:
			return refuse("sets: %q stands alone", AllSets)
		case s != AllSets && !reVariantID.MatchString(s):
			return refuse("sets: %q is not a component set's id", s)
		case seen[s]:
			return refuse("sets names %q twice", s)
		}
		seen[s] = true
	}
	c.Sets = sets
	lane, present := m["lane"]
	if !present || lane == nil {
		return refuse("lane is missing")
	}
	if c.Lane, isString = lane.(string); !isString {
		return refuse("lane must be a string")
	}
	if !KnownLane(c.Lane) {
		within, hasWithin := raw(m, "within_ms")
		if !hasWithin {
			return refuse("within_ms is missing: every check has its own bound")
		}
		got, viaRef, err := r.walk(copyValue(within))
		var u *unresolved
		switch {
		case errors.As(err, &u):
			c.Unresolved = "within_ms: " + u.why
			return c, nil
		case errors.Is(err, errRefForm):
			return refuse("within_ms %v", err)
		case err != nil:
			return Check{}, err
		}
		ms, isInt := checkMilliseconds(got)
		if !isInt || ms < 1 || ms > MaxCheckWithin.Milliseconds() {
			if viaRef {
				c.Unresolved = fmt.Sprintf("within_ms: the value its $ref brings must be a whole number of milliseconds, 1..%d", MaxCheckWithin.Milliseconds())
				return c, nil
			}
			return refuse("within_ms must be a whole number of milliseconds, 1..%d", MaxCheckWithin.Milliseconds())
		}
		c.Within = time.Duration(ms) * time.Millisecond
		return c, nil
	}
	expect, hasExpect, err := object(m, "expect", expectMembers)
	if err != nil {
		return refuse("%v", err)
	}
	if !hasExpect {
		return refuse("expect is missing")
	}
	contains, hasContains := raw(expect, "result_contains")
	jsonExp, hasJSON, err := object(expect, "json", jsonMembers)
	if err != nil {
		return refuse("expect.%v", err)
	}
	cosine, hasCosine, err := object(expect, "vector_cosine_at_least", cosineMembers)
	if err != nil {
		return refuse("expect.%v", err)
	}
	events, hasEvents := raw(expect, "events")
	forms := 0
	for _, set := range []bool{hasContains, hasJSON, hasCosine, hasEvents} {
		if set {
			forms++
		}
	}
	if forms != 1 {
		return refuse("expect holds exactly one of result_contains, json, vector_cosine_at_least, events; it holds %d", forms)
	}
	input, hasInput := raw(m, "input")
	call, hasCall, err := object(m, "call", callMembers)
	if err != nil {
		return refuse("%v", err)
	}
	switch c.Lane {
	case LaneOperation:
		op, _ := call["operation"].(string)
		if v, present := call["operation"]; present && v != nil {
			if _, isString := v.(string); !isString {
				return refuse("call.operation must be a string")
			}
		}
		switch {
		case !hasCall:
			return refuse("a check on the operation lane carries call: the operation and its arguments")
		case hasInput:
			return refuse("a check on the operation lane carries call, not input")
		case hasCosine || hasEvents:
			return refuse("a check on the operation lane expects result_contains or json")
		case op == "" || len(op) > 128 || strings.IndexFunc(op, unicode.IsControl) >= 0:
			return refuse("call.operation must name an operation")
		}
		c.Operation = op
	case LaneEmbeddings:
		switch {
		case !hasInput:
			return refuse("a check on the embeddings lane carries input: a kind and a text")
		case hasCall:
			return refuse("a check on the embeddings lane carries input, not call")
		case !hasCosine:
			return refuse("a check on the embeddings lane expects vector_cosine_at_least")
		}
	case LaneSession:
		switch {
		case !hasInput:
			return refuse("a check on the session lane carries input: a recording the package carries, or a text")
		case hasCall:
			return refuse("a check on the session lane carries input, not call")
		case !hasEvents:
			return refuse("a check on the session lane expects events")
		}
	}
	within, hasWithin := raw(m, "within_ms")
	if !hasWithin {
		return refuse("within_ms is missing: every check has its own bound")
	}
	jsonPath := ""
	if hasJSON {
		if v, present := jsonExp["path"]; present && v != nil {
			p, isString := v.(string)
			if !isString {
				return refuse("expect.json.path must be a string")
			}
			jsonPath = p
		}
		if !validPointer(jsonPath) {
			return refuse(`expect.json.path must be a JSON pointer ("" or "/…")`)
		}
		_, hasEquals := raw(jsonExp, "equals")
		_, hasAtLeast := raw(jsonExp, "at_least")
		if hasEquals == hasAtLeast {
			return refuse("expect.json holds exactly one of equals and at_least")
		}
	}

	take := func(v interface{}, what string, hold func(interface{}) error) error {
		got, viaRef, err := r.walk(copyValue(v))
		var u *unresolved
		switch {
		case errors.As(err, &u):
			if c.Unresolved == "" {
				c.Unresolved = what + ": " + u.why
			}
			return nil
		case errors.Is(err, errRefForm):
			return &ValidationError{Check: n, Detail: fmt.Sprintf("%s %v", what, err)}
		case err != nil:
			return err
		}
		if herr := hold(got); herr != nil {
			if !viaRef {
				return &ValidationError{Check: n, Detail: fmt.Sprintf("%s %v", what, herr)}
			}
			if c.Unresolved == "" {
				c.Unresolved = fmt.Sprintf("%s: the value its $ref brings %v", what, herr)
			}
		}
		return nil
	}
	steps := []func() error{func() error {
		return take(within, "within_ms", func(v interface{}) error {
			ms, isInt := checkMilliseconds(v)
			if !isInt || ms < 1 || ms > MaxCheckWithin.Milliseconds() {
				return fmt.Errorf("must be a whole number of milliseconds, 1..%d", MaxCheckWithin.Milliseconds())
			}
			c.Within = time.Duration(ms) * time.Millisecond
			return nil
		})
	}}
	switch c.Lane {
	case LaneOperation:
		args, present := call["arguments"]
		if !present {
			args = map[string]interface{}{}
		}
		steps = append(steps, func() error {
			return take(args, "call.arguments", func(v interface{}) error {
				o, isObj := v.(map[string]interface{})
				if !isObj {
					return errors.New("must be an object")
				}
				c.Arguments = o
				return nil
			})
		})
	case LaneEmbeddings:
		steps = append(steps, func() error {
			return take(input, "input", func(v interface{}) error {
				o, isObj := v.(map[string]interface{})
				kind, k := o["kind"].(string)
				text, t := o["text"].(string)
				if !isObj || len(o) != 2 || !k || !t || kind == "" || text == "" {
					return errors.New(`must be an object of two members, "kind" and "text", each a string`)
				}
				c.Kind, c.Text = kind, text
				return nil
			})
		})
	case LaneSession:
		steps = append(steps, func() error {
			return take(input, "input", func(v interface{}) error {
				audioPath, text, err := sessionInput(v)
				if err == nil {
					c.Audio, c.Text = audioPath, text
				}
				return err
			})
		}, func() error {
			if c.Audio == "" || c.Unresolved != "" {
				return nil
			}
			why, err := r.recording(&c)
			if why != "" && c.Unresolved == "" {
				c.Unresolved = "input.audio: " + why
			}
			return err
		})
	}
	switch {
	case hasContains:
		steps = append(steps, func() error {
			return take(contains, "expect.result_contains", func(v interface{}) error {
				words, err := containsList(v)
				c.Expect.Contains = words
				return err
			})
		})
	case hasEvents:
		steps = append(steps, func() error {
			return take(events, "expect.events", func(v interface{}) error {
				ev, err := sessionEvents(v, c.Audio != "", c.Text != "")
				c.Expect.Events = ev
				return err
			})
		}, func() error {
			if c.Unresolved == "" && c.Recording > c.Within {
				c.Unresolved = fmt.Sprintf("within_ms: %d ms is shorter than its recording (%d ms), which is sent in real time: it can never pass", c.Within.Milliseconds(), c.Recording.Milliseconds())
			}
			return nil
		})
	case hasJSON:
		j := &JSONExpect{Path: jsonPath}
		c.Expect.JSON = j
		steps = append(steps, func() error {
			if eq, present := raw(jsonExp, "equals"); present {
				return take(eq, "expect.json.equals", func(v interface{}) error {
					j.Equals = v
					return nil
				})
			}
			least, _ := raw(jsonExp, "at_least")
			return take(least, "expect.json.at_least", func(v interface{}) error {
				f, isNum := v.(float64)
				if !isNum {
					return errors.New("must be a number")
				}
				j.AtLeast = &f
				return nil
			})
		})
	default:
		cos := &CosineExpect{}
		c.Expect.Cosine = cos
		ref, hasRef := raw(cosine, "reference")
		min, hasMin := raw(cosine, "min")
		if !hasRef || !hasMin {
			return refuse("expect.vector_cosine_at_least holds reference and min")
		}
		steps = append(steps, func() error {
			return take(ref, "expect.vector_cosine_at_least.reference", func(v interface{}) error {
				vec, err := referenceVector(v)
				cos.Reference = vec
				return err
			})
		}, func() error {
			return take(min, "expect.vector_cosine_at_least.min", func(v interface{}) error {
				f, isNum := v.(float64)
				if !isNum || f < -1 || f > 1 {
					return errors.New("must be a number from -1 to 1")
				}
				cos.Min = f
				return nil
			})
		})
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return Check{}, err
		}
	}
	return c, nil
}

func (r *resolver) walk(v interface{}) (interface{}, bool, error) {
	switch t := v.(type) {
	case map[string]interface{}:
		if ref, has := t["$ref"]; has {
			s, isString := ref.(string)
			if len(t) != 1 || !isString {
				return nil, false, errRefForm
			}
			got, err := r.ref(s)
			return got, true, err
		}
		via := false
		for k, item := range t {
			got, ref, err := r.walk(item)
			if err != nil {
				return nil, false, err
			}
			t[k], via = got, via || ref
		}
		return t, via, nil
	case []interface{}:
		via := false
		for i, item := range t {
			got, ref, err := r.walk(item)
			if err != nil {
				return nil, false, err
			}
			t[i], via = got, via || ref
		}
		return t, via, nil
	}
	return v, false, nil
}

func (r *resolver) ref(s string) (interface{}, error) {
	rel, pointer, hasFragment := strings.Cut(s, "#")
	if !hasFragment {
		pointer = ""
	}
	switch {
	case rel == "" || rel == ValidationFile:
		return nil, &unresolved{why: fmt.Sprintf("$ref %q names validation.json itself; a $ref names another member of the package", s)}
	case !insideRoot(rel):
		return nil, &unresolved{why: fmt.Sprintf("$ref %q leaves the install-root", s)}
	case !validPointer(pointer):
		return nil, &unresolved{why: fmt.Sprintf("$ref %q: %q is not a JSON pointer", s, pointer)}
	}
	doc, err := r.doc(rel)
	if err != nil {
		return nil, err
	}
	got, found := Pointer(doc, pointer)
	if !found {
		return nil, &unresolved{why: fmt.Sprintf("$ref %q points at nothing in %s", s, rel)}
	}
	return copyValue(got), nil
}

func (r *resolver) doc(rel string) (interface{}, error) {
	if d, ok := r.docs[rel]; ok {
		return d, nil
	}
	if why, ok := r.bad[rel]; ok {
		return nil, &unresolved{why: why}
	}
	b, present, err := r.read(rel)
	if err != nil {
		return nil, err
	}
	var why string
	var d interface{}
	if !present {
		why = fmt.Sprintf("$ref names %s, which the package does not carry", rel)
	} else if d, err = decodeStrict(b); err != nil {
		why = fmt.Sprintf("$ref names %s, which is not JSON", rel)
	}
	if why != "" {
		r.bad[rel] = why
		return nil, &unresolved{why: why}
	}
	r.docs[rel] = d
	return d, nil
}

func insideRoot(rel string) bool {
	return !strings.HasPrefix(rel, "/") && !strings.Contains(rel, `\`) && !strings.Contains(rel, ":") &&
		path.Clean(rel) == rel && rel != ".." && !strings.HasPrefix(rel, "../")
}

func validPointer(p string) bool {
	if p == "" {
		return true
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' && (i+1 >= len(p) || (p[i+1] != '0' && p[i+1] != '1')) {
			return false
		}
	}
	return true
}

func Pointer(doc interface{}, p string) (interface{}, bool) {
	if !validPointer(p) {
		return nil, false
	}
	if p == "" {
		return doc, true
	}
	cur := doc
	for _, token := range strings.Split(p[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch t := cur.(type) {
		case map[string]interface{}:
			next, ok := t[token]
			if !ok {
				return nil, false
			}
			cur = next
		case []interface{}:
			if token == "" || (len(token) > 1 && token[0] == '0') {
				return nil, false
			}
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(t) || strconv.Itoa(i) != token {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func JSONEqual(a, b interface{}) bool {
	ra, err1 := json.Marshal(a)
	rb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ra, rb)
}

func FloatCosine(got []float32, ref []float64) float64 {
	if len(got) != len(ref) || len(got) == 0 {
		return 0
	}
	var dot, ng, nr float64
	for i := range got {
		g := float64(got[i])
		dot += float64(g * ref[i])
		ng += float64(g * g)
		nr += float64(ref[i] * ref[i])
	}
	if ng == 0 || nr == 0 {
		return 0
	}
	return dot / (math.Sqrt(ng) * math.Sqrt(nr))
}

func decodeStrict(raw []byte) (interface{}, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("not valid UTF-8")
	}
	if err := validateUnicodeEscapes(raw); err != nil {
		return nil, errors.New("a string holds an escape that names no character")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("content after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := map[string]interface{}{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				if _, dup := out[key]; dup {
					return nil, fmt.Errorf("an object names %q twice", key)
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				out[key] = v
			}
			_, err := dec.Token()
			return out, err
		case '[':
			out := []interface{}{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			_, err := dec.Token()
			return out, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return nil, fmt.Errorf("the number %s is not one a float holds", t)
		}
		return f, nil
	}
	return tok, nil
}

func copyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, item := range t {
			out[k] = copyValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = copyValue(item)
		}
		return out
	}
	return v
}

func validCheckName(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > MaxCheckNameChars {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

func checkMilliseconds(v interface{}) (int64, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}

func containsList(v interface{}) ([]string, error) {
	const want = "must be a string, or a list of strings, none empty"
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil, errors.New(want)
		}
		return []string{t}, nil
	case []interface{}:
		if len(t) == 0 {
			return nil, errors.New(want)
		}
		out := make([]string, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok || s == "" {
				return nil, errors.New(want)
			}
			out[i] = s
		}
		return out, nil
	}
	return nil, errors.New(want)
}

func referenceVector(v interface{}) ([]float64, error) {
	items, ok := v.([]interface{})
	if !ok || len(items) == 0 {
		return nil, errors.New("must be a list of numbers")
	}
	out := make([]float64, len(items))
	var norm float64
	for i, item := range items {
		f, isNum := item.(float64)
		if !isNum {
			return nil, fmt.Errorf("must be a list of numbers; entry %d is not one", i+1)
		}
		out[i] = f
		norm += f * f
	}
	if norm == 0 || math.IsInf(norm, 0) {
		return nil, errors.New("must be a vector a cosine can be taken against: not all zeros, and its length a number")
	}
	return out, nil
}

const SessionInterfaceID = "speech.session"

type PackageChecks struct {
	Variants   []string
	Source     *EmbeddingsDecl
	Operations map[string]OperationContract

	SpeechEngine bool
	Native       map[string]bool
}

type OperationContract struct {
	Effects          string
	OperatorConfirms bool
}

func ValidatePackageChecks(v *Validation, p PackageChecks) error {
	declared := map[string]bool{}
	for _, id := range p.Variants {
		declared[id] = true
	}
	for _, c := range v.Checks {
		named := fmt.Sprintf("%s: check %q", ValidationFile, c.Name)
		if !KnownLane(c.Lane) {

			return fmt.Errorf("%s is sent on lane %q, which no host runs checks on (%s, %s, %s)", named, c.Lane, LaneOperation, LaneEmbeddings, LaneSession)
		}
		if c.Unresolved != "" {
			return fmt.Errorf("%s can never pass: %s", named, c.Unresolved)
		}
		for _, s := range c.Sets {
			if s != AllSets && !declared[s] {
				return fmt.Errorf("%s names component set %q, which the package does not declare", named, s)
			}
		}
		switch c.Lane {
		case LaneEmbeddings:
			switch {
			case p.Source == nil:
				return fmt.Errorf("%s is sent on the embeddings lane, and the package is not a source of vectors", named)
			case c.Kind != EmbedKindMemory && c.Kind != EmbedKindCue:
				return fmt.Errorf("%s can never pass: its input's kind %q is neither %s nor %s", named, c.Kind, EmbedKindMemory, EmbedKindCue)
			case utf8.RuneCountInString(c.Text) > MaxEmbedChars || strings.TrimSpace(c.Text) == "":
				return fmt.Errorf("%s can never pass: its text must be 1..%d characters, not white space only", named, MaxEmbedChars)
			case len(c.Expect.Cosine.Reference) != p.Source.Dimension:
				return fmt.Errorf("%s can never pass: its reference holds %d numbers; the declared dimension is %d", named, len(c.Expect.Cosine.Reference), p.Source.Dimension)
			}
		case LaneSession:
			if !p.SpeechEngine {
				return fmt.Errorf("%s is sent on the session lane, and the package is no resident speech engine (plugin_family voice_interface)", named)
			}
			sets := c.Sets
			if len(sets) == 1 && sets[0] == AllSets {
				sets = p.Variants
			}
			for _, s := range sets {
				if !p.Native[s] {
					return fmt.Errorf("%s names set %q, which is not a native component set: a resident session runs on one alone", named, s)
				}
			}
		case LaneOperation:
			op, ok := p.Operations[c.Operation]
			switch {
			case !ok:
				return fmt.Errorf("%s calls operation %q, which the package does not declare", named, c.Operation)
			case op.Effects != "read.internal" && op.Effects != "read.external":
				effects := op.Effects
				if effects == "" {
					effects = "none"
				}
				return fmt.Errorf("%s calls operation %q, whose declared effects are %s: a check calls only an operation that reads", named, c.Operation, effects)
			case op.OperatorConfirms:
				return fmt.Errorf("%s calls operation %q, which waits for the operator's confirmation: a check has no one to confirm it", named, c.Operation)
			}
		}
	}
	return nil
}
