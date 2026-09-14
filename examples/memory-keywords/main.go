// .
// .
// .
// .
// .
// .
// .
package main

import (
	"sort"
	"strconv"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	prefix = "m:"
	seqKey = "m:_seq"
)

func init() {
	p := sdk.New("com.aiii.examples.memory-keywords")

	p.Describe("memory.remember", sdk.Descriptor{
		Summary:      "Remember one text: store it with its word set under a new id",
		Input:        "schemas/remember_in.json",
		Output:       "schemas/remember_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"ring4.kv"},
	})
	p.Handle("memory.remember", remember)

	p.Describe("memory.recall", sdk.Descriptor{
		Summary:      "Recall the k stored texts sharing the most words with a query",
		Input:        "schemas/recall_in.json",
		Output:       "schemas/recall_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"ring4.kv"},
		Family:       "memory",
		Keywords:     []string{"search", "similar", "remember", "what did I store"},
		Examples:     []string{`{"query": "what did I learn about truth"}`, `{"query": "the ledger", "k": 5}`},
	})
	p.Handle("memory.recall", recall)

	p.Describe("memory.forget", sdk.Descriptor{
		Summary:      "Forget one stored text by id",
		Input:        "schemas/forget_in.json",
		Output:       "schemas/forget_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"ring4.kv"},
	})
	p.Handle("memory.forget", forget)

	p.Run()
}

func remember(c sdk.Call) (any, error) {
	text, ok := c.Args().String("text")
	if !ok || text == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "memory.remember requires arguments.text (string)")
	}
	seq, err := nextSeq()
	if err != nil {
		return denial(err)
	}
	key := prefix + strconv.Itoa(seq)
	res, err := sdk.KV.Put(key, string(record(text, words(text))))
	if err != nil {
		return denial(err)
	}
	if _, err := sdk.KV.Put(seqKey, strconv.Itoa(seq)); err != nil {
		return denial(err)
	}
	return map[string]any{"id": key, "words": len(words(text)), "scope": res.Scope}, nil
}

func recall(c sdk.Call) (any, error) {
	query, ok := c.Args().String("query")
	if !ok || query == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "memory.recall requires arguments.query (string)")
	}
	// .
	// .
	k := 3
	if vals, err := sdk.Settings.Load(); err == nil {
		if n, ok := vals.Int("recall_limit"); ok && n >= 1 && n <= 20 {
			k = int(n)
		}
	}
	if n, ok := c.Args().Int("k"); ok && n >= 1 && n <= 20 {
		k = int(n)
	}
	q := words(query)
	keys, _, err := sdk.KV.List(prefix, 0)
	if err != nil {
		return denial(err)
	}
	type match struct {
		id, text string
		score    float64
	}
	var found []match
	scanned := 0
	for _, key := range keys {
		if key == seqKey {
			continue
		}
		value, present, err := sdk.KV.Get(key)
		if err != nil {
			return denial(err)
		}
		if !present {
			continue
		}
		scanned++
		rec := sdk.Object([]byte(value))
		text, _ := rec.String("text")
		stored, _ := rec.StringArray("words")
		found = append(found, match{id: key, text: text, score: overlap(q, stored)})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	if len(found) > k {
		found = found[:k]
	}
	matches := make([]any, 0, len(found))
	for _, m := range found {
		matches = append(matches, map[string]any{"id": m.id, "text": m.text, "score": m.score})
	}
	return map[string]any{"matches": matches, "scanned": scanned, "limit": k}, nil
}

func forget(c sdk.Call) (any, error) {
	id, ok := c.Args().String("id")
	if !ok || id == "" || id == seqKey {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "memory.forget requires arguments.id (a stored id)")
	}
	deleted, err := sdk.KV.Delete(id)
	if err != nil {
		return denial(err)
	}
	return map[string]any{"deleted": deleted}, nil
}

func nextSeq() (int, error) {
	value, found, err := sdk.KV.Get(seqKey)
	if err != nil {
		return 0, err
	}
	seq := 0
	if found {
		seq, _ = strconv.Atoi(value)
	}
	return seq + 1, nil
}

// .
// .
func words(s string) []string {
	seen := map[string]bool{}
	var out []string
	word := make([]byte, 0, 32)
	flush := func() {
		if len(word) > 0 {
			w := string(word)
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
			word = word[:0]
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			word = append(word, c)
		} else {
			flush()
		}
	}
	flush()
	sort.Strings(out)
	return out
}

// .
func overlap(query, stored []string) float64 {
	if len(query) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, w := range stored {
		have[w] = true
	}
	hit := 0
	for _, w := range query {
		if have[w] {
			hit++
		}
	}
	return float64(hit) / float64(len(query))
}

// .
// .
func record(text string, ws []string) []byte {
	out := append([]byte(`{"text":`), jsonString(text)...)
	out = append(out, `,"words":[`...)
	for i, w := range ws {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, jsonString(w)...)
	}
	return append(out, ']', '}')
}

func jsonString(s string) []byte {
	out := []byte{'"'}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			out = append(out, '\\', c)
		case c == '\n':
			out = append(out, '\\', 'n')
		case c == '\r':
			out = append(out, '\\', 'r')
		case c == '\t':
			out = append(out, '\\', 't')
		case c < 0x20:
			const hex = "0123456789abcdef"
			out = append(out, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		default:
			out = append(out, c)
		}
	}
	return append(out, '"')
}

func denial(err error) (any, error) {
	if d, ok := sdk.AsDenied(err); ok {
		return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant kv in plugins.grants to enable this memory")
	}
	return nil, err
}

func main() { sdk.MainDescribe() }
