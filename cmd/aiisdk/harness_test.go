package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func decodeReplyObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %s", raw)
	}
	return m
}

// .
// .
// .
// .
func TestHarnessAnswersInTheBrokersVocabulary(t *testing.T) {
	put := json.RawMessage(`{"operation":"kv.put","target":{"key":"greeting"},"arguments":{"value":"hello"}}`)
	get := json.RawMessage(`{"operation":"kv.get","target":{"key":"greeting"}}`)

	bare, err := newHarness2(nil)
	if err != nil {
		t.Fatal(err)
	}
	if res, errObj := bare.answer("invoke.call", put); res != nil || !strings.Contains(string(errObj), `"reasonCode":"POLICY_DENY"`) {
		t.Fatalf("ungranted kv must be the audited denial, got %s / %s", res, errObj)
	}
	if _, errObj := bare.answer("observe.subscribe", nil); !strings.Contains(string(errObj), "POLICY_DENY") {
		t.Fatalf("a surface the harness has none of is denied, got %s", errObj)
	}

	granted, err := newHarness2([]string{"kv"})
	if err != nil {
		t.Fatal(err)
	}
	res, errObj := granted.answer("invoke.call", put)
	if errObj != nil {
		t.Fatalf("granted kv.put must succeed, got %s", errObj)
	}
	m := decodeReplyObject(t, res)
	if string(m["status"]) != `"succeeded"` || !strings.Contains(string(m["operation_result"]), `"stored":true`) {
		t.Fatalf("kv.put result must carry the broker's fields, got %s", res)
	}
	if !strings.Contains(string(m["external_receipt"]), `"host_authored":false`) {
		t.Fatalf("a harness receipt must say no host authored it, got %s", m["external_receipt"])
	}
	res, _ = granted.answer("invoke.call", get)
	if !strings.Contains(string(res), `"value":"hello"`) {
		t.Fatalf("kv.get must return the stored value, got %s", res)
	}
	res, _ = granted.answer("invoke.call", json.RawMessage(`{"operation":"kv.get","target":{"key":"absent"}}`))
	if !strings.Contains(string(res), `"KV_NOT_FOUND"`) || !strings.Contains(string(res), `"status":"failed"`) {
		t.Fatalf("a missing key is the broker's failed result, got %s", res)
	}
	if len(granted.observations) < 3 || !strings.Contains(granted.observations[0], "temporary") {
		t.Fatalf("every answer is observed, and storage is said to be temporary: %v", granted.observations)
	}
}

// .
// .
func TestHarnessRefusesAnUnknownGrant(t *testing.T) {
	if _, err := newHarness2([]string{"kv", "files"}); err == nil || !strings.Contains(err.Error(), `"files"`) {
		t.Fatalf("an unknown grant must be refused by name, got %v", err)
	}
	for _, bad := range []string{"net.outbound:", "net.outbound::443", "net.outbound:h:99999"} {
		if _, err := newHarness2([]string{bad}); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

// .
// .
func TestHarnessNetworkByScope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
		}
		fmt.Fprint(w, `{"weather":"aurora"}`)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	call := func(path string) json.RawMessage {
		return json.RawMessage(`{"operation":"http.get","target":{"url":"` + ts.URL + path + `"},"arguments":{}}`)
	}
	ungranted, _ := newHarness2([]string{"net.outbound:elsewhere.example.test:443"})
	if res, errObj := ungranted.answer("invoke.call", call("/x")); res != nil || !strings.Contains(string(errObj), "POLICY_DENY") {
		t.Fatalf("a host outside the granted scopes is denied, got %s / %s", res, errObj)
	}
	granted, _ := newHarness2([]string{"net.outbound:" + u.Hostname() + ":*"})
	res, errObj := granted.answer("invoke.call", call("/x"))
	if errObj != nil || !strings.Contains(string(res), `"http_status":200`) || !strings.Contains(string(res), `"weather":"aurora"`) {
		t.Fatalf("a granted fetch is performed and reported, got %s / %s", res, errObj)
	}
	res, _ = granted.answer("invoke.call", call("/missing"))
	if !strings.Contains(string(res), `"NET_REMOTE_OUTCOME_FAILED"`) || !strings.Contains(string(res), `"http_status":404`) {
		t.Fatalf("a remote failure is performed-but-failed with the evidence, got %s", res)
	}
	exact, _ := newHarness2([]string{"net.outbound:" + u.Hostname() + ":" + u.Port()})
	if _, errObj := exact.answer("invoke.call", call("/x")); errObj != nil {
		t.Fatalf("an exact host:port scope must admit its port, got %s", errObj)
	}
}

// .
// .
// .
func TestHarnessListsKeysAndEmbedsDeterministically(t *testing.T) {
	h, _ := newHarness2([]string{"kv", "embeddings"})
	for _, k := range []string{"m:2", "m:1", "x:1"} {
		h.answer("invoke.call", json.RawMessage(`{"operation":"kv.put","target":{"key":"`+k+`"},"arguments":{"value":"v"}}`))
	}
	res, errObj := h.answer("invoke.call", json.RawMessage(`{"operation":"kv.list","target":{"prefix":"m:"}}`))
	if errObj != nil || !strings.Contains(string(res), `"keys":["m:1","m:2"]`) {
		t.Fatalf("kv.list must list the prefixed keys sorted, got %s / %s", res, errObj)
	}
	res, errObj = h.answer("invoke.call", json.RawMessage(`{"operation":"embeddings.create","arguments":{"input":["Hello world","hello WORLD","other"]}}`))
	if errObj != nil || !strings.Contains(string(res), `"dimensions":32`) {
		t.Fatalf("embeddings must answer with 32 dimensions, got %s / %s", res, errObj)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(res, &m)
	var or struct {
		Vectors [][]float32 `json:"vectors"`
	}
	_ = json.Unmarshal(m["operation_result"], &or)
	if len(or.Vectors) != 3 || fmt.Sprint(or.Vectors[0]) != fmt.Sprint(or.Vectors[1]) || fmt.Sprint(or.Vectors[0]) == fmt.Sprint(or.Vectors[2]) {
		t.Fatalf("the stand-in is deterministic and case-insensitive by word: %v", or.Vectors)
	}
	var norm float64
	for _, x := range or.Vectors[0] {
		norm += float64(x * x)
	}
	if norm < 0.99 || norm > 1.01 {
		t.Fatalf("vectors are unit length, got %v", norm)
	}
	bare, _ := newHarness2([]string{"kv"})
	if _, errObj := bare.answer("invoke.call", json.RawMessage(`{"operation":"embeddings.create","arguments":{"input":["x"]}}`)); !strings.Contains(string(errObj), "POLICY_DENY") {
		t.Fatalf("ungranted embeddings must be denied, got %s", errObj)
	}
}

// .
func newHarness2(grants []string) (*harness, error) { return newHarness(grants, nil, nil) }

// .
// .
// .
// .
func TestHarnessAnswersSettingsFromTheDeclaration(t *testing.T) {
	min, max := 1.0, 20.0
	decls := []aiiospkg.SettingDecl{
		{Key: "recall_limit", Type: "number", Title: "Recall limit", Default: 3.0, Minimum: &min, Maximum: &max},
		{Key: "verbose", Type: "boolean", Title: "Verbose"},
		{Key: "api_key", Type: "secret", Title: "API key"},
	}
	if _, err := newHarness(nil, decls, []string{"colour=blue"}); err == nil || !strings.Contains(err.Error(), "declares no setting") {
		t.Fatalf("an undeclared -setting is refused by name: %v", err)
	}
	if _, err := newHarness(nil, decls, []string{"recall_limit=99"}); err == nil || !strings.Contains(err.Error(), "above the maximum") {
		t.Fatalf("a -setting outside its declaration is refused: %v", err)
	}
	if _, err := newHarness(nil, decls, []string{"verbose=maybe"}); err == nil {
		t.Fatal("a boolean -setting must be true or false")
	}
	h, err := newHarness(nil, decls, []string{"recall_limit=7", "verbose=true", "api_key=acme"})
	if err != nil {
		t.Fatal(err)
	}
	ask := func() string {
		res, deny := h.answer("invoke.call", json.RawMessage(`{"operation":"settings.get"}`))
		if deny != nil {
			t.Fatalf("settings.get denied: %s", deny)
		}
		return string(res)
	}
	if out := ask(); !strings.Contains(out, `"values":{"api_key":"acme","recall_limit":7,"verbose":true}`) {
		t.Fatalf("the run's values, typed: %s", out)
	}
	h.caseSettings = map[string]interface{}{"recall_limit": 1.0}
	if out := ask(); !strings.Contains(out, `"recall_limit":1`) {
		t.Fatalf("a case's settings override the run's: %s", out)
	}
	h.caseSettings = nil
	if _, err := caseSettingValues(decls, map[string]json.RawMessage{"colour": json.RawMessage(`"blue"`)}); err == nil {
		t.Fatal("a case naming an undeclared setting fails")
	}
	vals, err := caseSettingValues(decls, map[string]json.RawMessage{"recall_limit": json.RawMessage(`2`), "verbose": json.RawMessage(`null`)})
	if err != nil || vals["recall_limit"] != 2.0 || len(vals) != 1 {
		t.Fatalf("case values typed, null dropped: %v %v", vals, err)
	}
	bare, _ := newHarness(nil, nil, nil)
	if res, _ := bare.answer("invoke.call", json.RawMessage(`{"operation":"settings.get"}`)); !strings.Contains(string(res), `"values":{}`) {
		t.Fatalf("no declaration reads as an empty object: %s", res)
	}
}

// .
// .
// .
// .
// .
func TestHarnessAnswersVerbsAndStreams(t *testing.T) {
	var seen struct{ method, body, ctype, accept string }
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen.method, seen.body, seen.ctype, seen.accept = r.Method, string(b), r.Header.Get("Content-Type"), r.Header.Get("Accept")
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/stream":
			f := w.(http.Flusher)
			for i := 1; i <= 3; i++ {
				fmt.Fprintf(w, "chunk %d;", i)
				f.Flush()
				time.Sleep(10 * time.Millisecond)
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		}
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	h, err := newHarness([]string{"net.outbound:" + u.Host}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(params string) (string, string) {
		res, deny := h.answer("invoke.call", json.RawMessage(params))
		return string(res), string(deny)
	}
	res, deny := call(fmt.Sprintf(`{"operation":"http.post","target":{"url":%q},"arguments":{"body":"{\"title\":\"x\"}","content_type":"application/json","headers":{"Accept":"application/vnd.github+json"}}}`, ts.URL+"/issues"))
	if deny != "" || !strings.Contains(res, `"http_status":200`) || seen.method != "POST" || seen.body != `{"title":"x"}` || seen.ctype != "application/json" || seen.accept != "application/vnd.github+json" {
		t.Fatalf("post: %s %s seen %+v", res, deny, seen)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.delete","target":{"url":%q}}`, ts.URL+"/issues/1"))
	if seen.method != "DELETE" || !strings.Contains(res, `"http_status":200`) {
		t.Fatalf("delete: %s", res)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.post","target":{"url":%q},"arguments":{"body":"x","content_type":"text/plain"}}`, ts.URL+"/moved"))
	if !strings.Contains(res, `"http_status":302`) || !strings.Contains(res, `"location":"/final"`) {
		t.Fatalf("a mutation's redirect is reported: %s", res)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.post","target":{"url":%q},"arguments":{"body":"x","content_type":"text/plain","follow_redirects":true}}`, ts.URL+"/moved"))
	if !strings.Contains(res, `"http_status":200`) {
		t.Fatalf("asked, it follows: %s", res)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.get","target":{"url":%q},"arguments":{"body":"x","content_type":"text/plain"}}`, ts.URL))
	if !strings.Contains(res, "NET_UNKNOWN_ARGUMENT") {
		t.Fatalf("a body on a read is refused: %s", res)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.get","target":{"url":%q},"arguments":{"stream":true}}`, ts.URL+"/stream"))
	var head struct {
		OperationResult struct {
			StreamID string `json:"stream_id"`
			Status   int    `json:"http_status"`
		} `json:"operation_result"`
	}
	_ = json.Unmarshal([]byte(res), &head)
	if head.OperationResult.StreamID == "" || head.OperationResult.Status != 200 {
		t.Fatalf("stream head: %s", res)
	}
	var text string
	var seqs []int
	for i := 0; i < 10; i++ {
		res, deny = call(fmt.Sprintf(`{"operation":"http.read","target":{"stream_id":%q}}`, head.OperationResult.StreamID))
		if deny != "" {
			t.Fatalf("read denied: %s", deny)
		}
		var chunk struct {
			OperationResult struct {
				Seq     int    `json:"seq"`
				DataB64 string `json:"data_b64"`
				Done    bool   `json:"done"`
				Bytes   int    `json:"bytes"`
			} `json:"operation_result"`
		}
		_ = json.Unmarshal([]byte(res), &chunk)
		data, _ := base64.StdEncoding.DecodeString(chunk.OperationResult.DataB64)
		text += string(data)
		if chunk.OperationResult.Bytes > 0 {
			seqs = append(seqs, chunk.OperationResult.Seq)
		}
		if chunk.OperationResult.Done {
			break
		}
	}
	if text != "chunk 1;chunk 2;chunk 3;" {
		t.Fatalf("chunks reassemble: %q", text)
	}
	for i, s := range seqs {
		if s != i+1 {
			t.Fatalf("numbered in order: %v", seqs)
		}
	}
	if _, deny = call(fmt.Sprintf(`{"operation":"http.read","target":{"stream_id":%q}}`, head.OperationResult.StreamID)); !strings.Contains(deny, "no open stream") {
		t.Fatalf("a finished stream is gone: %s", deny)
	}
	res, _ = call(fmt.Sprintf(`{"operation":"http.get","target":{"url":%q},"arguments":{"stream":true}}`, ts.URL+"/stream"))
	_ = json.Unmarshal([]byte(res), &head)
	res, _ = call(fmt.Sprintf(`{"operation":"http.close","target":{"stream_id":%q}}`, head.OperationResult.StreamID))
	if !strings.Contains(res, `"closed":true`) {
		t.Fatalf("close: %s", res)
	}
}

// .
// .
// .
// .
func TestHarnessAnswersFilesUnderTheHostsRules(t *testing.T) {
	docs := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "notes.md"), []byte("# notes\nhello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(docs, "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	h, err := newHarness([]string{"root:docs=" + docs, "root:out=" + outside + ":rw"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.closeFiles()
	call := func(params string) (string, string) {
		res, deny := h.answer("invoke.call", json.RawMessage(params))
		return string(res), string(deny)
	}
	res, deny := call(`{"operation":"fs.read","target":{"root":"docs","path":"notes.md"}}`)
	if deny != "" || !strings.Contains(res, base64.StdEncoding.EncodeToString([]byte("# notes\nhello"))) {
		t.Fatalf("read a granted file: %s %s", res, deny)
	}
	if _, deny = call(`{"operation":"fs.write","target":{"root":"docs","path":"x"},"arguments":{"data":"x"}}`); !strings.Contains(deny, "FS_READ_ONLY") {
		t.Fatalf("read-only: %s", deny)
	}
	if _, deny = call(`{"operation":"fs.read","target":{"root":"docs","path":"link"}}`); !strings.Contains(deny, "FS_SYMLINK_REFUSED") {
		t.Fatalf("a symlink is refused, not followed: %s", deny)
	}
	if _, deny = call(`{"operation":"fs.read","target":{"root":"docs","path":"../secret"}}`); !strings.Contains(deny, "FS_PATH_INVALID") {
		t.Fatalf("traversal: %s", deny)
	}
	if _, deny = call(`{"operation":"fs.read","target":{"root":"nope","path":"x"}}`); !strings.Contains(deny, "no harness grant names root") {
		t.Fatalf("an ungranted root is denied by name: %s", deny)
	}
	res, deny = call(`{"operation":"fs.write","target":{"root":"private","path":"a/b.txt"},"arguments":{"data":"one"}}`)
	if deny != "" || !strings.Contains(res, `"bytes":3`) {
		t.Fatalf("private write: %s %s", res, deny)
	}
	if h.privateDir == "" {
		t.Fatal("the private directory exists for the run")
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(filepath.Join(h.privateDir, "a", "b.txt")); err != nil || fi.Mode().Perm()&0o111 != 0 {
			t.Fatalf("never executable: %v %v", fi, err)
		}
	}
	res, _ = call(`{"operation":"fs.write","target":{"root":"private","path":"a/b.txt"},"arguments":{"data":" two","append":true}}`)
	if !strings.Contains(res, `"size":7`) {
		t.Fatalf("append: %s", res)
	}
	res, _ = call(`{"operation":"fs.list","target":{"root":"private","path":"a"}}`)
	if !strings.Contains(res, `"name":"b.txt"`) {
		t.Fatalf("list: %s", res)
	}
	res, _ = call(`{"operation":"fs.write","target":{"root":"out","path":"w.txt"},"arguments":{"data":"w"}}`)
	if !strings.Contains(res, `"bytes":1`) {
		t.Fatalf("a :rw root accepts a write: %s", res)
	}
	res, _ = call(`{"operation":"fs.delete","target":{"root":"out","path":"w.txt"}}`)
	if !strings.Contains(res, `"deleted":true`) {
		t.Fatalf("delete: %s", res)
	}
	private := h.privateDir
	h.closeFiles()
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatal("the private directory dies with the run")
	}
}

// .
// .
func TestHarnessAnswersToolPublication(t *testing.T) {
	h, _ := newHarness(nil, nil, nil)
	h.pluginID = "com.example.mcp"
	if _, deny := h.answer("invoke.call", json.RawMessage(`{"operation":"tools.publish","arguments":{"name":"server.list","summary":"s","effects":"read.external","capabilities":[]}}`)); !strings.Contains(string(deny), "-grant tools") {
		t.Fatalf("without the grant: %s", deny)
	}
	h, _ = newHarness([]string{"tools"}, nil, nil)
	h.pluginID = "com.example.mcp"
	res, deny := h.answer("invoke.call", json.RawMessage(`{"operation":"tools.publish","arguments":{"name":"server.list","summary":"s","effects":"read.external","capabilities":[]}}`))
	if len(deny) != 0 || !strings.Contains(string(res), `"tool":"pl_com_example_mcp_dyn_server_list"`) {
		t.Fatalf("published: %s %s", res, deny)
	}
	if res, _ := h.answer("invoke.call", json.RawMessage(`{"operation":"tools.publish","arguments":{"name":"Server List","summary":"s","effects":"read.external"}}`)); !strings.Contains(string(res), "OPERATION_ARGUMENT_INVALID") {
		t.Fatalf("a bad name: %s", res)
	}
	if res, _ := h.answer("invoke.call", json.RawMessage(`{"operation":"tools.withdraw","target":{"name":"server.list"}}`)); !strings.Contains(string(res), `"withdrawn":true`) {
		t.Fatalf("withdraw: %s", res)
	}
	if res, _ := h.answer("invoke.call", json.RawMessage(`{"operation":"tools.withdraw","target":{"name":"server.list"}}`)); !strings.Contains(string(res), "OPERATION_TARGET_INVALID") {
		t.Fatalf("withdrawing twice: %s", res)
	}
}

// .
// .
// .
func TestHarnessPublishesWholeOrNothing(t *testing.T) {
	h, err := newHarness(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.closeFiles()
	call := func(op, path, args string) (string, string) {
		res, deny := h.answer("invoke.call", json.RawMessage(`{"operation":"`+op+`","target":{"root":"private","path":"`+path+`"},"arguments":`+args+`}`))
		return string(res), string(deny)
	}
	res, deny := call("fs.publish", "snap.json", `{"data":"one","expected_absent":true}`)
	if deny != "" || !strings.Contains(res, `"status":"succeeded"`) || !strings.Contains(res, `"replaced":false`) {
		t.Fatalf("first publication: %s %s", res, deny)
	}
	var first struct {
		OperationResult struct {
			SHA256 string `json:"sha256"`
		} `json:"operation_result"`
	}
	_ = json.Unmarshal([]byte(res), &first)
	res, _ = call("fs.read", "snap.json", `{}`)
	if !strings.Contains(res, `"sha256":"`+first.OperationResult.SHA256+`"`) {
		t.Fatalf("a whole read carries the digest: %s", res)
	}
	res, _ = call("fs.publish", "snap.json", `{"data":"two","expected_sha256":"`+first.OperationResult.SHA256+`"}`)
	if !strings.Contains(res, `"replaced":true`) {
		t.Fatalf("compare-and-replace: %s", res)
	}
	res, _ = call("fs.publish", "snap.json", `{"data":"stale","expected_sha256":"`+first.OperationResult.SHA256+`"}`)
	if !strings.Contains(res, "FS_GENERATION_MISMATCH") {
		t.Fatalf("a stale expectation is refused: %s", res)
	}
	if got, _ := os.ReadFile(filepath.Join(h.privateDir, "snap.json")); string(got) != "two" {
		t.Fatalf("a refusal leaves the file: %q", got)
	}
	call("fs.write", ".staging/snap.json", `{"data":"thr","append":true}`)
	call("fs.write", ".staging/snap.json", `{"data":"ee","append":true}`)
	sum := sha256.Sum256([]byte("three"))
	res, _ = call("fs.publish", "snap.json", `{"from":".staging/snap.json","sha256":"`+hex.EncodeToString(sum[:])+`"}`)
	if !strings.Contains(res, `"status":"succeeded"`) {
		t.Fatalf("staged publication: %s", res)
	}
	if got, _ := os.ReadFile(filepath.Join(h.privateDir, "snap.json")); string(got) != "three" {
		t.Fatalf("the staged bytes took the name: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.privateDir, ".staging", "snap.json")); !os.IsNotExist(err) {
		t.Fatal("the staged file was moved")
	}
	res, _ = call("fs.publish", "snap.json", `{"from":".staging/snap.json","sha256":"`+hex.EncodeToString(sum[:])+`"}`)
	if !strings.Contains(res, "FS_NOT_FOUND") {
		t.Fatalf("no staged file: %s", res)
	}
}

// .
// .
// .
func TestHarnessLocalNetworkByScope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"state":"on"}`)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	call := json.RawMessage(`{"operation":"http.get","target":{"url":"` + ts.URL + `/api"},"arguments":{}}`)
	for _, bad := range []string{"net.local:8.8.8.8:53", "net.local:1.0.0.0/8", "net.local:169.254.169.254", "net.local:", "net.local:dev.local:70000"} {
		if _, err := newHarness2([]string{bad}); err == nil {
			t.Fatalf("%q must be refused: not a device on the local network", bad)
		}
	}
	for _, grant := range []string{"net.local:" + u.Hostname() + ":" + u.Port(), "net.local:127.0.0.0/8:*", "net.local:" + u.Hostname()} {
		h, err := newHarness2([]string{grant})
		if err != nil {
			t.Fatalf("%q: %v", grant, err)
		}
		if res, errObj := h.answer("invoke.call", call); errObj != nil || !strings.Contains(string(res), `"state":"on"`) {
			t.Fatalf("%q admits the device, got %s / %s", grant, res, errObj)
		}
	}
	other, _ := newHarness2([]string{"net.local:" + u.Hostname() + ":1"})
	if res, errObj := other.answer("invoke.call", call); res != nil || !strings.Contains(string(errObj), "net.local:") {
		t.Fatalf("another port is denied and the denial names the local grant, got %s / %s", res, errObj)
	}
}
