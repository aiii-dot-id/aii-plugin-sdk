package main

import (
	"strconv"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func init() {
	p := sdk.New("com.aiii.examples.compat")

	p.Describe("remember", sdk.Descriptor{
		Summary:      "Remember a text in this plugin's own record, or correct one it holds",
		Input:        "schemas/remember_in.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"ring4.memory"},
	})
	p.Handle("remember", remember)

	p.Describe("recall", sdk.Descriptor{
		Summary:      "Search this plugin's own record, or read one memory by id",
		Input:        "schemas/recall_in.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"ring4.memory"},
	})
	p.Handle("recall", recall)

	p.Describe("interactions", sdk.Descriptor{
		Summary:        "Read one page of the identity's operator-visible history",
		Input:          "schemas/interactions_in.json",
		Effects:        sdk.EffectsReadInternal,
		Capabilities:   []string{"interaction.read"},
		MaxResultBytes: 32768,
	})
	p.Handle("interactions", interactions)

	p.Describe("overreach", sdk.Descriptor{
		Summary:      "Declare a read, then try to write a memory",
		Input:        "schemas/overreach_in.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"ring4.memory"},
	})
	p.Handle("overreach", overreach)

	p.Describe("spin", sdk.Descriptor{
		Summary:      "Remember a text, then compute until the host stops the call",
		Input:        "schemas/spin_in.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"ring4.memory"},
	})
	p.Handle("spin", spin)

	p.Run()
}

func remember(c sdk.Call) (any, error) {
	text, _ := c.Args().String("text")
	if text == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "remember requires arguments.text (a non-empty string)")
	}
	var opts []sdk.RememberOption
	if id, _ := c.Args().String("supersedes"); id != "" {
		opts = append(opts, sdk.Supersedes(id))
	}
	r, err := sdk.Memory.Remember(text, opts...)
	if err != nil {
		return nil, err
	}
	return remembered(r), nil
}

func recall(c sdk.Call) (any, error) {
	query, _ := c.Args().String("query")
	id, _ := c.Args().String("id")
	var opts []sdk.RecallOption
	switch {
	case id != "":
		opts = append(opts, sdk.ByID(id))
	case query == "":
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "recall requires arguments.query or arguments.id")
	}
	res, err := sdk.Memory.Recall(query, opts...)
	if err != nil {
		return nil, err
	}
	hits := make([]any, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, map[string]any{"id": h.ID, "text": h.Text, "match": h.Match, "superseded_by": h.SupersededBy})
	}
	return map[string]any{"status": res.Status, "matched": res.Matched, "hits": hits}, nil
}

func interactions(c sdk.Call) (any, error) {
	cursor, _ := c.Args().String("cursor")
	limit, _ := c.Args().Int("limit")
	page, err := sdk.Interactions.Query(sdk.InteractionQuery{Cursor: cursor, Limit: int(limit)})
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(page.Rows))
	for _, r := range page.Rows {
		row := map[string]any{
			"id": r.ID, "sequence": strconv.FormatUint(r.Sequence, 10), "kind": string(r.Kind),
			"role": string(r.Role), "turn_id": r.TurnID, "content": r.Content, "outcome": string(r.Outcome),
			"tool": r.Details.Tool, "model": r.Details.Model,
		}
		if r.Details.DurationMS != nil {
			row["duration_ms"] = *r.Details.DurationMS
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"rows": rows, "next_cursor": page.NextCursor, "lost": page.Lost,
		"availability": page.Availability, "incarnation": page.Incarnation,
	}, nil
}

func overreach(c sdk.Call) (any, error) {
	r, err := sdk.Memory.Remember("written by an operation that declared a read")
	if err != nil {
		return nil, err
	}
	return remembered(r), nil
}

var spun uint64

func spin(c sdk.Call) (any, error) {
	text, _ := c.Args().String("text")
	if text == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "spin requires arguments.text (a non-empty string)")
	}
	if _, err := sdk.Memory.Remember(text); err != nil {
		return nil, err
	}
	x := uint64(len(text)) | 1
	for {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		spun = x
	}
}

func remembered(r sdk.Remembered) map[string]any {
	return map[string]any{"id": r.ID, "outcome": r.Outcome, "of": r.Of, "scope": r.Scope, "created_at": r.CreatedAt}
}

func main() { sdk.MainDescribe() }
