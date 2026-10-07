package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	prefix   = "chunk:"
	indexKey = "index"
	maxBytes = 2 << 20
)

func init() {
	p := sdk.New("com.aiii.examples.document-ingest")

	p.Describe("document.ingest", sdk.Descriptor{
		Summary:      "Read a text file from the identity's sandbox and store it in chunks",
		Input:        "schemas/ingest_in.json",
		Output:       "schemas/ingest_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"fs.sandbox", "fs.private", "ring4.kv"},
		Family:       "documents",
		Keywords:     []string{"ingest", "file", "document", "read", "chunks"},
		Examples:     []string{`{"path": "notes/2026-09.md"}`},
	})
	p.Handle("document.ingest", ingest)

	p.Describe("document.recall", sdk.Descriptor{
		Summary:      "Find the stored chunks that share the most words with a query",
		Input:        "schemas/recall_in.json",
		Output:       "schemas/recall_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"ring4.kv"},
		Family:       "documents",
	})
	p.Handle("document.recall", recall)

	p.Describe("document.list", sdk.Descriptor{
		Summary:      "List what a folder in the identity's sandbox holds",
		Input:        "schemas/list_in.json",
		Output:       "schemas/list_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"fs.sandbox"},
		Family:       "documents",
	})
	p.Handle("document.list", list)

	p.Run()
}

func chunkSize() int {
	n := 400
	if vals, err := sdk.Settings.Load(); err == nil {
		if v, ok := vals.Int("chunk_chars"); ok && v >= 80 && v <= 4000 {
			n = int(v)
		}
	}
	return n
}

func ingest(c sdk.Call) (any, error) {
	path, ok := c.Args().String("path")
	if !ok || path == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "document.ingest requires arguments.path (as the identity's own tools take it)")
	}
	text, err := sdk.Files.ReadAll(sdk.SandboxRoot, path, maxBytes)
	if err != nil {
		return failure(err)
	}
	size := chunkSize()
	chunks := chunksOf(text, size)
	stored := 0
	for i, ch := range chunks {
		if _, err := sdk.KV.Put(chunkKey(path, i), ch); err != nil {
			return failure(err)
		}
		stored++
	}

	removed, err := removeStale(sdk.KV, path, len(chunks))
	if err != nil {
		return failure(err)
	}

	line := fmt.Sprintf("%s\t%d chunks\t%d bytes\n", path, stored, len(text))
	if err := sdk.Files.Append(sdk.PrivateRoot, "index.tsv", []byte(line)); err != nil {
		return failure(err)
	}
	return map[string]any{"path": path, "bytes": len(text), "chunks": stored, "chunk_chars": size, "removed": removed}, nil
}

func chunkKey(path string, i int) string { return fmt.Sprintf("%s%s:%d", prefix, path, i) }

type chunkStore interface {
	Get(key string) (string, bool, error)
	Delete(key string) (bool, error)
}

func removeStale(store chunkStore, path string, kept int) (int, error) {
	end := kept
	for {
		_, present, err := store.Get(chunkKey(path, end))
		if err != nil {
			return 0, err
		}
		if !present {
			break
		}
		end++
	}
	removed := 0
	for i := end - 1; i >= kept; i-- {
		deleted, err := store.Delete(chunkKey(path, i))
		if err != nil {
			return removed, err
		}
		if deleted {
			removed++
		}
	}
	return removed, nil
}

func chunksOf(text []byte, size int) []string {
	var out []string
	start, n := 0, 0
	for i := 0; i < len(text); {
		_, w := utf8.DecodeRune(text[i:])
		i += w
		n++
		if n == size {
			out = append(out, string(text[start:i]))
			start, n = i, 0
		}
	}
	if start < len(text) {
		out = append(out, string(text[start:]))
	}
	return out
}

func recall(c sdk.Call) (any, error) {
	query, ok := c.Args().String("query")
	if !ok || strings.TrimSpace(query) == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "document.recall requires arguments.query")
	}
	k := 3
	if n, ok := c.Args().Int("k"); ok && n >= 1 && n <= 20 {
		k = int(n)
	}
	want := words(query)
	keys, _, err := sdk.KV.List(prefix, 0)
	if err != nil {
		return failure(err)
	}
	type hit struct {
		key, text string
		score     int
	}
	var hits []hit
	for _, key := range keys {
		value, present, err := sdk.KV.Get(key)
		if err != nil {
			return failure(err)
		}
		if !present {
			continue
		}
		score := 0
		for w := range words(value) {
			if want[w] {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{key: key, text: value, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > k {
		hits = hits[:k]
	}
	matches := make([]any, 0, len(hits))
	for _, h := range hits {
		matches = append(matches, map[string]any{"key": h.key, "text": h.text, "score": h.score})
	}
	return map[string]any{"matches": matches, "scanned": len(keys)}, nil
}

func list(c sdk.Call) (any, error) {
	path, _ := c.Args().String("path")
	entries, truncated, err := sdk.Files.List(sdk.SandboxRoot, path)
	if err != nil {
		return failure(err)
	}
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"name": e.Name, "dir": e.Dir, "size": e.Size, "symlink": e.Symlink})
	}
	return map[string]any{"path": path, "entries": out, "truncated": truncated}, nil
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

func failure(err error) (any, error) {
	if d, ok := sdk.AsDenied(err); ok {
		return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant files and kv to enable this plugin")
	}
	return nil, err
}

func main() { sdk.MainDescribe() }

var _ = strconv.Itoa
