package aiiosdk

import (
	"encoding/json"
	"testing"
)

func TestWebhookRequestAndResultAreTheHostsShapes(t *testing.T) {
	c := Call{Arguments: json.RawMessage(`{"method":"POST","path":"sms","query":"a=1","headers":{"Content-Type":"application/x-www-form-urlencoded","X-Twilio-Signature":"sig"},"body":"Body=hi+there&From=%2B15550001&MessageSid=SM1&From=dup"}`)}
	req := ParseWebhook(c)
	if req.Method != "POST" || req.Path != "sms" || req.Query != "a=1" || req.Header("X-Twilio-Signature") != "sig" {
		t.Fatalf("request: %+v", req)
	}
	form := req.Form()
	if form["Body"] != "hi there" || form["From"] != "+15550001" || form["MessageSid"] != "SM1" {
		t.Fatalf("form: %v", form)
	}
	out, _ := json.Marshal(WebhookResult{Arrival: &Arrival{ID: "SM1", From: "+15550001", Body: "hi there"}, Response: &WebhookResponse{Status: 200, ContentType: "text/xml", Body: "<Response/>"}}.Value())
	if string(out) != `{"arrival":{"body":"hi there","from":"+15550001","id":"SM1"},"response":{"body":"\u003cResponse/\u003e","content_type":"text/xml","status":200}}` {
		t.Fatalf("result: %s", out)
	}
	if v := (WebhookResult{}).Value(); len(v) != 0 {
		t.Fatal("an empty result is an empty object")
	}
}

// A webhook may answer several arrivals — a notification can stand for a
// burst of mail — in the host's shape: arrivals, in order.
func TestAWebhookResultMayCarrySeveralArrivals(t *testing.T) {
	out, _ := json.Marshal(WebhookResult{Arrivals: []Arrival{{ID: "m1", From: "a@x.test", Body: "one"}, {ID: "m2", From: "b@x.test", Body: "two"}}, Response: &WebhookResponse{Status: 200}}.Value())
	if string(out) != `{"arrivals":[{"body":"one","from":"a@x.test","id":"m1"},{"body":"two","from":"b@x.test","id":"m2"}],"response":{"status":200}}` {
		t.Fatalf("result: %s", out)
	}
}

// A plugin is told where its webhooks are: by declared path, its own only, with
// no answer — and a reason to say so — when there is none.
func TestAPluginReadsWhereItsWebhooksAre(t *testing.T) {
	c := Call{Arguments: json.RawMessage(`{"_host_webhooks":{"notify":"https://identity.example.test/hooks/org.example.mail/notify","other":""}}`)}
	if u, ok := c.WebhookURL("notify"); !ok || u != "https://identity.example.test/hooks/org.example.mail/notify" {
		t.Errorf("notify: %q %v", u, ok)
	}
	for _, path := range []string{"other", "absent"} {
		if u, ok := c.WebhookURL(path); ok || u != "" {
			t.Errorf("%s: %q %v", path, u, ok)
		}
	}
	if u, ok := (Call{Arguments: json.RawMessage(`{}`)}).WebhookURL("notify"); ok || u != "" {
		t.Errorf("no key: %q %v", u, ok)
	}
	if u, ok := (Call{Arguments: json.RawMessage(`{"_host_webhooks":"x"}`)}).WebhookURL("notify"); ok || u != "" {
		t.Errorf("a value that is not an object: %q %v", u, ok)
	}
}
