package aiiosdk

import (
	"encoding/json"
	"testing"
)

func TestHostNowMillis(t *testing.T) {
	c := Call{Arguments: json.RawMessage(`{"note":"x","_host_now_ms":1787175740123}`)}
	now, ok := c.HostNowMillis()
	if !ok || now != 1787175740123 {
		t.Fatalf("got (%d,%v), want (1787175740123,true)", now, ok)
	}
	c2 := Call{Arguments: json.RawMessage(`{"note":"x"}`)}
	if _, ok := c2.HostNowMillis(); ok {
		t.Fatal("absent _host_now_ms must report ok=false (older host)")
	}
}
