//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestFilesReadAllFollowsReturnedChunksAndKeepsItsLimit(t *testing.T) {
	data := bytes.Repeat([]byte{0, 255, 42}, 3<<20)
	withHost(t, func(params []byte) ([]byte, error) {
		var p struct {
			Operation string `json:"operation"`
			Arguments struct {
				Offset int `json:"offset"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatal(err)
		}
		if p.Operation != "fs.read" || p.Arguments.Offset < 0 || p.Arguments.Offset >= len(data) {
			t.Fatalf("bad read: %s", params)
		}
		start := p.Arguments.Offset
		end := min(start+512<<10, len(data))
		return json.Marshal(map[string]any{"status": "succeeded", "success": true, "operation_result": map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data[start:end]), "bytes": end - start, "size": len(data), "eof": end == len(data)}})
	})
	got, err := Files.ReadAll(PrivateRoot, "large.bin", len(data))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("chunk reassembly: %d bytes, %v", len(got), err)
	}
	if _, err := Files.ReadAll(PrivateRoot, "large.bin", 0); err == nil {
		t.Fatal("the default 8 MiB limit silently expanded")
	}
}
