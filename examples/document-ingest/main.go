// .
// .
// .
// .
// .
// .
// .
// .
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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
		Summary:      "Read a text file from a granted folder and store it in chunks",
		Input:        "schemas/ingest_in.json",
		Output:       "schemas/ingest_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"fs.roots", "fs.private", "ring4.kv"},
		Family:       "documents",
		Keywords:     []string{"ingest", "file", "document", "read", "chunks"},
		Examples:     []string{`{"root": "docs", "path": "notes/2026-09.md"}`},
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
		Summary:      "List what a granted folder holds",
		Input:        "schemas/list_in.json",
		Output:       "schemas/list_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"fs.roots"},
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
	root, ok := c.Args().String("root")
	if !ok || root == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "document.ingest requires arguments.root (a granted folder's name)")
	}
	path, ok := c.Args().String("path")
	if !ok || path == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "document.ingest requires arguments.path (relative to the root)")
	}
	text, err := sdk.Files.ReadAll(root, path, maxBytes)
	if err != nil {
		return failure(err)
	}
	size := chunkSize()
	var chunks []string
	for start := 0; start < len(text); start += size {
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, string(text[start:end]))
	}
	stored := 0
	for i, ch := range chunks {
		key := fmt.Sprintf("%s%s:%d", prefix, path, i)
		if _, err := sdk.KV.Put(key, ch); err != nil {
			return failure(err)
		}
		stored++
	}
	// .
	// .
	line := fmt.Sprintf("%s\t%d chunks\t%d bytes\n", path, stored, len(text))
	if err := sdk.Files.Append(sdk.PrivateRoot, "index.tsv", []byte(line)); err != nil {
		return failure(err)
	}
	return map[string]any{"path": path, "bytes": len(text), "chunks": stored, "chunk_chars": size}, nil
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
	root, ok := c.Args().String("root")
	if !ok || root == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "document.list requires arguments.root")
	}
	path, _ := c.Args().String("path")
	entries, truncated, err := sdk.Files.List(root, path)
	if err != nil {
		return failure(err)
	}
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"name": e.Name, "dir": e.Dir, "size": e.Size, "symlink": e.Symlink})
	}
	return map[string]any{"root": root, "entries": out, "truncated": truncated}, nil
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
		return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant a root (plugins.grants.<id>.roots) and kv to enable this plugin")
	}
	return nil, err
}

// .
// .
func main() { sdk.MainDescribe() }

var _ = strconv.Itoa
