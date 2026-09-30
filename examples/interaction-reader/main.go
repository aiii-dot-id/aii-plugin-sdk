package main

import (
	"encoding/base64"
	"fmt"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func readerPlugin() *sdk.Plugin {
	p := sdk.New("org.example.interaction-reader")
	p.Describe("reader.query", sdk.Descriptor{Summary: "Read a bounded page of operator-visible interactions", Input: "schemas/query_in.json", Output: "schemas/query_out.json", Effects: sdk.EffectsReadInternal, Capabilities: []string{"interaction.read"}, MaxResultBytes: 32768})
	p.Handle("reader.query", func(c sdk.Call) (any, error) {
		args := c.Args()
		source, _ := args.String("source")
		cursor, _ := args.String("cursor")
		project, _ := args.String("project_id")
		page, err := sdk.Interactions.Query(sdk.InteractionQuery{Source: source, Cursor: cursor, Filter: sdk.InteractionFilter{ProjectID: project}})
		if err != nil {
			return nil, err
		}
		rows := make([]any, 0, len(page.Rows))
		for _, r := range page.Rows {
			item := map[string]any{"id": r.ID, "sequence": fmt.Sprint(r.Sequence), "kind": string(r.Kind), "role": string(r.Role), "content": r.Content, "related_id": r.RelatedID, "turn_id": r.TurnID, "outcome": string(r.Outcome), "project_id": r.ProjectID}
			if r.Details.Work != nil {
				w := r.Details.Work
				item["work_session"] = w.Session
				if w.Plan != nil {
					item["plan"] = *w.Plan
				}
				if w.Steps != nil {
					item["steps"] = *w.Steps
				}
				if w.Independent != nil {
					item["independent"] = *w.Independent
				}
				if w.Result != nil {
					item["result"] = *w.Result
				}
				if w.Evidence != nil {
					item["evidence"] = *w.Evidence
				}
				if w.EvidenceReadback != nil {
					item["evidence_readback"] = *w.EvidenceReadback
				}
			}
			if r.Details.Project != nil {
				item["criteria"] = r.Details.Project.After.Acceptance
			}
			if r.ContentRef != nil {
				ref := r.ContentRef
				item["content_ref"] = map[string]any{"id": ref.ID, "source": ref.Source, "incarnation": ref.Incarnation, "sha256": ref.SHA256, "bytes": ref.Bytes}
			}
			rows = append(rows, item)
		}
		return map[string]any{"rows": rows, "source": page.Source, "incarnation": page.Incarnation, "fingerprint": page.Fingerprint, "next_cursor": page.NextCursor}, nil
	})
	p.Describe("reader.read", sdk.Descriptor{Summary: "Read one exact bounded detail range", Input: "schemas/read_in.json", Output: "schemas/read_out.json", Effects: sdk.EffectsReadInternal, Capabilities: []string{"interaction.read"}, MaxResultBytes: 32768})
	p.Handle("reader.read", func(c sdk.Call) (any, error) {
		a := c.Args()
		id, _ := a.String("id")
		source, _ := a.String("source")
		inc, _ := a.String("incarnation")
		hash, _ := a.String("sha256")
		offset, _ := a.Int("offset")
		length, _ := a.Int("length")
		data, eof, size, err := sdk.Interactions.Read(sdk.InteractionReadRequest{ID: id, Source: source, Incarnation: inc, SHA256: hash, Offset: offset, Length: int(length)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data), "bytes": len(data), "offset": offset, "size": size, "eof": eof}, nil
	})
	return p
}
