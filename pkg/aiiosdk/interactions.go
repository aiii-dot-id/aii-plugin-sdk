package aiiosdk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const InteractionVersion = 1
const InteractionReadLimit = 16 << 10

type InteractionsClient struct{}

var Interactions InteractionsClient

func (InteractionsClient) Query(q InteractionQuery) (*InteractionPage, error) {
	if q.Version == 0 {
		q.Version = InteractionVersion
	}
	if q.Version != InteractionVersion {
		return nil, fmt.Errorf("aiiosdk: unsupported interaction version")
	}
	args, err := q.MarshalJSON()
	if err != nil {
		return nil, err
	}
	result, err := InvokeCall("interaction.query", nil, json.RawMessage(args))
	if err != nil {
		return nil, err
	}
	var page InteractionPage
	if err = decodeInteraction(result.OperationResult, &page); err != nil {
		return nil, err
	}
	if page.Version != InteractionVersion || page.Incarnation == "" || page.Fingerprint == "" || page.Availability == "" || page.Rows == nil {
		return nil, fmt.Errorf("aiiosdk: incomplete interaction snapshot")
	}
	source := q.Source
	if source == "" {
		source = "recorded"
	}
	if page.Source != source || page.Cursor != q.Cursor {
		return nil, fmt.Errorf("aiiosdk: interaction response does not match requested source/range")
	}
	var last uint64
	for i, r := range page.Rows {
		if r.ID == "" || (i > 0 && r.Sequence <= last) {
			return nil, fmt.Errorf("aiiosdk: unordered/unidentified interaction records")
		}
		last = r.Sequence
	}
	return &page, nil
}

func (InteractionsClient) Read(r InteractionReadRequest) (data []byte, eof bool, size int64, err error) {
	if r.Version == 0 {
		r.Version = InteractionVersion
	}
	if r.Length == 0 {
		r.Length = InteractionReadLimit
	}
	if r.Version != InteractionVersion || r.Length < 1 || r.Length > InteractionReadLimit || r.Offset < 0 {
		return nil, false, 0, fmt.Errorf("aiiosdk: invalid interaction range")
	}
	args, err := r.MarshalJSON()
	if err != nil {
		return nil, false, 0, err
	}
	result, err := InvokeCall("interaction.read", nil, json.RawMessage(args))
	if err != nil {
		return nil, false, 0, err
	}
	var chunk InteractionReadResult
	if err = decodeInteraction(result.OperationResult, &chunk); err != nil {
		return nil, false, 0, err
	}
	source := r.Source
	if source == "" {
		source = "recorded"
	}
	if chunk.Version != InteractionVersion || chunk.ID != r.ID || chunk.Incarnation != r.Incarnation || chunk.Source != source || chunk.SHA256 != r.SHA256 || chunk.Offset != r.Offset {
		return nil, false, 0, fmt.Errorf("aiiosdk: interaction detail reference changed")
	}
	data, err = base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil {
		return nil, false, 0, fmt.Errorf("aiiosdk: malformed interaction detail bytes")
	}
	end := chunk.Offset + int64(len(data))
	if chunk.Bytes != len(data) || len(data) > r.Length || end < chunk.Offset || end > chunk.Size || chunk.EOF != (end == chunk.Size) || (!chunk.EOF && len(data) == 0) {
		return nil, false, 0, fmt.Errorf("aiiosdk: inconsistent interaction detail range")
	}
	return data, chunk.EOF, chunk.Size, nil
}

func (q InteractionQuery) MarshalJSON() ([]byte, error) {
	f := q.Filter
	kinds := make([]any, len(f.Kinds))
	for i, k := range f.Kinds {
		kinds[i] = string(k)
	}
	filter := map[string]any{"kinds": kinds, "role": string(f.Role), "turn_id": f.TurnID, "session_id": f.SessionID, "work_session_id": f.WorkSession, "project_id": f.ProjectID, "related_id": f.RelatedID, "from": f.From, "until": f.Until, "undated": f.Undated}
	return marshalValue(map[string]any{"version": q.Version, "source": q.Source, "filter": filter, "cursor": q.Cursor, "limit": q.Limit})
}
func (r InteractionReadRequest) MarshalJSON() ([]byte, error) {
	return marshalValue(map[string]any{"version": r.Version, "source": r.Source, "id": r.ID, "incarnation": r.Incarnation, "sha256": r.SHA256, "offset": r.Offset, "length": r.Length})
}
