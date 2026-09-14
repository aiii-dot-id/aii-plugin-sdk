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

import (
	"fmt"
	"time"
)

// .
// .
type MemoryClient struct{}

// .
var Memory MemoryClient

// .
// .
const (
	DecayDefault = "carrd"
	DecayNone    = "none"
)

// .
type Remembered struct {
	// .
	// .
	ID string
	// .
	// .
	// .
	Outcome string
	// .
	Of        string
	CreatedAt string
	Scope     string
}

// .
type RememberOption func(args map[string]any)

// .
// .
// .
func Supersedes(id string) RememberOption {
	return func(args map[string]any) { args["supersedes"] = id }
}

// .
// .
// .
// .
func (MemoryClient) Remember(text string, opts ...RememberOption) (Remembered, error) {
	args := map[string]any{"text": text}
	for _, o := range opts {
		o(args)
	}
	var out Remembered
	res, err := InvokeCall("memory.remember", nil, args)
	if err != nil {
		return out, err
	}
	or := Object(res.OperationResult)
	out.ID, _ = or.String("id")
	out.Outcome, _ = or.String("outcome")
	out.Of, _ = or.String("of")
	out.CreatedAt, _ = or.String("created_at")
	out.Scope, _ = or.String("scope")
	if out.ID == "" || out.Outcome == "" {
		return out, fmt.Errorf("aiiosdk: memory.remember result carries no id and outcome")
	}
	return out, nil
}

// .
type RecallOption func(args map[string]any)

// .
// .
func Exact() RecallOption { return func(a map[string]any) { a["exact"] = true } }

// .
func Since(t time.Time) RecallOption {
	return func(a map[string]any) { a["since"] = t.UTC().Format(time.RFC3339) }
}

// .
func Before(t time.Time) RecallOption {
	return func(a map[string]any) { a["before"] = t.UTC().Format(time.RFC3339) }
}

// .
func Limit(n int) RecallOption { return func(a map[string]any) { a["limit"] = n } }

// .
func ByID(id string) RecallOption { return func(a map[string]any) { a["id"] = id } }

// .
// .
// .
// .
func Decay(policy string) RecallOption { return func(a map[string]any) { a["decay"] = policy } }

// .
const DecayACTR = "actr"

// .
type RecallHit struct {
	ID      string
	Text    string
	Snippet string
	// .
	// .
	// .
	// .
	Match string
	// .
	// .
	Similarity float64
	// .
	// .
	Score       float64
	Strength    float64
	Attribution string
	Ring        int
	Time        string
	Accesses    int64
	Class       string
	// .
	SupersededBy string
}

// .
type RecallResult struct {
	Hits []RecallHit
	// .
	// .
	Status    string
	Matched   int
	Shown     int
	Policy    string
	Truncated bool
	// .
	// .
	// .
	// .
	// .
	Meaning       string
	MeaningDetail string
	MeaningBasis  string
}

// .
// .
// .
// .
// .
func (MemoryClient) Recall(query string, opts ...RecallOption) (RecallResult, error) {
	args := map[string]any{"query": query}
	for _, o := range opts {
		o(args)
	}
	var out RecallResult
	res, err := InvokeCall("memory.recall", nil, args)
	if err != nil {
		if oe, ok := asOperationError(err); ok && oe.ReasonCode == "MEMORY_NOT_FOUND" {
			out.Status = "found_nothing"
			return out, nil
		}
		return out, err
	}
	or := Object(res.OperationResult)
	out.Status, _ = or.String("status")
	if n, ok := or.Int("matched"); ok {
		out.Matched = int(n)
	}
	if n, ok := or.Int("shown"); ok {
		out.Shown = int(n)
	}
	out.Policy, _ = or.String("policy")
	out.Truncated, _ = or.Bool("truncated")
	if raw := or.Raw("meaning"); raw != nil {
		m := Object(raw)
		out.Meaning, _ = m.String("status")
		out.MeaningDetail, _ = m.String("detail")
		out.MeaningBasis, _ = m.String("basis")
	}
	raw := or.Raw("hits")
	if raw == nil {
		return out, fmt.Errorf("aiiosdk: memory.recall result carries no hits array")
	}
	elems, ok := arrayElements(raw)
	if !ok {
		return out, fmt.Errorf("aiiosdk: memory.recall hits is not an array")
	}
	for _, e := range elems {
		h := Object(e)
		var hit RecallHit
		hit.ID, _ = h.String("id")
		hit.Text, _ = h.String("text")
		hit.Snippet, _ = h.String("snippet")
		hit.Match, _ = h.String("match")
		hit.Score, _ = h.Float("score")
		hit.Strength, _ = h.Float("strength")
		hit.Attribution, _ = h.String("attribution")
		if n, ok := h.Int("ring"); ok {
			hit.Ring = int(n)
		}
		hit.Time, _ = h.String("time")
		hit.Accesses, _ = h.Int("accesses")
		hit.Class, _ = h.String("class")
		hit.Similarity, _ = h.Float("similarity")
		hit.SupersededBy, _ = h.String("superseded_by")
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}
