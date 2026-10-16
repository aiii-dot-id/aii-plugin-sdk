package aiiosdk

import (
	"fmt"
	"time"
)

type MemoryClient struct{}

var Memory MemoryClient

const (
	DecayDefault = "carrd"
	DecayNone    = "none"
)

type Remembered struct {
	ID string

	Outcome string

	Of        string
	CreatedAt string
	Scope     string
	Context   MemoryContext
	Evidence  []MemoryEvidenceRef
}

type MemoryContext struct {
	Applicability     string `json:"applicability"`
	ProjectID         string `json:"project_id,omitempty"`
	WorkSessionID     string `json:"work_session_id,omitempty"`
	SourceIncarnation string `json:"source_incarnation,omitempty"`
}

type MemoryEvidenceRef struct {
	ID           string `json:"id"`
	Sequence     string `json:"sequence"`
	SHA256       string `json:"sha256"`
	Incarnation  string `json:"incarnation"`
	Availability string `json:"availability,omitempty"`
}

type RememberOption func(args map[string]any)

func MemoryApplicability(value string) RememberOption {
	return func(args map[string]any) { args["applicability"] = value }
}

func MemoryEvidence(refs ...MemoryEvidenceRef) RememberOption {
	return func(args map[string]any) {

		items := make([]map[string]any, len(refs))
		for i, ref := range refs {
			items[i] = map[string]any{"id": ref.ID, "sequence": ref.Sequence, "sha256": ref.SHA256, "incarnation": ref.Incarnation}
			if ref.Availability != "" {
				items[i]["availability"] = ref.Availability
			}
		}
		args["evidence"] = items
	}
}

func Supersedes(id string) RememberOption {
	return func(args map[string]any) { args["supersedes"] = id }
}

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
	out.Context, out.Evidence, err = memoryMetadata(or)
	if err != nil {
		return out, err
	}

	if out.ID == "" || out.Outcome == "" {
		return out, fmt.Errorf("aiiosdk: memory.remember result carries no id and outcome")
	}
	return out, nil
}

type RecallOption func(args map[string]any)

func MemorySelection(value string) RecallOption {
	return func(args map[string]any) { args["selection"] = value }
}

func RecentMemories() RecallOption {
	return func(args map[string]any) { args["recent"] = true }
}

func Exact() RecallOption { return func(a map[string]any) { a["exact"] = true } }

func Since(t time.Time) RecallOption {
	return func(a map[string]any) { a["since"] = t.UTC().Format(time.RFC3339) }
}

func Before(t time.Time) RecallOption {
	return func(a map[string]any) { a["before"] = t.UTC().Format(time.RFC3339) }
}

func Limit(n int) RecallOption { return func(a map[string]any) { a["limit"] = n } }

func ByID(id string) RecallOption { return func(a map[string]any) { a["id"] = id } }

func Decay(policy string) RecallOption { return func(a map[string]any) { a["decay"] = policy } }

const DecayACTR = "actr"

type RecallHit struct {
	ID      string
	Text    string
	Snippet string

	Match string

	Similarity float64

	Score       float64
	Strength    float64
	Attribution string
	Ring        int
	Time        string
	Accesses    int64
	Class       string

	SupersededBy string
	Context      MemoryContext
	Evidence     []MemoryEvidenceRef
}

type RecallResult struct {
	Hits []RecallHit

	Status    string
	Matched   int
	Shown     int
	Policy    string
	Truncated bool

	Meaning       string
	MeaningDetail string
	MeaningBasis  string
}

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
		hit.Context, hit.Evidence, err = memoryMetadata(h)
		if err != nil {
			return out, err
		}

		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

func memoryMetadata(o Object) (MemoryContext, []MemoryEvidenceRef, error) {
	var c MemoryContext
	refs := []MemoryEvidenceRef{}
	if raw := o.Raw("context"); raw != nil {
		if _, ok := objectMembers(raw); !ok {
			return c, nil, fmt.Errorf("aiiosdk: memory context is not an object")
		}
		v := Object(raw)
		c.Applicability, _ = v.String("applicability")
		c.ProjectID, _ = v.String("project_id")
		c.WorkSessionID, _ = v.String("work_session_id")
		c.SourceIncarnation, _ = v.String("source_incarnation")
	}
	if raw := o.Raw("evidence"); raw != nil {
		elems, ok := arrayElements(raw)
		if !ok {
			return c, nil, fmt.Errorf("aiiosdk: memory evidence is not an array")
		}
		for _, raw := range elems {
			if _, ok := objectMembers(raw); !ok {
				return c, nil, fmt.Errorf("aiiosdk: memory evidence entry is not an object")
			}
			v := Object(raw)
			var r MemoryEvidenceRef
			r.ID, _ = v.String("id")
			r.Sequence, _ = v.String("sequence")
			r.SHA256, _ = v.String("sha256")
			r.Incarnation, _ = v.String("incarnation")
			r.Availability, _ = v.String("availability")
			refs = append(refs, r)
		}
	}
	return c, refs, nil
}
