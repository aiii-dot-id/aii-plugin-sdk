package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	host = "net.outbound:api.telegram.org:443"
	api  = "https://api.telegram.org/bot" + sdk.CredentialPlaceholder + "/"

	budget      = 10
	pollSeconds = budget - 1
	pollTimeout = (budget*1000 - 500)

	pollLimit = 1
	offsetKey = "telegram.offset"

	sendsKey = "telegram.sends"

	maxText = 4096
)

var sends = sdk.SendWindow{Key: sendsKey}

var adapter = sdk.New("com.aiii.examples.telegram")

var (
	offset int64
	saved  int64
	loaded bool
)

type held struct {
	update  int64
	arrival string
}

var pending []held

func init() {
	p := adapter

	p.Describe("describe", sdk.Descriptor{
		Summary: "Name the channel this adapter serves and how it receives",
		Input:   "schemas/describe_in.json",
		Output:  "schemas/describe_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("describe", func(c sdk.Call) (any, error) {
		return sdk.ChannelDescription{Channel: "telegram", Receive: "poll", BudgetSeconds: budget, Acknowledges: true}.Value(), nil
	})

	p.Describe("send", sdk.Descriptor{
		Summary:      "Send one message to a chat through the Telegram bot API",
		Input:        "schemas/send_in.json",
		Output:       "schemas/send_out.json",
		Effects:      sdk.EffectsWriteExternal,
		Capabilities: []string{host, "ring4.kv"},
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

func denied(d *sdk.Denied) error {
	return sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and the bot_token handle in plugins.grants")
}

func send(c sdk.Call) (any, error) {
	to, ok := c.Args().String("address")
	if !ok || !validAddress(to) {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "send requires arguments.address: a chat id (a number, negative for a group) or @username")
	}

	body, _ := c.Args().String("body")
	if n := utf8.RuneCountInString(body); n < 1 || n > maxText {
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", fmt.Sprintf("Telegram carries a message of 1 to %d characters and this one has %d — the message was not sent", maxText, n))
	}
	handle, err := token()
	if err != nil {
		return nil, err
	}

	v, err := sends.Admit(c)
	if err != nil {
		return nil, err
	}
	if !v.OK {
		return nil, v.Refusal("the message was not sent")
	}
	payload, _ := json.Marshal(map[string]any{"chat_id": to, "text": body})
	res, err := sdk.HTTP.Post(api+"sendMessage", &sdk.HTTPOptions{
		Body: string(payload), ContentType: "application/json", AuthProfile: handle, TimeoutMS: 15000,
	})
	out, err := sent(res, err)
	if sends.Counts(err) {
		if rerr := sends.Record(c, v); rerr != nil && err == nil {
			if m, ok := out.(map[string]any); ok {
				m["effect"] = fmt.Sprint(m["effect"]) + "; the send window could not be updated (" + rerr.Error() + ")"
			}
		}
	}
	return out, err
}

func sent(res sdk.HTTPResult, err error) (any, error) {
	if err != nil && res.Status < 400 {
		return nil, unanswered(res, err)
	}
	answer := sdk.Object(res.Body)
	switch {
	case res.Status == 403 || res.Status == 400 && aboutTheChat(answer):
		return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "Telegram will not deliver to this chat ("+telegramSaid(res.Status, answer)+") — the message was not sent")
	case res.Status == 429:
		return nil, sdk.Fail("NET_REMOTE_FAILED", "Telegram refused it ("+telegramSaid(res.Status, answer)+") and "+wait(res, answer)+" — the message was not sent")
	case res.Status >= 500:
		return nil, sdk.Fail("NET_EFFECT_UNKNOWN", "Telegram answered "+telegramSaid(res.Status, answer)+" — the message may have gone; do not resend blindly")
	case res.Status >= 400:
		return nil, sdk.Fail("NET_REMOTE_FAILED", "Telegram refused it ("+telegramSaid(res.Status, answer)+") — the message was not sent")
	}
	ok, readable := answer.Bool("ok")
	if readable && !ok {
		return nil, sdk.Fail("NET_REMOTE_FAILED", "Telegram refused it ("+telegramSaid(res.Status, answer)+") — the message was not sent")
	}
	msg := answer.Object("result")
	id, hasID := msg.Int("message_id")
	chat, hasChat := msg.Object("chat").Int("id")
	if !ok || !hasID || !hasChat {
		return nil, sdk.Fail("NET_EFFECT_UNKNOWN", fmt.Sprintf("Telegram answered %d and the answer could not be read — the message may have gone; do not resend blindly", res.Status))
	}
	return map[string]any{"receipt": fmt.Sprintf("%d:%d", chat, id), "http_status": res.Status, "effect": res.Effect}, nil
}

func unanswered(res sdk.HTTPResult, err error) error {
	if d, ok := sdk.AsDenied(err); ok {
		return denied(d)
	}
	oe, typed := sdk.AsOperationError(err)
	switch {
	case !typed:
		return sdk.Fail("NET_EFFECT_UNKNOWN", "the host's answer to the send could not be read — the message may have gone; do not resend blindly")
	case oe.ReasonCode == "NET_EFFECT_UNKNOWN":
		return sdk.Fail("NET_EFFECT_UNKNOWN", "the send was written and the response was lost — the message may have gone; do not resend blindly")
	case oe.ReasonCode == "NET_REMOTE_OUTCOME_FAILED" && res.Effect == "not-performed":
		return sdk.Fail("NET_REMOTE_FAILED", "Telegram was not reached — the message was not sent")
	}
	return err
}

func aboutTheChat(answer sdk.Object) bool {
	if _, moved := answer.Object("parameters").Int("migrate_to_chat_id"); moved {
		return true
	}
	d, _ := answer.String("description")
	return strings.Contains(strings.ToLower(d), "chat not found")
}

func telegramSaid(status int, answer sdk.Object) string {
	s := strconv.Itoa(status)
	if d, _ := answer.String("description"); d != "" {
		s += ": " + d
	}
	if to, moved := answer.Object("parameters").Int("migrate_to_chat_id"); moved {
		s += fmt.Sprintf("; the group is now chat %d", to)
	}
	return s
}

func wait(res sdk.HTTPResult, answer sdk.Object) string {
	if s, ok := answer.Object("parameters").Int("retry_after"); ok && s >= 0 {
		return fmt.Sprintf("asks to wait %d s", s)
	}
	if res.RetryAfterS != nil {
		return fmt.Sprintf("asks to wait %d s", *res.RetryAfterS)
	}
	return "named no time to wait"
}

func receive(c sdk.Call) (any, error) {
	handle, err := token()
	if err != nil {
		return nil, err
	}
	settle(c.Args())
	commitOffset()
	url := fmt.Sprintf("%sgetUpdates?timeout=%d&limit=%d&offset=%d&allowed_updates=%%5B%%22message%%22%%5D", api, pollSeconds, pollLimit, offset)
	res, err := sdk.HTTP.Get(url, &sdk.HTTPOptions{AuthProfile: handle, TimeoutMS: pollTimeout})
	if err != nil && res.Status == 0 {
		if d, ok := sdk.AsDenied(err); ok {
			return nil, denied(d)
		}
		return nil, err
	}
	answer := sdk.Object(res.Body)
	updates, readable := sdk.ObjectArray(answer.Raw("result"))
	if ok, _ := answer.Bool("ok"); err != nil || !ok || !readable {
		return nil, sdk.Fail("NET_REMOTE_FAILED", "getUpdates failed ("+telegramSaid(res.Status, answer)+"); the next poll asks for the same updates")
	}
	arrivals := []map[string]any{}
	pending = pending[:0]
	for _, u := range updates {
		id, ok := u.Int("update_id")
		if !ok {
			continue
		}
		pending = append(pending, held{update: id})
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
		arrival := fmt.Sprintf("%d:%d", chat, mid)
		pending[len(pending)-1].arrival = arrival
		arrivals = append(arrivals, map[string]any{
			"id":   arrival,
			"from": strconv.FormatInt(chat, 10),
			"body": body,
		})
	}
	return arrivals, nil
}

func settle(args sdk.Object) {
	recorded, told := args.StringArray("recorded")
	told = told && args.Has("recorded")
	holds := map[string]bool{}
	for _, id := range recorded {
		holds[id] = true
	}
	for _, h := range pending {
		if told && h.arrival != "" && !holds[h.arrival] {
			break
		}
		if h.update >= offset {
			offset = h.update + 1
		}
	}
	pending = nil
}

func commitOffset() {
	if !loaded {
		loaded = true
		if v, ok, err := sdk.KV.Get(offsetKey); err == nil && ok {
			if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
				offset, saved = n, n
			}
		}
	}
	if offset != saved {
		saved = offset
		_, _ = sdk.KV.Put(offsetKey, strconv.FormatInt(offset, 10))
	}
}

func kind(msg sdk.Object) string {
	for _, k := range []string{"photo", "document", "voice", "audio", "video", "sticker", "location", "contact"} {
		if msg.Has(k) {
			return "[" + k + "]"
		}
	}
	return "[message]"
}

func main() { sdk.MainDescribe() }
