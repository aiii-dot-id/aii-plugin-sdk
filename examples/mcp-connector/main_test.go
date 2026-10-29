package main

// The connector's forwarding against its host's contract, over the native
// lane: this test binary re-executes itself as the connector (TestMain), so
// a published tool's call reaches the handler the guest carries, behind the
// kit's own dispatch, over framed BBB. The host is a stand-in that answers
// settings.get and http.post in the broker's reply vocabulary, and what the
// connector asked it to post is what the server outside would receive.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// serveEnv starts this binary as the connector instead of as the tests.
const serveEnv = "AIISDK_EXAMPLE_MCP_SERVE"

func TestMain(m *testing.M) {
	if os.Getenv(serveEnv) == "1" {
		p := sdk.New("com.aiii.examples.mcp-connector")
		p.HandleDynamic(forward)
		if err := p.Serve(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sentToTheServer calls the published tool mcp.echo with arguments, as the
// host dispatches it, and returns the arguments member of the tools/call
// the connector posted, byte for byte.
func sentToTheServer(t *testing.T, arguments string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), serveEnv+"=1")
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		in.Close()
		cmd.Wait()
	}()
	call := `{"jsonrpc":"2.0","id":"call","method":"invoke.call","params":{"operation":"mcp.echo","arguments":` + arguments + `}}`
	if err := sdk.WriteFrame(in, []byte(call), sdk.MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	sent := ""
	for {
		raw, err := sdk.ReadFrame(out, sdk.MaxControlFrameBytes)
		if err != nil {
			t.Fatal(err)
		}
		var frame struct {
			ID     json.RawMessage
			Method string
			Params struct {
				Operation string
				Arguments struct{ Body string }
			}
			Result struct{ Status string }
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		if frame.Method == "" {
			if frame.Result.Status != "succeeded" {
				t.Fatalf("the forwarded call did not succeed: %s", raw)
			}
			return sent
		}
		var result string
		switch frame.Params.Operation {
		case "settings.get":
			result = `{"success":true,"status":"succeeded","operation_result":{"values":{"server_url":"https://mcp.example.test/rpc"}}}`
		case "http.post":
			var rpc struct {
				Method string
				Params struct {
					Name      string
					Arguments json.RawMessage
				}
			}
			if err := json.Unmarshal([]byte(frame.Params.Arguments.Body), &rpc); err != nil || rpc.Method != "tools/call" || rpc.Params.Name != "echo" {
				t.Fatalf("the connector posted no tools/call for echo: %s", frame.Params.Arguments.Body)
			}
			sent = string(rpc.Params.Arguments)
			result = `{"success":true,"status":"succeeded","operation_result":{"http_status":200,"body":{"jsonrpc":"2.0","id":1,"result":{}}}}`
		default:
			t.Fatalf("the connector asked the host for %s", frame.Params.Operation)
		}
		reply := `{"jsonrpc":"2.0","id":` + string(frame.ID) + `,"result":` + result + `}`
		if err := sdk.WriteFrame(in, []byte(reply), sdk.MaxControlFrameBytes); err != nil {
			t.Fatal(err)
		}
	}
}

// A CLOSED-SCHEMA TOOL RECEIVES EXACTLY ITS DECLARED ARGUMENTS. The host
// writes its own members into
// every call under names that begin "_host": the clock always, a confirmed
// act's stamp, this plugin's webhook addresses, a calibration frame. The
// connector forwarded the arguments verbatim, so a server outside was told
// the host's metadata, and a tool whose input schema admits no other
// property (additionalProperties false) would refuse every call. Here the
// tool declares text and count: it receives those two, in the caller's
// order and spelling, and nothing else, whichever reserved names came and
// however one is spelled.
func TestAClosedSchemaToolReceivesExactlyItsDeclaredArguments(t *testing.T) {
	for _, c := range []struct{ name, arguments, want string }{
		{"the host's members of today",
			`{"text":"hello","_host_now_ms":12345,"count":2,"_host_operator_act":{"id":"act-1","confirmed_at":"2026-01-02T03:04:05Z"},"_host_webhooks":{"/hook":"https://example.test/hook"},"_host_calibration":{"version":1,"available":false}}`,
			`{"text":"hello","count":2}`},
		{"a reserved name this connector has never seen", `{"_hostnext":[1,{"a":"}"}],"text":"hello","count":2}`, `{"text":"hello","count":2}`},
		{"a reserved name spelled with an escape", `{"text":"hello","_host_now_ms":12345,"count":2}`, `{"text":"hello","count":2}`},
		{"arguments written with white space", "{ \"text\" : \"hello\" ,\n\t\"_host_now_ms\" : 12345 , \"count\" : 2 }", `{"text":"hello","count":2}`},
		{"a call the caller gave no arguments", `{"_host_now_ms":12345}`, `{}`},
	} {
		if got := sentToTheServer(t, c.arguments); got != c.want {
			t.Errorf("%s: the server received %s, want exactly %s", c.name, got, c.want)
		}
	}
}

// A GENUINE ARGUMENT IS FORWARDED UNTOUCHED. The reserved namespace is a
// member name that BEGINS "_host", at the top of the arguments, and nothing
// wider: a name that only contains those letters, a value that spells one
// of the host's names, and whatever is nested inside a genuine argument
// are the caller's own, and reach the server in the caller's order with
// every value as it was written.
func TestAGenuineArgumentThatOnlyContainsHostIsUntouched(t *testing.T) {
	const genuine = `"my_host":"db1","x_host_now_ms":7,"note":"_host_now_ms is a name, \"_host\" a prefix","nested":{"_host_now_ms":1,"list":[1.50,{"_host_operator_act":null}]},"host":"_host","amount":1e2`
	if got, want := sentToTheServer(t, `{"_host_now_ms":12345,`+genuine+`}`), `{`+genuine+`}`; got != want {
		t.Errorf("the server received\n%s\nwant exactly\n%s", got, want)
	}
}
