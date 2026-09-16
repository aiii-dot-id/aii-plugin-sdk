package aiiosdk

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
)

// .
// .
// .
// .
type InvokeResult struct {
	// .
	// .
	// .
	// .
	// .
	Status string
	// .
	OperationResult json.RawMessage
	// .
	// .
	// .
	Reason     string
	ReasonCode string
	// .
	// .
	// .
	ExternalReceipt json.RawMessage
	// .
	// .
	Raw json.RawMessage
}

// .
func (r *InvokeResult) Succeeded() bool { return r.Status == "succeeded" }

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
func InvokeCall(operation string, target, arguments any) (*InvokeResult, error) {
	if operation == "" {
		return nil, fmt.Errorf("aiiosdk: InvokeCall requires an operation")
	}
	// .
	// .
	// .
	params := appendJSONString([]byte(`{"operation":`), operation)
	if target != nil {
		raw, err := marshalValue(target)
		if err != nil {
			return nil, fmt.Errorf("aiiosdk: marshal target: %w", err)
		}
		params = append(params, `,"target":`...)
		params = append(params, raw...)
	}
	if arguments != nil {
		raw, err := marshalValue(arguments)
		if err != nil {
			return nil, fmt.Errorf("aiiosdk: marshal arguments: %w", err)
		}
		params = append(params, `,"arguments":`...)
		params = append(params, raw...)
	}
	params = append(params, '}')

	reply, err := hostInvokeCallRaw(params)
	if err != nil {
		return nil, err
	}
	return decodeHostReply(reply)
}

// .
func decodeHostReply(reply []byte) (*InvokeResult, error) {
	if len(reply) == 0 {
		return nil, fmt.Errorf("aiiosdk: host returned an empty reply")
	}
	// .
	// .
	// .
	if err := ValidateStrict(reply); err != nil {
		return nil, fmt.Errorf("aiiosdk: host reply outside the strict JSON domain")
	}
	members, isObject := objectMembers(reply)
	if !isObject {
		return nil, fmt.Errorf("aiiosdk: host reply is not a JSON object")
	}

	codeRaw := memberByKey(members, "code")
	msgRaw := memberByKey(members, "message")
	statusRaw := memberByKey(members, "status")
	okRaw := memberByKey(members, "ok")
	successRaw := memberByKey(members, "success")

	// .
	// .
	// .
	// .
	// .
	if codeRaw != nil && msgRaw != nil && statusRaw == nil && okRaw == nil && successRaw == nil {
		code, err := strconv.Atoi(string(codeRaw))
		if err != nil {
			return nil, fmt.Errorf("aiiosdk: error object code is not an integer")
		}
		msg, ok := decodeJSONString(msgRaw)
		if !ok {
			return nil, fmt.Errorf("aiiosdk: error object message is not a string")
		}
		d := &Denied{Code: code, Message: msg}
		if dataMembers, ok := objectMembers(memberByKey(members, "data")); ok {
			// .
			// .
			// .
			if rc, ok := decodeJSONString(memberByKey(dataMembers, "reasonCode")); ok {
				d.ReasonCode = rc
			} else if rc, ok := decodeJSONString(memberByKey(dataMembers, "reason_code")); ok {
				d.ReasonCode = rc
			}
			if at, ok := decodeJSONString(memberByKey(dataMembers, "denied_at")); ok {
				d.DeniedAt = at
			}
		}
		return nil, d
	}

	res := &InvokeResult{Raw: cloneBytes(reply)}
	if s, ok := decodeJSONString(statusRaw); ok {
		res.Status = s
	}
	res.OperationResult = cloneBytes(memberByKey(members, "operation_result"))
	res.ExternalReceipt = cloneBytes(memberByKey(members, "external_receipt"))
	if r, ok := decodeJSONString(memberByKey(members, "reason")); ok {
		res.Reason = r
	}
	if rc, ok := decodeJSONString(memberByKey(members, "reasonCode")); ok {
		res.ReasonCode = rc
	} else if rc, ok := decodeJSONString(memberByKey(members, "reason_code")); ok {
		res.ReasonCode = rc
	}
	if res.Status == "" {
		// .
		// .
		// .
		// .
		if string(okRaw) == "false" || string(successRaw) == "false" {
			res.Status = "failed"
		} else {
			res.Status = "succeeded"
		}
	}
	if res.Status != "succeeded" {
		return res, &OperationError{Status: res.Status, ReasonCode: res.ReasonCode, Reason: res.Reason}
	}
	return res, nil
}

// .
// .
// .

// .
// .
// .
// .
// .
type KVClient struct{}

// .
var KV KVClient

// .
// .
type KVPutResult struct {
	Stored     bool
	Key        string
	ValueBytes int
	Scope      string
}

// .
// .
// .
// .
func (KVClient) Put(key, value string) (KVPutResult, error) {
	var out KVPutResult
	res, err := InvokeCall("kv.put",
		map[string]any{"key": key},
		map[string]any{"value": value})
	if err != nil {
		return out, err
	}
	or := Object(res.OperationResult)
	out.Stored, _ = or.Bool("stored")
	out.Key, _ = or.String("key")
	if n, ok := or.Int("value_bytes"); ok {
		out.ValueBytes = int(n)
	}
	out.Scope, _ = or.String("scope")
	return out, nil
}

// .
// .
// .
func (KVClient) Get(key string) (string, bool, error) {
	res, err := InvokeCall("kv.get", map[string]any{"key": key}, nil)
	if err != nil {
		if oe, ok := asOperationError(err); ok && oe.ReasonCode == "KV_NOT_FOUND" {
			return "", false, nil
		}
		return "", false, err
	}
	value, ok := Object(res.OperationResult).String("value")
	if !ok {
		return "", false, fmt.Errorf("aiiosdk: kv.get result carries no value string")
	}
	return value, true, nil
}

// .
// .
func (KVClient) Delete(key string) (bool, error) {
	res, err := InvokeCall("kv.delete", map[string]any{"key": key}, nil)
	if err != nil {
		return false, err
	}
	deleted, _ := Object(res.OperationResult).Bool("deleted")
	return deleted, nil
}

// .
// .
// .
func (KVClient) List(prefix string, limit int) ([]string, bool, error) {
	target := map[string]any{"prefix": prefix}
	var args map[string]any
	if limit > 0 {
		args = map[string]any{"limit": limit}
	}
	res, err := InvokeCall("kv.list", target, args)
	if err != nil {
		return nil, false, err
	}
	or := Object(res.OperationResult)
	keys, ok := or.StringArray("keys")
	if !ok {
		return nil, false, fmt.Errorf("aiiosdk: kv.list result carries no keys array")
	}
	more, _ := or.Bool("truncated")
	return keys, more, nil
}

// .
// .
// .
// .

// .
type EmbeddingsClient struct{}

// .
var Embeddings EmbeddingsClient

// .
// .
type EmbeddingsResult struct {
	Model      string
	Dimensions int
	Vectors    [][]float32
}

// .
// .
// .
func (EmbeddingsClient) Create(inputs []string) (EmbeddingsResult, error) {
	var out EmbeddingsResult
	res, err := InvokeCall("embeddings.create", nil, map[string]any{"input": inputs})
	if err != nil {
		return out, err
	}
	or := Object(res.OperationResult)
	out.Model, _ = or.String("model")
	if n, ok := or.Int("dimensions"); ok {
		out.Dimensions = int(n)
	}
	vectors, ok := or.FloatMatrix("vectors")
	if !ok {
		return out, fmt.Errorf("aiiosdk: embeddings.create result carries no vectors")
	}
	out.Vectors = vectors
	return out, nil
}

// .
// .

// .
// .
// .
// .
type HTTPClient struct{}

// .
var HTTP HTTPClient

// .
// .
// .
// .
// .
// .
const CredentialPlaceholder = "{credential}"

// .
// .
// .
// .
type HTTPOptions struct {
	// .
	// .
	TimeoutMS int
	// .
	// .
	// .
	// .
	// .
	// .
	AuthProfile string
	// .
	// .
	Body string
	// .
	ContentType string
	// .
	// .
	Headers map[string]string
	// .
	// .
	// .
	FollowRedirects *bool
	// .
	// .
	// .
	// .
	// .
	Idempotent bool
}

func (o *HTTPOptions) args(stream bool) map[string]any {
	args := map[string]any{}
	if o != nil {
		if o.TimeoutMS > 0 {
			args["timeout_ms"] = o.TimeoutMS
		}
		if o.AuthProfile != "" {
			args["auth_profile"] = o.AuthProfile
		}
		if o.Body != "" || o.ContentType != "" {
			args["body"] = o.Body
			args["content_type"] = o.ContentType
		}
		if len(o.Headers) > 0 {
			hs := map[string]any{}
			for k, v := range o.Headers {
				hs[k] = v
			}
			args["headers"] = hs
		}
		if o.FollowRedirects != nil {
			args["follow_redirects"] = *o.FollowRedirects
		}
		if o.Idempotent {
			args["idempotent"] = true
		}
	}
	if stream {
		args["stream"] = true
	}
	return args
}

// .
// .
// .
type HTTPResult struct {
	Status      int
	ContentType string
	Location    string
	Body        json.RawMessage
	// .
	// .
	// .
	// .
	Effect string
}

// .
// .
// .
// .
func (c HTTPClient) Get(url string, opts *HTTPOptions) (HTTPResult, error) {
	return c.do("http.get", url, opts)
}

// .
// .
func (c HTTPClient) Post(url string, opts *HTTPOptions) (HTTPResult, error) {
	return c.do("http.post", url, opts)
}

// .
func (c HTTPClient) Put(url string, opts *HTTPOptions) (HTTPResult, error) {
	return c.do("http.put", url, opts)
}

// .
func (c HTTPClient) Patch(url string, opts *HTTPOptions) (HTTPResult, error) {
	return c.do("http.patch", url, opts)
}

// .
func (c HTTPClient) Delete(url string, opts *HTTPOptions) (HTTPResult, error) {
	return c.do("http.delete", url, opts)
}

func (HTTPClient) do(op, url string, opts *HTTPOptions) (HTTPResult, error) {
	var out HTTPResult
	res, err := InvokeCall(op, map[string]any{"url": url}, opts.args(false))
	if res != nil && len(res.OperationResult) > 0 {
		or := Object(res.OperationResult)
		if n, ok := or.Int("http_status"); ok {
			out.Status = int(n)
		}
		out.ContentType, _ = or.String("content_type")
		out.Location, _ = or.String("location")
		out.Body = or.Raw("body")
	}
	if res != nil && len(res.ExternalReceipt) > 0 {
		out.Effect, _ = Object(res.ExternalReceipt).String("effect")
	}
	return out, err
}

// .
// .
// .
func EffectUnknown(err error) bool {
	oe, ok := asOperationError(err)
	return ok && oe.ReasonCode == "NET_EFFECT_UNKNOWN"
}

// .
// .
// .
type HTTPStream struct {
	id string
	// .
	Status      int
	ContentType string
	Location    string
	// .
	// .
	ChunkMax int
	Total    int
	Chunks   int
	done     bool
}

// .
// .
// .
func (HTTPClient) Stream(method, url string, opts *HTTPOptions) (*HTTPStream, error) {
	var op string
	switch method {
	case "GET":
		op = "http.get"
	case "POST":
		op = "http.post"
	case "PUT":
		op = "http.put"
	case "PATCH":
		op = "http.patch"
	case "DELETE":
		op = "http.delete"
	default:
		return nil, fmt.Errorf("aiiosdk: Stream method must be GET, POST, PUT, PATCH or DELETE, got %q", method)
	}
	res, err := InvokeCall(op, map[string]any{"url": url}, opts.args(true))
	if err != nil {
		return nil, err
	}
	or := Object(res.OperationResult)
	id, ok := or.String("stream_id")
	if !ok || id == "" {
		return nil, fmt.Errorf("aiiosdk: the host answered whole, not as a stream")
	}
	s := &HTTPStream{id: id}
	if n, ok := or.Int("http_status"); ok {
		s.Status = int(n)
	}
	if n, ok := or.Int("chunk_max_bytes"); ok {
		s.ChunkMax = int(n)
	}
	s.ContentType, _ = or.String("content_type")
	s.Location, _ = or.String("location")
	return s, nil
}

// .
// .
// .
// .
func (s *HTTPStream) Next() (chunk []byte, done bool, err error) {
	if s.done {
		return nil, true, nil
	}
	res, err := InvokeCall("http.read", map[string]any{"stream_id": s.id}, nil)
	if res != nil && len(res.OperationResult) > 0 {
		or := Object(res.OperationResult)
		if b64, ok := or.String("data_b64"); ok && b64 != "" {
			decoded, derr := base64.StdEncoding.DecodeString(b64)
			if derr != nil {
				// .
				// .
				s.done = true
				return nil, true, fmt.Errorf("aiiosdk: http.read answered a chunk that is not base64: %w", derr)
			}
			chunk = decoded
		}
		if d, ok := or.Bool("done"); ok {
			done = d
		}
		if n, ok := or.Int("total_bytes"); ok {
			s.Total = int(n)
		}
		if n, ok := or.Int("seq"); ok {
			s.Chunks = int(n)
		}
	}
	if err != nil {
		done = true
	}
	if done {
		s.done = true
	}
	return chunk, done, err
}

// .
func (s *HTTPStream) Close() error {
	if s.done {
		return nil
	}
	s.done = true
	_, err := InvokeCall("http.close", map[string]any{"stream_id": s.id}, nil)
	return err
}

// .
// .
func (s *HTTPStream) ReadAll(limit int) ([]byte, error) {
	var out []byte
	for {
		chunk, done, err := s.Next()
		out = append(out, chunk...)
		if err != nil {
			return out, err
		}
		if done {
			return out, nil
		}
		if limit > 0 && len(out) > limit {
			_ = s.Close()
			return out, fmt.Errorf("aiiosdk: stream exceeds the %d-byte limit this plugin set", limit)
		}
	}
}

// .
// .
// .
// .
func asOperationError(err error) (*OperationError, bool) {
	for e := err; e != nil; e = unwrapOnce(e) {
		if oe, ok := e.(*OperationError); ok {
			return oe, true
		}
	}
	return nil, false
}

// .
// .
// .
// .
func AsDenied(err error) (*Denied, bool) {
	for e := err; e != nil; e = unwrapOnce(e) {
		if d, ok := e.(*Denied); ok {
			return d, true
		}
	}
	return nil, false
}

// .
func AsOperationError(err error) (*OperationError, bool) { return asOperationError(err) }
