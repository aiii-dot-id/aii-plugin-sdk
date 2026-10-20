//go:build !wasm_unknown

package aiiosdk

// The host-call wrappers against the traced reply bytes. The fixtures
// here are not invented: the deny-all object is byte-identical to what
// the committed worker answered a live probe guest, and
// the result shapes are the broker's own marshalled structs
// (broker.go successResult/failureResult and the kv/http
// operation_result maps).

import (
	"errors"
	"strings"
	"testing"
)

// denyAllReply is the worker's zero-capability answer, byte-verified
// against the committed oracle (hostbbb.go:63-74).
const denyAllReply = `{"code":-32000,"message":"no capability broker attached to this worker; invoke-call denied","data":{"reasonCode":"POLICY_DENY"}}`

func withHost(t *testing.T, fn func(params []byte) ([]byte, error)) {
	t.Helper()
	saved := hostInvokeStub
	hostInvokeStub = fn
	t.Cleanup(func() { hostInvokeStub = saved })
}

func TestHostCallsOutsideGuest(t *testing.T) {
	if _, err := InvokeCall("kv.get", map[string]any{"key": "x"}, nil); !errors.Is(err, ErrNotInGuest) {
		t.Fatalf("want ErrNotInGuest, got %v", err)
	}
	if _, err := KV.Put("k", "v"); !errors.Is(err, ErrNotInGuest) {
		t.Fatalf("want ErrNotInGuest, got %v", err)
	}
	if _, err := HTTP.Get("https://example.com/", nil); !errors.Is(err, ErrNotInGuest) {
		t.Fatalf("want ErrNotInGuest, got %v", err)
	}
}

func TestDenyAllIsATypedDenial(t *testing.T) {
	var gotParams []byte
	withHost(t, func(params []byte) ([]byte, error) {
		gotParams = append([]byte(nil), params...)
		return []byte(denyAllReply), nil
	})

	_, err := KV.Put("focus", "the-value")
	var d *Denied
	if !errors.As(err, &d) {
		t.Fatalf("want *Denied, got %T %v", err, err)
	}
	if d.Code != -32000 || d.ReasonCode != "POLICY_DENY" {
		t.Fatalf("denial fields: %+v", d)
	}
	// The audited params surface, exactly (operation + target
	// {"key":..} + arguments {"value":..} — broker.go:585-599, 609-621).
	want := `{"operation":"kv.put","target":{"key":"focus"},"arguments":{"value":"the-value"}}`
	if string(gotParams) != want {
		t.Fatalf("params bytes:\n got %s\nwant %s", gotParams, want)
	}
}

func TestKVSuccessShapes(t *testing.T) {
	t.Run("put", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			// The broker's success superset (broker.go:553-555, 640).
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"stored":true,"key":"focus","value_bytes":9,"scope":"temp"},"external_receipt":{"success":true,"host_authored":true}}`), nil
		})
		res, err := KV.Put("focus", "the-value")
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if !res.Stored || res.Key != "focus" || res.ValueBytes != 9 || res.Scope != "temp" {
			t.Fatalf("put result: %+v", res)
		}
	})

	t.Run("get found", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"key":"focus","value":"the-value"},"external_receipt":{}}`), nil
		})
		v, found, err := KV.Get("focus")
		if err != nil || !found || v != "the-value" {
			t.Fatalf("get: %q %v %v", v, found, err)
		}
	})

	t.Run("get missing is an answer, not an error", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			// KV_NOT_FOUND rides the failed-result shape
			// (broker.go:649-651 with resultReply:556-561).
			return []byte(`{"success":false,"ok":false,"status":"failed","reason":"KV_NOT_FOUND","reasonCode":"KV_NOT_FOUND","reason_code":"KV_NOT_FOUND","external_receipt":{}}`), nil
		})
		v, found, err := KV.Get("absent")
		if err != nil || found || v != "" {
			t.Fatalf("get missing: %q %v %v", v, found, err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"deleted":true,"key":"focus"},"external_receipt":{}}`), nil
		})
		deleted, err := KV.Delete("focus")
		if err != nil || !deleted {
			t.Fatalf("delete: %v %v", deleted, err)
		}
	})

	t.Run("quota failure is a typed OperationError", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			return []byte(`{"success":false,"ok":false,"status":"failed","reason":"KV_QUOTA_EXCEEDED","reasonCode":"KV_QUOTA_EXCEEDED","reason_code":"KV_QUOTA_EXCEEDED","external_receipt":{}}`), nil
		})
		_, err := KV.Put("k", "v")
		var oe *OperationError
		if !errors.As(err, &oe) || oe.ReasonCode != "KV_QUOTA_EXCEEDED" || oe.Status != "failed" {
			t.Fatalf("got %T %v", err, err)
		}
	})
}

func TestHTTPShapes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotParams string
		withHost(t, func(params []byte) ([]byte, error) {
			gotParams = string(params)
			// The daemon's operation_result shape (rpc.c:2092-2162
			// via broker.go:922-941).
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"http_status":200,"content_type":"application/json","body":{"ok":true}},"external_receipt":{}}`), nil
		})
		res, err := HTTP.Get("https://api.example.com/v1", &HTTPOptions{TimeoutMS: 5000})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if res.Status != 200 || res.ContentType != "application/json" || string(res.Body) != `{"ok":true}` {
			t.Fatalf("result: %+v", res)
		}
		if !strings.Contains(gotParams, `"operation":"http.get"`) ||
			!strings.Contains(gotParams, `"url":"https://api.example.com/v1"`) ||
			!strings.Contains(gotParams, `"timeout_ms":5000`) {
			t.Fatalf("params: %s", gotParams)
		}
		if strings.Contains(gotParams, "auth_profile") {
			t.Fatalf("unset auth_profile must not be sent (NET_UNKNOWN_ARGUMENT discipline): %s", gotParams)
		}
	})

	t.Run("remote 500 returns evidence AND a typed failure", func(t *testing.T) {
		withHost(t, func(params []byte) ([]byte, error) {
			// performed-but-failed (broker.go:947-954).
			return []byte(`{"success":false,"ok":false,"status":"failed","reason":"NET_REMOTE_OUTCOME_FAILED","reasonCode":"NET_REMOTE_OUTCOME_FAILED","reason_code":"NET_REMOTE_OUTCOME_FAILED","operation_result":{"http_status":500,"body":"boom"},"external_receipt":{}}`), nil
		})
		res, err := HTTP.Get("https://api.example.com/v1", nil)
		var oe *OperationError
		if !errors.As(err, &oe) || oe.ReasonCode != "NET_REMOTE_OUTCOME_FAILED" {
			t.Fatalf("got %T %v", err, err)
		}
		if res.Status != 500 || string(res.Body) != `"boom"` {
			t.Fatalf("evidence must be populated: %+v", res)
		}
	})
}

// The service's word on when to ask again: retry_after_s in operation_result is present with the
// seconds the host read, 0 included, and absent when the answer carried
// nothing usable or the host writes no field — nil, never a zero. The
// status, the effect, the answer and the typed failure are what they are
// without it.
func TestHTTPRetryAfterIsPresentZeroOrAbsent(t *testing.T) {
	failed := func(or string) string {
		return `{"success":false,"ok":false,"status":"failed","reason":"NET_REMOTE_OUTCOME_FAILED","reasonCode":"NET_REMOTE_OUTCOME_FAILED","reason_code":"NET_REMOTE_OUTCOME_FAILED","operation_result":` + or + `,"external_receipt":{"effect":"performed"}}`
	}
	for _, c := range []struct {
		name   string
		reply  string
		status int
		failed bool
		want   int64 // -1: nil
	}{
		{"a 429 with a hint", failed(`{"http_status":429,"retry_after_s":120,"body":"slow down"}`), 429, true, 120},
		{"a zero the service sent", failed(`{"http_status":429,"retry_after_s":0,"body":"slow down"}`), 429, true, 0},
		{"no hint", failed(`{"http_status":429,"body":"slow down"}`), 429, true, -1},
		{"a 503 whose hint is past what 32 bits hold", failed(`{"http_status":503,"retry_after_s":9223372036,"body":"slow down"}`), 503, true, 9223372036},
		{"a negative no host sends", failed(`{"http_status":429,"retry_after_s":-1,"body":"slow down"}`), 429, true, -1},
		{"a success that carries one", `{"success":true,"ok":true,"status":"succeeded","operation_result":{"http_status":200,"retry_after_s":30,"body":"slow down"},"external_receipt":{"effect":"performed"}}`, 200, false, 30},
	} {
		withHost(t, func(params []byte) ([]byte, error) { return []byte(c.reply), nil })
		res, err := HTTP.Post("https://api.example.com/send", &HTTPOptions{Body: "x", ContentType: "text/plain"})
		var oe *OperationError
		if c.failed != errors.As(err, &oe) || (c.failed && oe.ReasonCode != "NET_REMOTE_OUTCOME_FAILED") {
			t.Fatalf("%s: error %T %v", c.name, err, err)
		}
		if res.Status != c.status || res.Effect != "performed" || string(res.Body) != `"slow down"` {
			t.Fatalf("%s: the answer changed: %+v", c.name, res)
		}
		switch {
		case c.want < 0 && res.RetryAfterS != nil:
			t.Fatalf("%s: RetryAfterS = %d, want nil — no hint is not a zero", c.name, *res.RetryAfterS)
		case c.want >= 0 && (res.RetryAfterS == nil || *res.RetryAfterS != c.want):
			t.Fatalf("%s: RetryAfterS = %v, want %d", c.name, res.RetryAfterS, c.want)
		}
	}
}

func TestDecodeHostReplyEdges(t *testing.T) {
	t.Run("error object without data", func(t *testing.T) {
		res, err := decodeHostReply([]byte(`{"code":-32602,"message":"operation (string) required"}`))
		var d *Denied
		if res != nil || !errors.As(err, &d) || d.Code != -32602 || d.ReasonCode != "" {
			t.Fatalf("got %v %v", res, err)
		}
	})

	t.Run("denied_at decodes", func(t *testing.T) {
		_, err := decodeHostReply([]byte(`{"code":-32000,"message":"m","data":{"reasonCode":"capability_policy_denied","denied_at":"capability_evaluation"}}`))
		var d *Denied
		if !errors.As(err, &d) || d.DeniedAt != "capability_evaluation" {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("the F-5 cancelled shape decodes as a failure", func(t *testing.T) {
		// No status member, snake-only reason_code (rpc.c:2172-2181):
		// the old Go client misread this as success — the C booleans
		// are the tiebreak here.
		res, err := decodeHostReply([]byte(`{"success":false,"ok":false,"reason":"cancelled","reason_code":"INVOKE_CANCELLED","external_receipt":{}}`))
		var oe *OperationError
		if !errors.As(err, &oe) || oe.ReasonCode != "INVOKE_CANCELLED" {
			t.Fatalf("got %v %v", res, err)
		}
		if res == nil || res.Status != "failed" {
			t.Fatalf("cancelled must decode failed, got %+v", res)
		}
	})

	t.Run("camel reasonCode wins over snake", func(t *testing.T) {
		res, _ := decodeHostReply([]byte(`{"status":"failed","reasonCode":"CAMEL","reason_code":"SNAKE"}`))
		if res.ReasonCode != "CAMEL" {
			t.Fatalf("got %q", res.ReasonCode)
		}
	})

	t.Run("replies outside the domain are transport faults", func(t *testing.T) {
		for _, bad := range []string{``, `{"a":1,"a":2}`, `[1]`, `not json`} {
			if _, err := decodeHostReply([]byte(bad)); err == nil {
				t.Fatalf("%q must fail", bad)
			} else {
				var d *Denied
				var oe *OperationError
				if errors.As(err, &d) || errors.As(err, &oe) {
					t.Fatalf("%q must be a plain fault, got %T", bad, err)
				}
			}
		}
	})
}

func TestInvokeCallValidation(t *testing.T) {
	if _, err := InvokeCall("", nil, nil); err == nil {
		t.Fatalf("empty operation must fail")
	}
	var gotParams string
	withHost(t, func(params []byte) ([]byte, error) {
		gotParams = string(params)
		return []byte(`{"status":"succeeded"}`), nil
	})
	if _, err := InvokeCall("custom.op", nil, nil); err != nil {
		t.Fatalf("nil target/arguments must be legal: %v", err)
	}
	if gotParams != `{"operation":"custom.op"}` {
		t.Fatalf("omitempty members: %s", gotParams)
	}
}

// A REPLY THAT STATES NO OUTCOME IS NOT A SUCCESS. Every producer of a
// result — the host's broker, the daemon before it, this kit's own
// harness — states status and/or the success booleans; the default for an
// absent status exists for the reply that states the booleans alone. A
// reply stating neither was answered by nothing that performed anything,
// and reading it as "succeeded" tells a plugin its write happened.
func TestAReplyThatStatesNoOutcomeIsNotASuccess(t *testing.T) {
	for _, reply := range []string{
		`{}`,
		`{"operation_result":{"v":1}}`,
		`{"external_receipt":{}}`,
		`{"status":null}`,
		`{"status":7,"operation_result":{"v":1}}`,
		`{"reason":"x"}`,
		`{"ok":"yes","operation_result":{"v":1}}`,
		`{"success":1}`,
		`{"status":""}`,
	} {
		res, err := decodeHostReply([]byte(reply))
		if err == nil {
			t.Errorf("%s decoded as status %q with no error", reply, res.Status)
			continue
		}
		var d *Denied
		var oe *OperationError
		if errors.As(err, &d) || errors.As(err, &oe) {
			t.Errorf("%s: a reply outside the contract is a plain fault, not an outcome the host stated; got %T", reply, err)
		}
	}
	withHost(t, func([]byte) ([]byte, error) { return []byte(`{}`), nil })
	if _, err := KV.Put("k", "v"); err == nil {
		t.Error("kv.put answered {} and the plugin was told the write happened")
	}
}

// AND THE DEFAULT STANDS WHERE A PRODUCER USES IT: the booleans alone.
func TestAnAbsentStatusTakesTheBooleansWord(t *testing.T) {
	res, err := decodeHostReply([]byte(`{"success":true,"ok":true,"operation_result":{"v":1}}`))
	if err != nil || !res.Succeeded() {
		t.Fatalf("booleans true and no status: got %+v %v", res, err)
	}
	res, err = decodeHostReply([]byte(`{"ok":true}`))
	if err != nil || !res.Succeeded() {
		t.Fatalf("ok alone: got %+v %v", res, err)
	}
}

// A refusal carries the host's words. The broker spells the code in reason
// and says why in its receipt's detail; read as the bare code, a Memory
// store refused after work was delivered reached an identity as
// OPERATION_ARGUMENT_INVALID with nothing to correct (2026-10-20).
func TestARefusalCarriesTheReceiptsDetail(t *testing.T) {
	_, err := decodeHostReply([]byte(`{"status":"denied","ok":false,"success":false,"reason":"OPERATION_ARGUMENT_INVALID","reasonCode":"OPERATION_ARGUMENT_INVALID","external_receipt":{"success":false,"detail":"scoped memory writes require a resolvable executing work session"}}`))
	oe, ok := asOperationError(err)
	if !ok || oe.ReasonCode != "OPERATION_ARGUMENT_INVALID" || oe.Reason != "scoped memory writes require a resolvable executing work session" {
		t.Fatalf("the refusal lost the host's words: %#v", err)
	}
	// A reply that says why itself keeps its own words.
	_, err = decodeHostReply([]byte(`{"status":"failed","ok":false,"reason":"the plugin's own words","reasonCode":"X_FAILED","external_receipt":{"detail":"receipt words"}}`))
	if oe, ok := asOperationError(err); !ok || oe.Reason != "the plugin's own words" {
		t.Fatalf("a reply's own reason was replaced: %#v", err)
	}
}
