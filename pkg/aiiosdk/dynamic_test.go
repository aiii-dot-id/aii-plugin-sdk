package aiiosdk

import (
	"strings"
	"testing"
)

// .
// .
// .
func TestDynamicHandlerAnswersUnknownOperations(t *testing.T) {
	p := New("com.example.mcp")
	p.Handle("mcp.refresh", func(c Call) (any, error) { return map[string]any{"static": true}, nil })
	var seen string
	p.HandleDynamic(func(name string, c Call) (any, error) {
		seen = name
		return map[string]any{"forwarded": name}, nil
	})
	frame := func(op string) []byte {
		return []byte(`{"jsonrpc":"2.0","id":"h1","method":"invoke.call","params":{"operation":"` + op + `","arguments":{"path":"/"}}}`)
	}
	out := string(p.respond(frame("mcp.list_files")))
	if !strings.Contains(out, `"forwarded":"mcp.list_files"`) || seen != "mcp.list_files" {
		t.Fatalf("dynamic: %s", out)
	}
	if out := string(p.respond(frame("mcp.refresh"))); !strings.Contains(out, `"static":true`) {
		t.Fatalf("static wins: %s", out)
	}
	bare := New("com.example.bare")
	if out := string(bare.respond(frame("nothing"))); !strings.Contains(out, "OPERATION_NOT_FOUND") {
		t.Fatalf("no dynamic handler, no answer: %s", out)
	}
}
