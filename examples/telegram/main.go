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
package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	host = "net.outbound:api.telegram.org:443"
	api  = "https://api.telegram.org/bot" + sdk.CredentialPlaceholder + "/"
	// .
	// .
	// .
	budget      = 10
	pollSeconds = budget - 1
	pollTimeout = (budget*1000 - 500)
	offsetKey   = "telegram.offset"
)

// .
// .
var (
	offset       int64
	offsetLoaded bool
)

func init() {
	p := sdk.New("com.aiii.examples.telegram")

	p.Describe("describe", sdk.Descriptor{
		Summary: "Name the channel this adapter serves and how it receives",
		Input:   "schemas/describe_in.json",
		Output:  "schemas/describe_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("describe", func(c sdk.Call) (any, error) {
		return sdk.ChannelDescription{Channel: "telegram", Receive: "poll", BudgetSeconds: budget}.Value(), nil
	})

	p.Describe("send", sdk.Descriptor{
		Summary:      "Send one message to a chat through the Telegram bot API",
		Input:        "schemas/send_in.json",
		Output:       "schemas/send_out.json",
		Effects:      sdk.EffectsWriteExternal,
		Capabilities: []string{host},
	})
	p.Handle("send", send)

	p.Describe("receive", sdk.Descriptor{
		Summary:      "Wait for messages: a long poll of getUpdates within the budget",
		Input:        "schemas/receive_in.json",
		Output:       "schemas/receive_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host, "ring4.kv"},
	})
	p.Handle("receive", receive)

	p.Run()
}

// .
// .
func token() (string, error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return "", err
	}
	handle, _ := vals.Handle("bot_token")
	if handle == "" {
		return "", sdk.Fail("OPERATION_ARGUMENT_INVALID", "the bot_token handle must be set in the plugins view")
	}
	return handle, nil
}

// .
// .
func validAddress(a string) bool {
	if strings.HasPrefix(a, "@") {
		name := a[1:]
		if len(name) < 5 || len(name) > 32 {
			return false
		}
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
				return false
			}
		}
		return true
	}
	_, err := strconv.ParseInt(a, 10, 64)
	return err == nil
}

// .
func description(body []byte) string {
	d, _ := sdk.Object(body).String("description")
	return d
}

func send(c sdk.Call) (any, error) {
	to, ok := c.Args().String("address")
	if !ok || !validAddress(to) {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "send requires arguments.address: a chat id (a number, negative for a group) or @username")
	}
	body, _ := c.Args().String("body")
	handle, err := token()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"chat_id": to, "text": body})
	res, err := sdk.HTTP.Post(api+"sendMessage", &sdk.HTTPOptions{
		Body: string(payload), ContentType: "application/json", AuthProfile: handle, TimeoutMS: 15000,
	})
	if err != nil {
		if sdk.EffectUnknown(err) {
			return nil, sdk.Fail("NET_EFFECT_UNKNOWN", "the send was written and the response was lost — the message may have gone; do not resend blindly")
		}
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and the bot_token handle in plugins.grants")
		}
		// .
		// .
		// .
		if res.Status == 400 || res.Status == 403 {
			return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", fmt.Sprintf("Telegram refused the address: %s", description(res.Body)))
		}
		return nil, err
	}
	envelope := sdk.Object(res.Body)
	if okField, _ := envelope.Bool("ok"); !okField {
		return nil, sdk.Fail("NET_REMOTE_FAILED", "Telegram answered ok=false: "+description(res.Body))
	}
	id, _ := envelope.Object("result").Int("message_id")
	chat, _ := envelope.Object("result").Object("chat").Int("id")
	return map[string]any{"receipt": fmt.Sprintf("%d:%d", chat, id), "http_status": res.Status, "effect": res.Effect}, nil
}

// .
// .
func loadOffset() {
	if offsetLoaded {
		return
	}
	offsetLoaded = true
	if v, ok, err := sdk.KV.Get(offsetKey); err == nil && ok {
		if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
			offset = n
		}
	}
}

func receive(c sdk.Call) (any, error) {
	handle, err := token()
	if err != nil {
		return nil, err
	}
	loadOffset()
	url := fmt.Sprintf("%sgetUpdates?timeout=%d&offset=%d&allowed_updates=%%5B%%22message%%22%%5D", api, pollSeconds, offset)
	res, err := sdk.HTTP.Get(url, &sdk.HTTPOptions{AuthProfile: handle, TimeoutMS: pollTimeout})
	if err != nil {
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and the bot_token handle in plugins.grants")
		}
		return nil, err
	}
	envelope := sdk.Object(res.Body)
	if okField, _ := envelope.Bool("ok"); !okField {
		return nil, sdk.Fail("NET_REMOTE_FAILED", "Telegram answered ok=false: "+description(res.Body))
	}
	updates, _ := sdk.ObjectArray(envelope.Raw("result"))
	arrivals := []map[string]any{}
	next := offset
	for _, u := range updates {
		if id, ok := u.Int("update_id"); ok && id >= next {
			next = id + 1
		}
		msg := u.Object("message")
		if msg == nil {
			continue
		}
		chat, ok := msg.Object("chat").Int("id")
		mid, ok2 := msg.Int("message_id")
		if !ok || !ok2 {
			continue
		}
		body, _ := msg.String("text")
		if body == "" {
			body, _ = msg.String("caption")
		}
		if body == "" {
			body = kind(msg)
		}
		arrivals = append(arrivals, map[string]any{
			"id":   fmt.Sprintf("%d:%d", chat, mid),
			"from": strconv.FormatInt(chat, 10),
			"body": body,
		})
	}
	if next != offset {
		offset = next
		// .
		_, _ = sdk.KV.Put(offsetKey, strconv.FormatInt(offset, 10))
	}
	return arrivals, nil
}

// .
// .
func kind(msg sdk.Object) string {
	for _, k := range []string{"photo", "document", "voice", "audio", "video", "sticker", "location", "contact"} {
		if msg.Has(k) {
			return "[" + k + "]"
		}
	}
	return "[message]"
}

// .
// .
func main() { sdk.MainDescribe() }
