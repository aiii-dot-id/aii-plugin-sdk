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
