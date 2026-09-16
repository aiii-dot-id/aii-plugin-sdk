package main

// .
// .
// .
// .
// .
// .
// .

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const (
	harnessHTTPTimeout = 10 * time.Second
	harnessMaxBody     = 768 << 10
)

type harness struct {
	pluginID     string
	kvGranted    bool
	voiceGranted bool
	embedGranted bool
	memGranted   bool
	hosts        []hostScope
	// .
	// .
	// .
	local []localScope
	kv    map[string]string
	// .
	// .
	memories     []*harnessMemory
	memoryN      int
	observations []string
	client       *http.Client

	// .
	// .
	// .
	decls        []aiiospkg.SettingDecl
	runSettings  map[string]interface{}
	caseSettings map[string]interface{}

	// .
	// .
	streams map[string]*harnessStream
	streamN int

	// .
	// .
	toolsGranted bool
	published    map[string]bool

	// .
	// .
	// .
	// .
	privateDir string
	roots      map[string]harnessRoot
}

type harnessRoot struct {
	path  string
	write bool
}

type harnessStream struct {
	resp  *http.Response
	seq   int
	total int
}

// .
// .
type hostScope struct {
	host    string
	port    int
	anyPort bool
}

func parseHostScope(s string) (hostScope, error) {
	if s == "" {
		return hostScope{}, fmt.Errorf("empty host scope")
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return hostScope{host: strings.ToLower(s)}, nil
	}
	host, port := strings.ToLower(s[:i]), s[i+1:]
	if host == "" {
		return hostScope{}, fmt.Errorf("scope %q names no host", s)
	}
	if port == "*" {
		return hostScope{host: host, anyPort: true}, nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return hostScope{}, fmt.Errorf("scope %q has no valid port", s)
	}
	return hostScope{host: host, port: n}, nil
}

func (s hostScope) matches(host string, port int) bool {
	if !strings.EqualFold(s.host, host) {
		return false
	}
	if s.anyPort {
		return true
	}
	if s.port == 0 {
		return port == 443 || port == 80
	}
	return s.port == port
}

// .
// .
// .
// .
// .
func newHarness(grants []string, decls []aiiospkg.SettingDecl, settings []string) (*harness, error) {
	h := &harness{kv: map[string]string{}, client: &http.Client{Timeout: harnessHTTPTimeout}, decls: decls, runSettings: map[string]interface{}{}, roots: map[string]harnessRoot{}}
	for _, s := range settings {
		key, raw, ok := strings.Cut(s, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("-setting %q: want key=value", s)
		}
		v, err := settingValue(decls, key, raw)
		if err != nil {
			return nil, fmt.Errorf("-setting %q: %v", s, err)
		}
		h.runSettings[key] = v
	}
	for _, g := range grants {
		switch {
		case g == "kv":
			h.kvGranted = true
		case g == "voice":
			h.voiceGranted = true
		case g == "embeddings":
			h.embedGranted = true
		case g == "memory":
			h.memGranted = true
		case g == "tools":
			h.toolsGranted = true
		case strings.HasPrefix(g, "net.outbound:"):
			sc, err := parseHostScope(strings.TrimPrefix(g, "net.outbound:"))
			if err != nil {
				return nil, fmt.Errorf("-grant %q: %v", g, err)
			}
			h.hosts = append(h.hosts, sc)
		case strings.HasPrefix(g, "net.local:"):
			sc, err := parseLocalScope(strings.TrimPrefix(g, "net.local:"))
			if err != nil {
				return nil, fmt.Errorf("-grant %q: %v", g, err)
			}
			h.local = append(h.local, sc)
		case strings.HasPrefix(g, "root:"):
			// .
			spec := strings.TrimPrefix(g, "root:")
			name, rest, ok := strings.Cut(spec, "=")
			if !ok || name == "" || rest == "" {
				return nil, fmt.Errorf("-grant %q: want root:<name>=<path>[:rw]", g)
			}
			write := false
			if strings.HasSuffix(rest, ":rw") {
				write, rest = true, strings.TrimSuffix(rest, ":rw")
			}
			abs, err := filepath.Abs(rest)
			if err != nil {
				return nil, fmt.Errorf("-grant %q: %v", g, err)
			}
			if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
				return nil, fmt.Errorf("-grant %q: %s is not a directory", g, abs)
			}
			h.roots[name] = harnessRoot{path: abs, write: write}
		default:
			return nil, fmt.Errorf("-grant %q is not a grant this harness knows (kv, voice, embeddings, memory, tools, net.outbound:host[:port|:*], net.local:<address|range|name>[:port|:*], root:<name>=<path>[:rw])", g)
		}
	}
	return h, nil
}

func (h *harness) observe(format string, args ...interface{}) {
	h.observations = append(h.observations, fmt.Sprintf(format, args...))
}

// .
const harnessReceipt = `{"host_authored":false,"harness":"aiisdk test","note":"a local harness observation, not a receipt"}`

func deny(message, reason string) json.RawMessage {
	raw, _ := json.Marshal(map[string]interface{}{
		"code": -32000, "message": message,
		"data": map[string]string{"reasonCode": reason, "denied_at": "capability_evaluation"},
	})
	return raw
}

func succeeded(operationResult interface{}) json.RawMessage {
	or, _ := json.Marshal(operationResult)
	return json.RawMessage(`{"success":true,"ok":true,"status":"succeeded","operation_result":` + string(or) + `,"external_receipt":` + harnessReceipt + `}`)
}

func failed(reason string, operationResult interface{}) json.RawMessage {
	or := []byte("null")
	if operationResult != nil {
		or, _ = json.Marshal(operationResult)
	}
	q, _ := json.Marshal(reason)
	return json.RawMessage(`{"success":false,"ok":false,"status":"failed","reason":` + string(q) + `,"reasonCode":` + string(q) + `,"reason_code":` + string(q) + `,"operation_result":` + string(or) + `,"external_receipt":` + harnessReceipt + `}`)
}

// .
func (h *harness) answer(method string, params json.RawMessage) (json.RawMessage, json.RawMessage) {
	if method != "invoke.call" {
		h.observe("%s -> denied (no such surface in the harness)", method)
		return nil, deny(fmt.Sprintf("no %s surface in the harness; denied", method), "POLICY_DENY")
	}
	var p struct {
		Operation string          `json:"operation"`
		Target    json.RawMessage `json:"target"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Operation == "" {
		return nil, json.RawMessage(`{"code":-32602,"message":"operation (string) required"}`)
	}
	switch p.Operation {
	case "settings.get":
		values := map[string]interface{}{}
		for k, v := range h.runSettings {
			values[k] = v
		}
		for k, v := range h.caseSettings {
			values[k] = v
		}
		effective := aiiospkg.EffectiveSettings(h.decls, values)
		h.observe("settings.get -> %d value(s) (defaults under -setting and the case's settings)", len(effective))
		return succeeded(map[string]interface{}{"values": effective}), nil
	case "kv.put", "kv.get", "kv.delete", "kv.list":
		return h.answerKV(p.Operation, p.Target, p.Arguments)
	case "embeddings.create":
		return h.answerEmbeddings(p.Arguments)
	case "memory.remember", "memory.recall":
		return h.answerMemory(p.Operation, p.Arguments)
	case "http.get", "http.post", "http.put", "http.patch", "http.delete":
		return h.answerHTTP(p.Operation, p.Target, p.Arguments)
	case "http.read":
		return h.answerHTTPRead(p.Target, p.Arguments)
	case "http.close":
		return h.answerHTTPClose(p.Target)
	case "fs.list", "fs.read", "fs.write", "fs.delete", "fs.publish":
		return h.answerFS(p.Operation, p.Target, p.Arguments)
	case "tools.publish", "tools.withdraw":
		return h.answerTools(p.Operation, p.Target, p.Arguments)
	case "voice.observe":
		if !h.voiceGranted {
			h.observe("voice.observe -> denied (run with -grant voice)")
			return nil, deny("voice is not granted in this harness run (aiisdk test -grant voice)", "POLICY_DENY")
		}
		h.observe("voice.observe -> accepted (the harness records nothing)")
		return succeeded(map[string]bool{"accepted": true}), nil
	}
	h.observe("%s -> denied (no such operation)", p.Operation)
	return nil, deny(fmt.Sprintf("operation %q is not allowed for any capability this harness serves", p.Operation), "OPERATION_NOT_ALLOWED")
}

// .
// .
// .
func settingValue(decls []aiiospkg.SettingDecl, key, raw string) (interface{}, error) {
	for _, d := range decls {
		if d.Key != key {
			continue
		}
		var v interface{} = raw
		switch d.Type {
		case aiiospkg.SettingNumber, aiiospkg.SettingInteger:
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, fmt.Errorf("%s is a number", key)
			}
			v = f
		case aiiospkg.SettingBoolean:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("%s is true or false", key)
			}
			v = b
		}
		if err := aiiospkg.CheckSettingValue(d, v); err != nil {
			return nil, fmt.Errorf("%s %v", key, err)
		}
		return v, nil
	}
	keys := make([]string, 0, len(decls))
	for _, d := range decls {
		keys = append(keys, d.Key)
	}
	return nil, fmt.Errorf("plugin.json declares no setting %q (declared: %s)", key, strings.Join(keys, ", "))
}

// .
// .
func caseSettingValues(decls []aiiospkg.SettingDecl, raw map[string]json.RawMessage) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	for key, rv := range raw {
		var d *aiiospkg.SettingDecl
		for i := range decls {
			if decls[i].Key == key {
				d = &decls[i]
			}
		}
		if d == nil {
			return nil, fmt.Errorf("plugin.json declares no setting %q", key)
		}
		var v interface{}
		if err := json.Unmarshal(rv, &v); err != nil {
			return nil, fmt.Errorf("setting %q: %v", key, err)
		}
		if v == nil {
			continue
		}
		if err := aiiospkg.CheckSettingValue(*d, v); err != nil {
			return nil, fmt.Errorf("setting %q %v", key, err)
		}
		out[key] = v
	}
	return out, nil
}

func (h *harness) answerKV(op string, target, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	if !h.kvGranted {
		h.observe("%s -> denied (run with -grant kv)", op)
		return nil, deny("kv is not granted in this harness run (aiisdk test -grant kv)", "POLICY_DENY")
	}
	if op == "kv.list" {
		var lt struct {
			Prefix string `json:"prefix"`
		}
		_ = json.Unmarshal(target, &lt)
		var la struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(arguments, &la)
		limit := 256
		if la.Limit > 0 && la.Limit < limit {
			limit = la.Limit
		}
		keys := make([]string, 0, len(h.kv))
		for k := range h.kv {
			if strings.HasPrefix(k, lt.Prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		truncated := len(keys) > limit
		if truncated {
			keys = keys[:limit]
		}
		h.observe("kv.list %q -> %d keys (temporary store)", lt.Prefix, len(keys))
		return succeeded(map[string]interface{}{"prefix": lt.Prefix, "keys": keys, "truncated": truncated}), nil
	}
	var t struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(target, &t)
	if t.Key == "" {
		return failed("KV_KEY_INVALID", nil), nil
	}
	switch op {
	case "kv.put":
		var a struct {
			Value *string `json:"value"`
		}
		_ = json.Unmarshal(arguments, &a)
		if a.Value == nil {
			return failed("KV_VALUE_INVALID", nil), nil
		}
		h.kv[t.Key] = *a.Value
		h.observe("kv.put %s -> stored (%d bytes; temporary, dies with this run)", t.Key, len(*a.Value))
		return succeeded(map[string]interface{}{"stored": true, "key": t.Key, "value_bytes": len(*a.Value), "scope": "harness"}), nil
	case "kv.get":
		v, ok := h.kv[t.Key]
		if !ok {
			h.observe("kv.get %s -> not found", t.Key)
			return failed("KV_NOT_FOUND", nil), nil
		}
		h.observe("kv.get %s -> found", t.Key)
		return succeeded(map[string]string{"key": t.Key, "value": v}), nil
	default:
		_, had := h.kv[t.Key]
		delete(h.kv, t.Key)
		h.observe("kv.delete %s -> deleted=%v", t.Key, had)
		return succeeded(map[string]interface{}{"deleted": had, "key": t.Key}), nil
	}
}

func (h *harness) answerHTTP(op string, target, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	method := map[string]string{"http.get": "GET", "http.post": "POST", "http.put": "PUT", "http.patch": "PATCH", "http.delete": "DELETE"}[op]
	var t struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(target, &t)
	u, err := url.Parse(t.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return failed("NET_TARGET_INVALID", nil), nil
	}
	port := 443
	if u.Scheme == "http" {
		port = 80
	}
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	granted := false
	for _, sc := range h.hosts {
		if sc.matches(u.Hostname(), port) {
			granted = true
			break
		}
	}
	for _, sc := range h.local {
		if !granted && sc.matches(u.Hostname(), port) {
			granted = true
		}
	}
	if !granted {
		h.observe("%s %s -> denied (run with -grant net.outbound:%s:%d, or -grant net.local:%s:%d for a device on your own network)", op, t.URL, u.Hostname(), port, u.Hostname(), port)
		return nil, deny(fmt.Sprintf("no harness grant covers %s:%d (aiisdk test -grant net.outbound:%s:%d, or -grant net.local:%s:%d for a device on your own network)", u.Hostname(), port, u.Hostname(), port, u.Hostname(), port), "POLICY_DENY")
	}
	// .
	// .
	// .
	var args struct {
		TimeoutMS       int               `json:"timeout_ms"`
		AuthProfile     string            `json:"auth_profile"`
		Body            *string           `json:"body"`
		ContentType     string            `json:"content_type"`
		Headers         map[string]string `json:"headers"`
		FollowRedirects *bool             `json:"follow_redirects"`
		Idempotent      bool              `json:"idempotent"`
		Stream          bool              `json:"stream"`
	}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
	}
	if args.Body != nil && method != "POST" && method != "PUT" && method != "PATCH" {
		h.observe("%s %s -> denied: body is not an argument of %s", op, t.URL, op)
		return failed("NET_UNKNOWN_ARGUMENT", nil), nil
	}
	if (args.Body != nil) != (args.ContentType != "") {
		h.observe("%s %s -> denied: body and content_type go together", op, t.URL)
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if args.AuthProfile != "" {
		h.observe("%s %s -> auth_profile %q named; the harness holds no credential and sends none (the host's broker injects it at T2)", op, t.URL, args.AuthProfile)
	}
	var body io.Reader
	if args.Body != nil {
		body = bytes.NewReader([]byte(*args.Body))
	}
	req, err := http.NewRequest(method, t.URL, body)
	if err != nil {
		return failed("NET_TARGET_INVALID", nil), nil
	}
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}
	if args.ContentType != "" {
		req.Header.Set("Content-Type", args.ContentType)
	}
	req.Header.Set("User-Agent", "AII-OS/1.0 (aiisdk test)")
	c := *h.client
	if args.TimeoutMS > 0 {
		c.Timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	follow := method == "GET"
	if args.FollowRedirects != nil {
		follow = *args.FollowRedirects
	}
	if !follow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	resp, err := c.Do(req)
	if err != nil {
		h.observe("%s %s -> transport failed: %v (the host's receipt would say whether the effect is unknown or not performed)", op, t.URL, err)
		return failed("NET_REMOTE_OUTCOME_FAILED", nil), nil
	}
	if args.Stream && resp.StatusCode < 400 {
		h.streamN++
		id := fmt.Sprintf("s%d", h.streamN)
		if h.streams == nil {
			h.streams = map[string]*harnessStream{}
		}
		h.streams[id] = &harnessStream{resp: resp}
		or := map[string]interface{}{"stream_id": id, "http_status": resp.StatusCode, "chunk_max_bytes": harnessChunkMax, "stream_max_bytes": harnessMaxBody}
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			or["content_type"] = ct
		}
		if loc := resp.Header.Get("Location"); loc != "" {
			or["location"] = loc
		}
		h.observe("%s %s -> %d, streamed as %s", op, t.URL, resp.StatusCode, id)
		return succeeded(or), nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, harnessMaxBody))
	var parsed interface{}
	var bodyValue interface{} = string(data)
	if json.Unmarshal(data, &parsed) == nil {
		bodyValue = parsed
	}
	or := map[string]interface{}{"http_status": resp.StatusCode, "body": bodyValue}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		or["content_type"] = ct
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		or["location"] = loc
	}
	h.observe("%s %s -> %d (%d bytes)", op, t.URL, resp.StatusCode, len(data))
	if resp.StatusCode >= 400 {
		return failed("NET_REMOTE_OUTCOME_FAILED", or), nil
	}
	return succeeded(or), nil
}

// .

// .
// .
// .
// .
func (h *harness) answerTools(op string, target, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	if !h.toolsGranted {
		h.observe("%s -> denied (run with -grant tools)", op)
		return nil, deny("publishing tools is not granted in this harness run (aiisdk test -grant tools)", "POLICY_DENY")
	}
	if h.published == nil {
		h.published = map[string]bool{}
	}
	if op == "tools.withdraw" {
		var t struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(target, &t)
		if !h.published[t.Name] {
			return failed("OPERATION_TARGET_INVALID", nil), nil
		}
		delete(h.published, t.Name)
		h.observe("tools.withdraw %s -> withdrawn", t.Name)
		return succeeded(map[string]interface{}{"name": t.Name, "withdrawn": true}), nil
	}
	var spec struct {
		Name         string   `json:"name"`
		Summary      string   `json:"summary"`
		Effects      string   `json:"effects"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(arguments, &spec); err != nil || spec.Name == "" || spec.Summary == "" {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if !rePublishedName.MatchString(spec.Name) || len(spec.Name) > 64 {
		h.observe("tools.publish %q -> denied: not lowercase dotted segments", spec.Name)
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	switch spec.Effects {
	case "read.internal", "read.external", "write.local", "write.external", "exec":
	default:
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if h.published[spec.Name] {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if len(h.published) >= 32 {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	h.published[spec.Name] = true
	registry := "pl_" + strings.NewReplacer(".", "_", "-", "_").Replace(h.pluginID) + "_dyn_" + strings.ReplaceAll(spec.Name, ".", "_")
	h.observe("tools.publish %s -> %s (the host would hold its capabilities %v to the envelope; the harness has none)", spec.Name, registry, spec.Capabilities)
	return succeeded(map[string]interface{}{"name": spec.Name, "tool": registry, "published": true}), nil
}

var rePublishedName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// .

const (
	harnessFilesMax  = 64 << 20
	harnessReadMax   = 768 << 10
	harnessWriteMax  = 256 << 10
	harnessListMax   = 1024
	harnessPathBytes = 512
	harnessPathDepth = 16
)

// .
// .
func cleanRel(p string) (string, error) {
	if len(p) > harnessPathBytes {
		return "", fmt.Errorf("path over %d bytes", harnessPathBytes)
	}
	if strings.ContainsAny(p, "\x00\\") {
		return "", fmt.Errorf("path carries a NUL or a backslash")
	}
	if p == "" || p == "." {
		return ".", nil
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("path must be relative to the root")
	}
	parts := strings.Split(p, "/")
	if len(parts) > harnessPathDepth {
		return "", fmt.Errorf("path deeper than %d", harnessPathDepth)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path carries an empty, . or .. component")
		}
	}
	return filepath.Clean(filepath.FromSlash(p)), nil
}

// .
// .
// .
func noSymlinkOnPath(dir, rel string) error {
	if rel == "." {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 1; i <= len(parts); i++ {
		prefix := filepath.FromSlash(strings.Join(parts[:i], "/"))
		fi, err := os.Lstat(filepath.Join(dir, prefix))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", prefix)
		}
	}
	return nil
}

// .
// .
// .
// .
func (h *harness) answerFS(op string, target, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	var t struct {
		Root string `json:"root"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal(target, &t)
	if t.Root == "" {
		return failed("OPERATION_TARGET_INVALID", nil), nil
	}
	var dir string
	write := true
	if t.Root == "private" {
		if h.privateDir == "" {
			d, err := os.MkdirTemp("", "aiisdk-private-")
			if err != nil {
				return failed("FS_IO_FAILED", nil), nil
			}
			h.privateDir = d
			h.observe("fs: the private directory is %s for this run (deleted at the end)", d)
		}
		dir = h.privateDir
	} else {
		r, ok := h.roots[t.Root]
		if !ok {
			h.observe("%s %s:%s -> denied (run with -grant root:%s=<path>[:rw])", op, t.Root, t.Path, t.Root)
			return nil, deny(fmt.Sprintf("no harness grant names root %q (aiisdk test -grant root:%s=<path>[:rw])", t.Root, t.Root), "POLICY_DENY")
		}
		dir, write = r.path, r.write
	}
	rel, err := cleanRel(t.Path)
	if err != nil {
		h.observe("%s %s:%s -> denied: %v", op, t.Root, t.Path, err)
		return nil, deny(err.Error(), "FS_PATH_INVALID")
	}
	if rel == "." && op != "fs.list" {
		return nil, deny(op+" requires target.path", "FS_PATH_INVALID")
	}
	if err := noSymlinkOnPath(dir, rel); err != nil {
		h.observe("%s %s:%s -> denied: real paths only: %v", op, t.Root, t.Path, err)
		return nil, deny("real paths only: "+err.Error(), "FS_SYMLINK_REFUSED")
	}
	switch op {
	case "fs.list":
		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			return failed("FS_NOT_FOUND", nil), nil
		}
		defer f.Close()
		entries, err := f.ReadDir(-1)
		if err != nil {
			return failed("FS_IO_FAILED", nil), nil
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		truncated := len(entries) > harnessListMax
		if truncated {
			entries = entries[:harnessListMax]
		}
		list := make([]map[string]interface{}, 0, len(entries))
		for _, e := range entries {
			entry := map[string]interface{}{"name": e.Name(), "dir": e.IsDir()}
			if info, ierr := e.Info(); ierr == nil {
				entry["size"] = info.Size()
				entry["modified"] = info.ModTime().UTC().Format(time.RFC3339)
				if info.Mode()&os.ModeSymlink != 0 {
					entry["symlink"] = true
				}
			}
			list = append(list, entry)
		}
		h.observe("fs.list %s:%s -> %d entries", t.Root, t.Path, len(list))
		return succeeded(map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "entries": list, "truncated": truncated}), nil
	case "fs.read":
		var a struct {
			Offset int64 `json:"offset"`
			Length int   `json:"length"`
			Digest bool  `json:"digest"`
		}
		_ = json.Unmarshal(arguments, &a)
		if a.Length <= 0 || a.Length > harnessReadMax {
			a.Length = harnessReadMax
		}
		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			h.observe("fs.read %s:%s -> not found", t.Root, t.Path)
			return failed("FS_NOT_FOUND", nil), nil
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			return failed("FS_NOT_FOUND", nil), nil
		}
		if _, err := f.Seek(a.Offset, io.SeekStart); err != nil {
			return failed("FS_IO_FAILED", nil), nil
		}
		data, _ := io.ReadAll(io.LimitReader(f, int64(a.Length)))
		h.observe("fs.read %s:%s -> %d bytes at %d of %d", t.Root, t.Path, len(data), a.Offset, fi.Size())
		result := map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "data_b64": base64.StdEncoding.EncodeToString(data), "bytes": len(data), "offset": a.Offset, "size": fi.Size(), "eof": a.Offset+int64(len(data)) >= fi.Size()}
		// .
		// .
		if a.Offset == 0 && a.Offset+int64(len(data)) >= fi.Size() {
			sum := sha256.Sum256(data)
			result["sha256"] = hex.EncodeToString(sum[:])
		} else if a.Digest {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return failed("FS_IO_FAILED", nil), nil
			}
			hh := sha256.New()
			if _, err := io.Copy(hh, f); err != nil {
				return failed("FS_IO_FAILED", nil), nil
			}
			result["sha256"] = hex.EncodeToString(hh.Sum(nil))
		}
		return succeeded(result), nil
	case "fs.publish":
		// .
		// .
		// .
		// .
		if !write {
			h.observe("fs.publish %s:%s -> denied (the root is read-only; grant it :rw)", t.Root, t.Path)
			return nil, deny(fmt.Sprintf("root %q is granted read-only", t.Root), "FS_READ_ONLY")
		}
		var a struct {
			Data           *string `json:"data"`
			DataB64        *string `json:"data_b64"`
			From           string  `json:"from"`
			SHA256         string  `json:"sha256"`
			ExpectedSHA256 string  `json:"expected_sha256"`
			ExpectedAbsent bool    `json:"expected_absent"`
		}
		if err := json.Unmarshal(arguments, &a); err != nil {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
		var data []byte
		haveData := false
		switch {
		case a.DataB64 != nil:
			dec, err := base64.StdEncoding.DecodeString(*a.DataB64)
			if err != nil {
				return failed("OPERATION_ARGUMENT_INVALID", nil), nil
			}
			data, haveData = dec, true
		case a.Data != nil:
			data, haveData = []byte(*a.Data), true
		}
		if (haveData && a.From != "") || (!haveData && a.From == "") || (a.From != "" && a.SHA256 == "") || (a.ExpectedSHA256 != "" && a.ExpectedAbsent) {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
		if haveData && len(data) > harnessWriteMax {
			return failed("NET_REQUEST_TOO_LARGE", nil), nil
		}
		full := filepath.Join(dir, rel)
		current, exists := "", false
		if cur, err := os.ReadFile(full); err == nil {
			sum := sha256.Sum256(cur)
			current, exists = hex.EncodeToString(sum[:]), true
		}
		switch {
		case a.ExpectedAbsent && exists, a.ExpectedSHA256 != "" && !exists, a.ExpectedSHA256 != "" && current != a.ExpectedSHA256:
			h.observe("fs.publish %s:%s -> generation mismatch (in place: %s) — nothing replaced", t.Root, t.Path, current)
			return failed("FS_GENERATION_MISMATCH", nil), nil
		}
		if d := filepath.Dir(rel); d != "." {
			if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
				return failed("FS_IO_FAILED", nil), nil
			}
		}
		var source, digest string
		var size int64
		if haveData {
			sum := sha256.Sum256(data)
			digest = hex.EncodeToString(sum[:])
			if a.SHA256 != "" && a.SHA256 != digest {
				return failed("FS_DIGEST_MISMATCH", nil), nil
			}
			tmp := full + ".publish-tmp"
			if err := os.WriteFile(tmp, data, 0o600); err != nil {
				_ = os.Remove(tmp)
				return failed("FS_IO_FAILED", nil), nil
			}
			source, size = tmp, int64(len(data))
		} else {
			fromRel, err := cleanRel(a.From)
			if err != nil || fromRel == "." || fromRel == rel {
				return nil, deny("from must name a staged file in the same root", "FS_PATH_INVALID")
			}
			staged, err := os.ReadFile(filepath.Join(dir, fromRel))
			if err != nil {
				return failed("FS_NOT_FOUND", nil), nil
			}
			sum := sha256.Sum256(staged)
			digest = hex.EncodeToString(sum[:])
			if digest != a.SHA256 {
				h.observe("fs.publish %s:%s -> the staged file measures %s, not the declared %s — nothing replaced", t.Root, t.Path, digest, a.SHA256)
				return failed("FS_DIGEST_MISMATCH", nil), nil
			}
			source, size = filepath.Join(dir, fromRel), int64(len(staged))
		}
		if err := os.Rename(source, full); err != nil {
			if haveData {
				_ = os.Remove(source)
			}
			return failed("FS_IO_FAILED", nil), nil
		}
		_ = os.Chmod(full, 0o600)
		h.observe("fs.publish %s:%s -> %d bytes, sha256 %s, replaced=%v", t.Root, t.Path, size, digest[:12], exists)
		return succeeded(map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "size": size, "sha256": digest, "replaced": exists, "durable": true, "durability": "synced"}), nil
	case "fs.write":
		if !write {
			h.observe("fs.write %s:%s -> denied (the root is read-only; grant it :rw)", t.Root, t.Path)
			return nil, deny(fmt.Sprintf("root %q is granted read-only", t.Root), "FS_READ_ONLY")
		}
		var a struct {
			Data    *string `json:"data"`
			DataB64 *string `json:"data_b64"`
			Append  bool    `json:"append"`
		}
		_ = json.Unmarshal(arguments, &a)
		var data []byte
		switch {
		case a.DataB64 != nil:
			dec, err := base64.StdEncoding.DecodeString(*a.DataB64)
			if err != nil {
				return failed("OPERATION_ARGUMENT_INVALID", nil), nil
			}
			data = dec
		case a.Data != nil:
			data = []byte(*a.Data)
		default:
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
		if len(data) > harnessWriteMax {
			return failed("NET_REQUEST_TOO_LARGE", nil), nil
		}
		if d := filepath.Dir(rel); d != "." {
			if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
				return failed("FS_IO_FAILED", nil), nil
			}
		}
		flags := os.O_WRONLY | os.O_CREATE
		if a.Append {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		full := filepath.Join(dir, rel)
		f, err := os.OpenFile(full, flags, 0o600)
		if err != nil {
			return failed("FS_IO_FAILED", nil), nil
		}
		n, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			return failed("FS_IO_FAILED", nil), nil
		}
		_ = os.Chmod(full, 0o600)
		size := int64(n)
		if fi, err := os.Stat(full); err == nil {
			size = fi.Size()
		}
		h.observe("fs.write %s:%s -> %d bytes (append=%v)", t.Root, t.Path, n, a.Append)
		return succeeded(map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "bytes": n, "size": size, "appended": a.Append}), nil
	default:
		if !write {
			return nil, deny(fmt.Sprintf("root %q is granted read-only", t.Root), "FS_READ_ONLY")
		}
		if _, err := os.Lstat(filepath.Join(dir, rel)); err != nil {
			return succeeded(map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "deleted": false}), nil
		}
		if err := os.Remove(filepath.Join(dir, rel)); err != nil {
			return failed("FS_IO_FAILED", nil), nil
		}
		h.observe("fs.delete %s:%s -> deleted", t.Root, t.Path)
		return succeeded(map[string]interface{}{"root": t.Root, "path": filepath.ToSlash(rel), "deleted": true}), nil
	}
}

// .
func (h *harness) closeFiles() {
	if h.privateDir != "" {
		_ = os.RemoveAll(h.privateDir)
		h.privateDir = ""
	}
}

// .
const harnessChunkMax = 64 << 10

// .
// .
func (h *harness) answerHTTPRead(target, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	var t struct {
		StreamID string `json:"stream_id"`
	}
	_ = json.Unmarshal(target, &t)
	s := h.streams[t.StreamID]
	if s == nil {
		return nil, deny(fmt.Sprintf("no open stream %s in this run", t.StreamID), "OPERATION_TARGET_INVALID")
	}
	n := harnessChunkMax
	if len(arguments) > 0 {
		var a struct {
			MaxBytes int `json:"max_bytes"`
		}
		_ = json.Unmarshal(arguments, &a)
		if a.MaxBytes > harnessChunkMax {
			return nil, json.RawMessage(fmt.Sprintf(`{"code":-32602,"message":"max_bytes %d exceeds the %d-byte chunk ceiling"}`, a.MaxBytes, harnessChunkMax))
		}
		if a.MaxBytes > 0 {
			n = a.MaxBytes
		}
	}
	buf := make([]byte, n)
	k, err := s.resp.Body.Read(buf)
	s.total += k
	if k > 0 {
		s.seq++
	}
	done := err != nil || s.total > harnessMaxBody
	or := map[string]interface{}{"stream_id": t.StreamID, "seq": s.seq, "data_b64": base64.StdEncoding.EncodeToString(buf[:k]), "bytes": k, "done": done, "total_bytes": s.total}
	if done {
		s.resp.Body.Close()
		delete(h.streams, t.StreamID)
		switch {
		case s.total > harnessMaxBody:
			h.observe("http.read %s -> over the %d-byte stream ceiling after %d chunks", t.StreamID, harnessMaxBody, s.seq)
			return failed("NET_RESPONSE_TOO_LARGE", or), nil
		case err != io.EOF:
			h.observe("http.read %s -> ended after %d bytes: %v", t.StreamID, s.total, err)
			return failed("NET_REMOTE_OUTCOME_FAILED", or), nil
		}
		h.observe("http.read %s -> complete: %d bytes in %d chunks", t.StreamID, s.total, s.seq)
	}
	return succeeded(or), nil
}

// .
func (h *harness) answerHTTPClose(target json.RawMessage) (json.RawMessage, json.RawMessage) {
	var t struct {
		StreamID string `json:"stream_id"`
	}
	_ = json.Unmarshal(target, &t)
	s := h.streams[t.StreamID]
	if s == nil {
		return nil, deny(fmt.Sprintf("no open stream %s in this run", t.StreamID), "OPERATION_TARGET_INVALID")
	}
	s.resp.Body.Close()
	delete(h.streams, t.StreamID)
	h.observe("http.close %s -> closed by the plugin after %d bytes in %d chunks", t.StreamID, s.total, s.seq)
	return succeeded(map[string]interface{}{"stream_id": t.StreamID, "closed": true, "total_bytes": s.total, "chunks": s.seq}), nil
}

// .
// .
// .
// .
// .
// .
func (h *harness) answerEmbeddings(arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	if !h.embedGranted {
		h.observe("embeddings.create -> denied (run with -grant embeddings)")
		return nil, deny("embeddings are not granted in this harness run (aiisdk test -grant embeddings)", "POLICY_DENY")
	}
	var a struct {
		Input []string `json:"input"`
	}
	_ = json.Unmarshal(arguments, &a)
	if len(a.Input) == 0 || len(a.Input) > 16 {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	vectors := make([][]float32, len(a.Input))
	for i, s := range a.Input {
		if len(s) > 8<<10 {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
		vectors[i] = harnessEmbed(s)
	}
	h.observe("embeddings.create -> %d vectors from the harness stand-in (hashed words, not a model)", len(vectors))
	return succeeded(map[string]interface{}{"model": "harness-hashed-words-32", "dimensions": 32, "vectors": vectors}), nil
}

func harnessEmbed(text string) []float32 {
	v := make([]float32, 32)
	word := make([]byte, 0, 32)
	flush := func() {
		if len(word) == 0 {
			return
		}
		var hsh uint32 = 2166136261
		for _, b := range word {
			hsh ^= uint32(b)
			hsh *= 16777619
		}
		v[hsh%32]++
		word = word[:0]
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			word = append(word, c)
		} else {
			flush()
		}
	}
	flush()
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm > 0 {
		n := float32(math.Sqrt(norm))
		for i := range v {
			v[i] /= n
		}
	}
	return v
}

// .
// .
// .
// .
type localScope struct {
	addr   netip.Addr
	prefix netip.Prefix
	name   string
	port   int
}

func isLocalIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.ParseIP("169.254.169.254")) {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

func parseLocalScope(s string) (localScope, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return localScope{}, fmt.Errorf("empty local scope")
	}
	hostPart, portPart := s, ""
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return localScope{}, fmt.Errorf("unterminated IPv6 literal")
		}
		hostPart = s[1:end]
		if rest := s[end+1:]; rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return localScope{}, fmt.Errorf("expected :port after the IPv6 literal")
			}
			portPart = rest[1:]
		}
	case strings.Count(s, ":") == 1:
		i := strings.IndexByte(s, ':')
		hostPart, portPart = s[:i], s[i+1:]
	case strings.Count(s, ":") > 1:
		hostPart = s
	}
	sc := localScope{}
	switch portPart {
	case "", "*":
	default:
		n, err := strconv.Atoi(portPart)
		if err != nil || n < 1 || n > 65535 {
			return localScope{}, fmt.Errorf("port %q is not 1-65535 or *", portPart)
		}
		sc.port = n
	}
	if hostPart == "" || hostPart == "*" {
		return localScope{}, fmt.Errorf("a local scope names an address, a range or a name")
	}
	if pfx, err := netip.ParsePrefix(hostPart); err == nil {
		if !isLocalIP(pfx.Addr().AsSlice()) {
			return localScope{}, fmt.Errorf("%s is not a range on the local network", pfx)
		}
		sc.prefix = pfx.Masked()
		return sc, nil
	}
	if a, err := netip.ParseAddr(hostPart); err == nil {
		a = a.Unmap()
		if !isLocalIP(a.AsSlice()) {
			return localScope{}, fmt.Errorf("%s is not an address on the local network (use net.outbound for the internet)", a)
		}
		sc.addr = a
		return sc, nil
	}
	sc.name = strings.ToLower(strings.TrimSuffix(hostPart, "."))
	return sc, nil
}

func (s localScope) matches(host string, port int) bool {
	if s.port != 0 && s.port != port {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		switch {
		case s.addr.IsValid():
			return s.addr == ip
		case s.prefix.IsValid():
			return s.prefix.Contains(ip)
		}
		return false
	}
	return s.name != "" && s.name == strings.ToLower(strings.TrimSuffix(host, "."))
}
