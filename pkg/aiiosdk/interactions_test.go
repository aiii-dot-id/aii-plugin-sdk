//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestInteractionsReadPreservesSplitUTF8AndRejectsChangedReference(t *testing.T) {
	raw := []byte("😀abc")
	changed := false
	withHost(t, func(params []byte) ([]byte, error) {
		var p struct {
			Operation string                 `json:"operation"`
			Arguments InteractionReadRequest `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatal(err)
		}
		if p.Operation != "interaction.read" {
			t.Fatal(p.Operation)
		}
		r := p.Arguments
		end := min(r.Offset+int64(r.Length), int64(len(raw)))
		id := r.ID
		if changed {
			id = "other"
		}
		return json.Marshal(map[string]any{"status": "succeeded", "success": true, "operation_result": map[string]any{"version": 1, "id": id, "source": "recorded", "incarnation": r.Incarnation, "sha256": r.SHA256, "offset": r.Offset, "size": len(raw), "bytes": end - r.Offset, "data_b64": base64.StdEncoding.EncodeToString(raw[r.Offset:end]), "eof": end == int64(len(raw))}})
	})
	r := InteractionReadRequest{ID: "id", Source: "recorded", Incarnation: "source", SHA256: "hash", Length: 2}
	var got []byte
	for {
		data, eof, _, err := Interactions.Read(r)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, data...)
		r.Offset += int64(len(data))
		if eof {
			break
		}
	}
	if !bytes.Equal(raw, got) {
		t.Fatal("fragment decoding corrupted bytes")
	}
	changed = true
	r.Offset = 0
	if _, _, _, err := Interactions.Read(r); err == nil {
		t.Fatal("changed occurrence accepted")
	}
}
func TestInteractionsQueryRejectsUnsupportedAndUnorderedResponse(t *testing.T) {
	withHost(t, func([]byte) ([]byte, error) {
		return []byte(`{"status":"succeeded","success":true,"operation_result":{"version":1,"identity":"identity","incarnation":"source","source":"recorded","filter":{},"rows":[{"id":"a","sequence":"2"},{"id":"b","sequence":"1"}],"fingerprint":"hash","availability":"complete"}}`), nil
	})
	if _, err := Interactions.Query(InteractionQuery{Version: 2}); err == nil {
		t.Fatal("unsupported version accepted")
	}
	if _, err := Interactions.Query(InteractionQuery{}); err == nil {
		t.Fatal("unordered page accepted")
	}
}
