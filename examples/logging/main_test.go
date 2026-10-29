package main

// log.read against its host's contract, over the native lane: this test
// binary re-executes itself as the plugin (TestMain), so each case drives
// the handler the guest carries, behind the kit's own dispatch, over framed
// BBB. The host is a stand-in that answers settings.get with the values a
// case sets (none by default) and fs.read as each case scripts it, in the broker's own reply vocabulary
// (aii-os internal/broker/files.go: FS_NOT_FOUND, FS_IO_FAILED).

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// serveEnv starts this binary as the plugin instead of as the tests.
const serveEnv = "AIISDK_EXAMPLE_LOGGING_SERVE"

func TestMain(m *testing.M) {
	if os.Getenv(serveEnv) == "1" {
		p := sdk.New("com.aiii.examples.logging")
		p.Handle("log.read", read)
		if err := p.Serve(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// failed is the broker's failed result for one reason code.
func failed(code string) string {
	return `{"success":false,"ok":false,"status":"failed","reason":"` + code + `","reasonCode":"` + code + `"}`
}

// serving answers fs.read from a log held here, a piece at a time as the
// host does: at most piece bytes from the offset asked, the whole size, and
// eof at the end.
func serving(log string, piece int) func(offset int64) string {
	return func(offset int64) string {
		end := min(int(offset)+piece, len(log))
		return fmt.Sprintf(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"data_b64":%q,"size":%d,"eof":%t}}`,
			base64.StdEncoding.EncodeToString([]byte(log[offset:end])), len(log), end == len(log))
	}
}

// answer is what log.read answered: a result's status, reason code and
// operation_result, or the error object in a result's place.
type answer struct {
	status, code string
	result       string
	errObj       string
}

// logRead calls log.read with arguments, the stand-in host answering every
// fs.read with fsRead's reply value (a result, or an error object when it
// carries "code") and settings.get with no values.
func logRead(t *testing.T, arguments string, fsRead func(offset int64) string) answer {
	t.Helper()
	return logReadSet(t, `{}`, arguments, fsRead)
}

// logReadSet is logRead with the operator's settings values.
func logReadSet(t *testing.T, values, arguments string, fsRead func(offset int64) string) answer {
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
	call := `{"jsonrpc":"2.0","id":"call","method":"invoke.call","params":{"operation":"log.read","arguments":` + arguments + `}}`
	if err := sdk.WriteFrame(in, []byte(call), sdk.MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
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
				Arguments struct{ Offset int64 }
			}
			Result struct {
				Status          string
				ReasonCode      string          `json:"reasonCode"`
				OperationResult json.RawMessage `json:"operation_result"`
			}
			Error json.RawMessage
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		if frame.Method == "" {
			return answer{status: frame.Result.Status, code: frame.Result.ReasonCode, result: string(frame.Result.OperationResult), errObj: string(frame.Error)}
		}
		var reply string
		switch frame.Params.Operation {
		case "settings.get":
			reply = `{"success":true,"ok":true,"status":"succeeded","operation_result":{"values":` + values + `}}`
		case "fs.read":
			reply = fsRead(frame.Params.Arguments.Offset)
		default:
			t.Fatalf("the plugin asked the host for %s", frame.Params.Operation)
		}
		member := `"result":`
		if strings.HasPrefix(reply, `{"code"`) {
			member = `"error":`
		}
		if err := sdk.WriteFrame(in, []byte(`{"jsonrpc":"2.0","id":`+string(frame.ID)+`,`+member+reply+`}`), sdk.MaxControlFrameBytes); err != nil {
			t.Fatal(err)
		}
	}
}

// AN ABSENT LOG IS AN EMPTY HISTORY, and it is the only thing that is: the
// host answers a file that is not there with FS_NOT_FOUND, and before any
// event has been recorded that is the truth about the log.
func TestAnAbsentLogIsAnEmptyHistory(t *testing.T) {
	got := logRead(t, `{}`, func(int64) string { return failed("FS_NOT_FOUND") })
	if got.status != "succeeded" || got.result != `{"lines":[],"total":0}` {
		t.Fatalf("an absent log read back as %+v, want a succeeded empty history", got)
	}
}

// A LOG THAT COULD NOT BE READ IS A FAILURE, NEVER AN EMPTY HISTORY.
// Every read error but a denial
// was answered {"lines": [], "total": 0}, succeeded: a disk that failed,
// and whatever else the host or the lane reported, told the identity that
// nothing had ever happened. Each is now the failure it was, in the host's
// own code where the host gave one.
func TestALogThatCouldNotBeReadIsAFailureNotAnEmptyHistory(t *testing.T) {
	for _, c := range []struct{ name, reply, status, code string }{
		{"the host's I/O failure", failed("FS_IO_FAILED"), "failed", "FS_IO_FAILED"},
		{"a failure code this plugin has never seen", failed("FS_SOMETHING_NEW"), "failed", "FS_SOMETHING_NEW"},
		{"a denial", `{"code":-32000,"message":"not granted here","data":{"reasonCode":"POLICY_DENY"}}`, "denied", "POLICY_DENY"},
		{"a reply that states no outcome", `{}`, "", ""},
	} {
		got := logRead(t, `{}`, func(int64) string { return c.reply })
		if got.status == "succeeded" || got.status != c.status || got.code != c.code {
			t.Errorf("%s: log.read answered %+v, want status %q code %q", c.name, got, c.status, c.code)
		}
		if c.status == "" && !strings.Contains(got.errObj, `"PLUGIN_HANDLER_FAILED"`) {
			t.Errorf("%s: log.read answered %+v, want the handler's failure in a result's place", c.name, got)
		}
	}
}

// AN OVERSIZED LOG IS NOT AN EMPTY HISTORY. log.event appends for as long
// as the plugin runs and log.read reads the log whole, under a ceiling;
// past the ceiling the read's error took the same path and the identity
// was told, succeeded, that its fullest log was empty. It is told instead
// that the log is too large for this operation to read, and that nothing
// was removed.
func TestAnOversizedLogIsNotAnEmptyHistory(t *testing.T) {
	line := "2026-01-02T03:04:05Z\ttool.called\ttool=search failed=false\n"
	log := strings.Repeat(line, defaultMaxReadBytes/len(line)+2)
	got := logRead(t, `{"lines":5}`, serving(log, 512<<10))
	if got.status != "failed" || got.code != "LOG_TOO_LARGE" {
		t.Fatalf("a log of %d bytes read back as status %q code %q result %.60s, want failed LOG_TOO_LARGE", len(log), got.status, got.code, got.result)
	}
}

// THE LIMIT IS THE OPERATOR'S. max_read_bytes moves it both ways: raised,
// the same log past the default reads back its recent lines and its whole
// count; lowered, a smaller log is refused, by the limit set.
func TestTheReadLimitIsTheOperatorsSetting(t *testing.T) {
	line := "2026-01-02T03:04:05Z\ttool.called\ttool=search failed=false\n"
	n := defaultMaxReadBytes/len(line) + 2
	log := strings.Repeat(line, n)
	got := logReadSet(t, `{"max_read_bytes":8388608}`, `{"lines":1}`, serving(log, 512<<10))
	want := fmt.Sprintf(`{"lines":[%q],"total":%d}`, strings.TrimSuffix(line, "\n"), n)
	if got.status != "succeeded" || got.result != want {
		t.Fatalf("with max_read_bytes raised, a log of %d bytes read back as status %q code %q result %.80s, want %.80s", len(log), got.status, got.code, got.result, want)
	}
	small := strings.Repeat(line, (64<<10)/len(line)+2)
	got = logReadSet(t, `{"max_read_bytes":65536}`, `{"lines":1}`, serving(small, 512<<10))
	if got.status != "failed" || got.code != "LOG_TOO_LARGE" {
		t.Fatalf("with max_read_bytes lowered, a log of %d bytes read back as status %q code %q, want failed LOG_TOO_LARGE", len(small), got.status, got.code)
	}
}

// A log within the ceiling still reads back its recent lines and its
// count, across the pieces the host hands it in.
func TestALogReadInPiecesReadsBackItsRecentLines(t *testing.T) {
	got := logRead(t, `{"lines":2}`, serving("one\ntwo\nthree\n", 5))
	if got.status != "succeeded" || got.result != `{"lines":["two","three"],"total":3}` {
		t.Fatalf("log.read answered %+v", got)
	}
}
