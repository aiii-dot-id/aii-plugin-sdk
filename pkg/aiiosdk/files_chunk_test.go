//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
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

// ListAfter asks for the page after a name; List, and ListAfter with no name, send no arguments,
// so they answer as before from a host that does not page.
func TestListAfterAsksForTheNextPage(t *testing.T) {
	var sent []string
	hostInvokeStub = func(params []byte) ([]byte, error) {
		sent = append(sent, string(params))
		return []byte(`{"status":"succeeded","ok":true,"success":true,"operation_result":{"entries":[{"name":"b","size":1}],"truncated":false}}`), nil
	}
	defer func() { hostInvokeStub = nil }()
	if _, _, err := Files.List(SandboxRoot, "d"); err != nil {
		t.Fatal(err)
	}
	es, _, err := Files.ListAfter(SandboxRoot, "d", "a")
	if err != nil || len(es) != 1 || es[0].Name != "b" {
		t.Fatalf("%v %+v", err, es)
	}
	if strings.Contains(sent[0], "arguments") || !strings.Contains(sent[1], `"arguments":{"after":"a"}`) {
		t.Fatalf("wire: %q", sent)
	}
}
