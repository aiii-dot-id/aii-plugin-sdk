// .
// .
// .
// .
// .
// .
// .
// .
package main

import (
	"fmt"
	"net/url"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const host = "net.outbound:api.twilio.com:443"

func init() {
	p := sdk.New("com.aiii.examples.twilio-sms")

	p.Describe("describe", sdk.Descriptor{
		Summary: "Name the channel this adapter serves and how it receives",
		Input:   "schemas/describe_in.json",
		Output:  "schemas/describe_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("describe", func(c sdk.Call) (any, error) {
		return sdk.ChannelDescription{Channel: "sms", Receive: "webhook"}.Value(), nil
	})

	p.Describe("send", sdk.Descriptor{
		Summary:      "Send one SMS through Twilio",
		Input:        "schemas/send_in.json",
		Output:       "schemas/send_out.json",
		Effects:      sdk.EffectsWriteExternal,
		Capabilities: []string{host},
	})
	p.Handle("send", send)

	p.Describe("receive", sdk.Descriptor{
		Summary: "Not how this adapter hears: messages arrive by webhook",
		Input:   "schemas/receive_in.json",
		Output:  "schemas/receive_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("receive", func(c sdk.Call) (any, error) {
		return nil, sdk.Fail("OPERATION_NOT_SUPPORTED", "this adapter receives by webhook; describe says so and the host does not poll it")
	})

	p.Describe("sms.inbound", sdk.Descriptor{
		Summary: "Twilio's inbound message webhook, verified by the host",
		Input:   "schemas/inbound_in.json",
		Output:  "schemas/inbound_out.json",
		Effects: sdk.EffectsReadExternal,
	})
	p.Handle("sms.inbound", inbound)

	p.Run()
}

func send(c sdk.Call) (any, error) {
	to, ok := c.Args().String("address")
	if !ok || to == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "send requires arguments.address (E.164)")
	}
	body, _ := c.Args().String("body")
	vals, err := sdk.Settings.Load()
	if err != nil {
		return nil, err
	}
	sid, _ := vals.String("account_sid")
	from, _ := vals.String("from_number")
	token, _ := vals.Handle("auth_token")
	if sid == "" || from == "" || token == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "account_sid, from_number and the auth_token handle must be set in the plugins view")
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", from)
	form.Set("Body", body)
	res, err := sdk.HTTP.Post(fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid), &sdk.HTTPOptions{
		Body: form.Encode(), ContentType: "application/x-www-form-urlencoded", AuthProfile: token,
	})
	if err != nil {
		if sdk.EffectUnknown(err) {
			return nil, sdk.Fail("NET_EFFECT_UNKNOWN", "the send was written and the response was lost — the message may have gone; do not resend blindly")
		}
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and the auth_token handle in plugins.grants")
		}
		return nil, err
	}
	msgSid, _ := sdk.Object(res.Body).String("sid")
	return map[string]any{"receipt": msgSid, "http_status": res.Status, "effect": res.Effect}, nil
}

func inbound(c sdk.Call) (any, error) {
	req := sdk.ParseWebhook(c)
	form := req.Form()
	if form["MessageSid"] == "" || form["From"] == "" {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "not a Twilio message webhook: MessageSid and From are required")
	}
	return sdk.WebhookResult{
		Arrival:  &sdk.Arrival{ID: form["MessageSid"], From: form["From"], Body: form["Body"]},
		Response: &sdk.WebhookResponse{Status: 200, ContentType: "text/xml", Body: "<Response/>"},
	}.Value(), nil
}

// .
// .
func main() { sdk.MainDescribe() }
