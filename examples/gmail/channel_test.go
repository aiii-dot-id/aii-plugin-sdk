package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	topic     = "projects/aii-mail/topics/gmail"
	verifiedA = "mx.google.com; dkim=pass header.i=@example.com; spf=pass smtp.mailfrom=recipient@example.com; dmarc=pass (p=NONE sp=NONE dis=NONE) header.from=example.com"
)

// inbox is a mailbox with a watch already set: the plugin's cursor is history
// id 100, kept in kv, and its settings carry the topic.
func inbox(t *testing.T) (*gmail, *standIn, *proc) {
	t.Helper()
	g := newGmail(t)
	g.historyID = "100"
	h := &standIn{t: t, handle: handle, gmail: g.handle, topic: topic, kv: map[string]string{cursorKey: "100"}}
	return g, h, start(t)
}

// arrive is mail landing in the inbox at history record hid, from Recipient, with
// Gmail's word on her domain unless told otherwise.
func (g *gmail) arrive(hid, id, subject string, extra ...field) *stored {
	if extra == nil {
		extra = []field{{"Authentication-Results", verifiedA}}
	}
	m := g.received(id, fromRecipient(subject, extra...), "text of "+subject, nil)
	g.history = append(g.history, histRec{hid, []string{id}})
	g.historyID = hid
	return m
}

func pushArgs(recorded []string) map[string]any {
	data := b64(`{"emailAddress":"` + me + `","historyId":"101"}`)
	a := map[string]any{"method": "POST", "path": "notify", "query": "", "headers": map[string]any{},
		"body": `{"message":{"data":"` + data + `","messageId":"1"},"subscription":"projects/aii-mail/subscriptions/push"}`}
	if recorded != nil {
		a["recorded"] = recorded
	}
	return a
}

type gotArrival struct{ ID, From, Body string }

func arrivalsOf(t *testing.T, r reply) []gotArrival {
	t.Helper()
	var out struct {
		Arrivals []struct{ ID, From, Body string } `json:"arrivals"`
	}
	if err := json.Unmarshal(r.result, &out); err != nil {
		t.Fatalf("not a webhook result: %s", r.result)
	}
	var list []gotArrival
	for _, a := range out.Arrivals {
		list = append(list, gotArrival{a.ID, a.From, a.Body})
	}
	return list
}

func statusOf(t *testing.T, r reply) int {
	t.Helper()
	var out struct {
		Response struct{ Status int } `json:"response"`
	}
	_ = json.Unmarshal(r.result, &out)
	return out.Response.Status
}

func ids(list []gotArrival) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

func TestTheAdapterDescribesAPushChannelThatAcknowledges(t *testing.T) {
	_, h, p := inbox(t)
	got := p.invoke(h, "describe", map[string]any{})
	if got.status != "succeeded" || got.text("channel") != "gmail" || got.text("receive") != "webhook" || got.field("acknowledges") != true {
		t.Errorf("describe: %s", got.line())
	}
	check(t, "receive is never answered", p.invoke(h, "receive", map[string]any{}), "failed", "OPERATION_NOT_SUPPORTED", "receives by webhook")
}

// The watch is renewed with Gmail's own call, and the first success takes the
// mailbox's place as the cursor — mail from before it is not delivered; a later
// renewal never moves it.
func TestTheWatchIsRenewedAndTheFirstOneSetsTheCursor(t *testing.T) {
	g := newGmail(t)
	g.historyID = "500"
	h := &standIn{t: t, handle: handle, gmail: g.handle, topic: topic, kv: map[string]string{}}
	p := start(t)
	got := p.invoke(h, "gmail.renew", map[string]any{"recorded": []any{}})
	if got.status != "succeeded" || got.text("expiration") != "1791000000000" {
		t.Fatalf("renew: %s", got.line())
	}
	if len(g.watches) != 1 || g.watches[0]["topicName"] != topic || !reflect.DeepEqual(g.watches[0]["labelIds"], []any{"INBOX"}) || g.watches[0]["labelFilterBehavior"] != "INCLUDE" {
		t.Errorf("the watch asks for %v", g.watches)
	}
	if h.kv[cursorKey] != "500" {
		t.Errorf("the cursor is %q, want the mailbox's place at the watch, 500", h.kv[cursorKey])
	}
	g.historyID = "900"
	if got := p.invoke(h, "gmail.renew", map[string]any{}); got.status != "succeeded" || h.kv[cursorKey] != "500" {
		t.Errorf("a later renewal: %s, cursor %q — it must not move", got.line(), h.kv[cursorKey])
	}
}

// A renewal that fails says why, in the code the host reads, and stores nothing:
// silent expiry is the failure comparable systems report most.
func TestARenewalThatFailsSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		topic  string
		script map[int]step
		code   string
		says   string
	}{
		{"no topic set", "-", nil, argInvalid, "the topic setting is the Pub/Sub topic"},
		{"a topic that is not one", "aii-mail/gmail", nil, argInvalid, "projects/<project>/topics/<topic>"},
		{"the publisher not granted", topic, map[int]step{1: replies(gave(403, gerr(403, "forbidden", "User not authorized to perform this action."), -1))}, remoteFailed, "must grant publish to gmail-api-push@system.gserviceaccount.com"},
		{"a 400 naming the topic", topic, map[int]step{1: replies(gave(400, gerr(400, "invalidArgument", "Invalid topicName does not match projects/*/topics/*"), -1))}, remoteFailed, "Gmail refused the watch (400: Invalid topicName does not match projects/*/topics/*) — the watch was not renewed, and mail may stop"},
		{"rate limited", topic, map[int]step{1: replies(gave(429, gerr(429, "rateLimitExceeded", "slow down"), 20))}, remoteFailed, "asks to wait 20 s"},
		{"Gmail not reached", topic, map[int]step{1: replies(notWritten)}, remoteFailed, "Gmail was not reached"},
		{"an answer lost", topic, map[int]step{1: thenLost}, remoteFailed, "renewing again is safe"},
		{"an answer that shows no watch", topic, map[int]step{1: replies(gave(200, "{}", -1))}, remoteFailed, "does not show the watch"},
		{"the account disconnected", topic, map[int]step{1: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))}, "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page"},
	} {
		g := newGmail(t)
		g.historyID = "500"
		for n, s := range tc.script {
			g.at[n] = s
		}
		h := &standIn{t: t, handle: handle, gmail: g.handle, topic: tc.topic, kv: map[string]string{}}
		if tc.topic == "-" {
			h.topic = ""
		}
		got := start(t).invoke(h, "gmail.renew", map[string]any{})
		if got.status == "succeeded" || got.code != tc.code || !strings.Contains(got.said(), tc.says) {
			t.Errorf("%s: %s", tc.name, got.line())
		}
		if len(h.kv) != 0 {
			t.Errorf("%s: a failed renewal stored %v", tc.name, h.kv)
		}
	}
}

// Mail arrives once through the cursor, and the cursor moves only through what
// the host says it recorded: the same notification again offers it again.
func TestMailArrivesOnceAndTheCursorMovesOnlyThroughWhatTheHostRecorded(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "Thursday")
	g.arrive("102", "19am1000000002", "Friday")

	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	got := arrivalsOf(t, first)
	if want := []string{"19am1000000001", "19am1000000002"}; !reflect.DeepEqual(ids(got), want) || statusOf(t, first) != 200 {
		t.Fatalf("the first notification: %v (status %d)", ids(got), statusOf(t, first))
	}
	if got[0].From != recipient || !strings.Contains(got[0].Body, "Subject: Thursday") || !strings.Contains(got[0].Body, "text of Thursday") || strings.Contains(got[0].Body, "not verified") {
		t.Errorf("an arrival from a verified sender: %+v", got[0])
	}
	if h.kv[cursorKey] != "100" {
		t.Errorf("the cursor moved to %q before the host said it recorded anything", h.kv[cursorKey])
	}

	// The host refused them (recorded says so): they are offered again.
	again := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{})))
	if !reflect.DeepEqual(ids(again), ids(got)) || h.kv[cursorKey] != "100" {
		t.Errorf("after a refusal: %v, cursor %q", ids(again), h.kv[cursorKey])
	}
	// Half of them held is not enough.
	partial := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{"19am1000000001"})))
	if len(partial) != 2 || h.kv[cursorKey] != "100" {
		t.Errorf("with one of two held: %v, cursor %q", ids(partial), h.kv[cursorKey])
	}
	// All held: the cursor moves — to the mailbox's place, which is past the
	// last inbox record when other things have happened since — and there is
	// nothing new.
	g.historyID = "150"
	done := p.invoke(h, "gmail.notify", pushArgs([]string{"19am1000000001", "19am1000000002"}))
	if len(arrivalsOf(t, done)) != 0 || h.kv[cursorKey] != "150" || statusOf(t, done) != 200 {
		t.Errorf("once recorded: %v, cursor %q", ids(arrivalsOf(t, done)), h.kv[cursorKey])
	}
}

// A sender Gmail did not authenticate is not offered as their address, so no
// contact matches it; Gmail's word is its own header's, first, not one a sender
// wrote below it.
func TestASenderGmailDidNotAuthenticateIsNotOfferedAsTheirAddress(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "no header", field{"Received", "x"})
	g.arrive("102", "19am1000000002", "failed", field{"Authentication-Results", "mx.google.com; dmarc=fail (p=REJECT) header.from=example.com"})
	g.arrive("103", "19am1000000003", "forged", field{"Authentication-Results", "evil.example; dmarc=pass header.from=example.com"})
	g.arrive("104", "19am1000000004", "verified", field{"Authentication-Results", verifiedA})
	got := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{})))
	if len(got) != 4 {
		t.Fatalf("arrivals: %v", ids(got))
	}
	for i, a := range got[:3] {
		if a.From != "unverified:"+recipient || !strings.Contains(a.Body, "Sender not verified: Gmail did not report dmarc=pass") {
			t.Errorf("arrival %d (%s) is offered as %q: %s", i, a.ID, a.From, a.Body)
		}
	}
	if got[3].From != recipient || strings.Contains(got[3].Body, "not verified") {
		t.Errorf("the verified sender: %+v", got[3])
	}
}

// One call takes in at most six messages — each is a read inside a ninety-second
// invocation — and asks for the rest: Pub/Sub delivers a non-2xx again.
func TestOneCallTakesInAtMostSixMessagesAndAsksForMore(t *testing.T) {
	g, h, p := inbox(t)
	for i := 1; i <= 8; i++ {
		g.arrive(fmt.Sprint(100+i), fmt.Sprintf("19am10000000%02d", i), fmt.Sprintf("mail %d", i))
	}
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	got := arrivalsOf(t, first)
	if len(got) != maxArrivals || statusOf(t, first) != 503 {
		t.Fatalf("the first call took %d (status %d), want %d and 503 so Pub/Sub delivers again", len(got), statusOf(t, first), maxArrivals)
	}
	rest := p.invoke(h, "gmail.notify", pushArgs(ids(got)))
	more := arrivalsOf(t, rest)
	if want := []string{"19am1000000007", "19am1000000008"}; !reflect.DeepEqual(ids(more), want) || statusOf(t, rest) != 200 {
		t.Errorf("the rest: %v (status %d)", ids(more), statusOf(t, rest))
	}
	if h.kv[cursorKey] != "106" {
		t.Errorf("the cursor after the first six were recorded: %q, want 106", h.kv[cursorKey])
	}
}

// Only the inbox's new mail is offered: not what the mailbox sent, not drafts,
// and a message deleted since is skipped, not an error.
func TestOnlyNewInboxMailIsOffered(t *testing.T) {
	g, h, p := inbox(t)
	sent := g.put(&stored{id: "19am1000000001", thread: "t1", labels: []string{"SENT"}, fields: []field{{"From", me}}, payload: part("text/plain", "", nil, "mine")})
	g.history = append(g.history, histRec{"101", []string{sent.id}})
	draft := g.put(&stored{id: "19am1000000002", thread: "t2", labels: []string{"DRAFT"}, fields: []field{{"From", me}}, payload: part("text/plain", "", nil, "draft")})
	g.history = append(g.history, histRec{"102", []string{draft.id}})
	self := g.put(&stored{id: "19am1000000004", thread: "t4", labels: []string{"INBOX", "SENT"}, fields: []field{{"From", me}, {"To", me}}, payload: part("text/plain", "", nil, "to myself")})
	g.history = append(g.history, histRec{"103", []string{self.id}}) // mail to itself: an identity must not hear itself
	g.arrive("104", "19am1000000003", "real")
	g.history = append(g.history, histRec{"105", []string{"19am1000000099"}}) // deleted since: Gmail no longer has it
	g.historyID = "105"
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	got := arrivalsOf(t, first)
	if want := []string{"19am1000000003"}; !reflect.DeepEqual(ids(got), want) || statusOf(t, first) != 200 {
		t.Errorf("offered %v (status %d), want %v and 200 — a message deleted since is no reason to ask for more", ids(got), statusOf(t, first), want)
	}
}

// A message that cannot be read waits and the cursor stops before it: with mail
// already read the call returns that; with none it says why.
func TestAMessageThatCannotBeReadWaits(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "one")
	g.arrive("102", "19am1000000002", "two")
	g.at[3] = replies(backend) // the read of the second message: history is call 1, the first message 2
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	if got := arrivalsOf(t, first); len(got) != 1 || got[0].ID != "19am1000000001" || statusOf(t, first) != 503 {
		t.Errorf("with the second unreadable: %v (status %d), want the first and 503", ids(arrivalsOf(t, first)), statusOf(t, first))
	}
	next := p.invoke(h, "gmail.notify", pushArgs([]string{"19am1000000001"}))
	if got := arrivalsOf(t, next); len(got) != 1 || got[0].ID != "19am1000000002" || h.kv[cursorKey] != "101" {
		t.Errorf("the next call: %v, cursor %q", ids(arrivalsOf(t, next)), h.kv[cursorKey])
	}

	g, h, p = inbox(t)
	g.arrive("101", "19am1000000001", "one")
	g.at[2] = replies(backend)
	check(t, "no progress at all", p.invoke(h, "gmail.notify", pushArgs([]string{})), "failed", remoteFailed, "Gmail answered 500")
	if h.kv[cursorKey] != "100" {
		t.Errorf("a call that read nothing moved the cursor to %q", h.kv[cursorKey])
	}
}

// Gmail no longer holds the history a cursor names: the gap is said, the inbox's
// recent mail is found by search — at most six, the rest declared with the search
// that finds them — and a new cursor is set.
func TestAGoneHistoryIsSaidAndTheInboxsRecentMailIsSearched(t *testing.T) {
	g, h, p := inbox(t)
	g.gone = true
	g.historyID = "700"
	for i := 1; i <= 8; i++ {
		g.received(fmt.Sprintf("19am10000000%02d", i), fromRecipient(fmt.Sprintf("mail %d", i), field{"Authentication-Results", verifiedA}), fmt.Sprintf("text %d", i), nil)
	}
	got := p.invoke(h, "gmail.sync", map[string]any{"recorded": []any{}})
	list := arrivalsOf(t, got)
	if len(list) != 1+maxArrivals || list[0].ID != "gap-100" {
		t.Fatalf("the gap notice and %d messages: %v", maxArrivals, ids(list))
	}
	n := list[0].Body
	for _, want := range []string{"not a person's mail", "no longer holds this mailbox's history from cursor 100", "in:inbox newer_than:7d", "there is more than that, and it was not read: search Gmail for `in:inbox newer_than:7d`"} {
		if !strings.Contains(n, want) {
			t.Errorf("the notice does not say %q: %s", want, n)
		}
	}
	if list[0].From != me || list[1].ID != "19am1000000003" || list[maxArrivals].ID != "19am1000000008" {
		t.Errorf("arrivals go oldest first from the newest six: %v", ids(list))
	}
	if h.kv[cursorKey] != "100" {
		t.Errorf("the new cursor is set only once the host recorded them: kv %q", h.kv[cursorKey])
	}
	g.gone = false
	after := p.invoke(h, "gmail.sync", map[string]any{"recorded": ids(list)})
	if len(arrivalsOf(t, after)) != 0 || h.kv[cursorKey] != "700" {
		t.Errorf("once recorded, the cursor is the mailbox's place then: %q, arrivals %v", h.kv[cursorKey], ids(arrivalsOf(t, after)))
	}
}

// The scheduled sync is the same read from the cursor, answering arrivals alone.
func TestTheScheduledSyncFindsWhatANotificationMissed(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "dropped notification")
	got := p.invoke(h, "gmail.sync", map[string]any{"recorded": []any{}})
	if !reflect.DeepEqual(ids(arrivalsOf(t, got)), []string{"19am1000000001"}) || strings.Contains(string(got.result), `"response"`) {
		t.Errorf("sync: %s", got.result)
	}
}

// With no cursor nothing was ever watched: the mailbox's place is taken and mail
// from before it is not delivered.
func TestWithNoCursorThePluginTakesTheMailboxsPlace(t *testing.T) {
	g := newGmail(t)
	g.arrive("299", "19am1000000001", "from before")
	g.historyID = "300"
	h := &standIn{t: t, handle: handle, gmail: g.handle, topic: topic, kv: map[string]string{}}
	got := start(t).invoke(h, "gmail.sync", map[string]any{})
	if len(arrivalsOf(t, got)) != 0 || h.kv[cursorKey] != "300" {
		t.Errorf("no cursor: arrivals %v, cursor %q", ids(arrivalsOf(t, got)), h.kv[cursorKey])
	}
}

// A push this code cannot read is acknowledged and ignored — Pub/Sub would
// deliver it forever — and nothing is read.
func TestANotificationThatIsNotAPushIsAcknowledgedAndIgnored(t *testing.T) {
	g, h, p := inbox(t)
	for _, body := range []string{"", "not json", `{"message":{}}`} {
		args := pushArgs(nil)
		args["body"] = body
		got := p.invoke(h, "gmail.notify", args)
		if got.status != "succeeded" || statusOf(t, got) != 200 || len(arrivalsOf(t, got)) != 0 || !strings.Contains(string(got.result), "ignored") {
			t.Errorf("body %q: %s", body, got.result)
		}
	}
	if len(g.calls) != 0 {
		t.Errorf("an unreadable push made Gmail calls: %v", g.ops())
	}
}

// Without kv the plugin cannot keep its place, and says which grant is missing.
func TestWithoutKVThePluginSaysWhichGrantIsMissing(t *testing.T) {
	g, h, p := inbox(t)
	h.kv = nil
	g.arrive("101", "19am1000000001", "one")
	got := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	if got.status != "denied" || !strings.Contains(got.said(), "grant kv to this plugin on the Plugins page") {
		t.Errorf("no kv: %s", got.line())
	}
}

// An arrival's text is bounded, with the route to the rest.
func TestALongMessageIsBoundedWithTheRouteToTheRest(t *testing.T) {
	g, h, p := inbox(t)
	long := strings.Repeat("word ", 2000)
	g.received("19am1000000001", fromRecipient("long", field{"Authentication-Results", verifiedA}), "", part("text/plain", "", nil, long))
	g.history = append(g.history, histRec{"101", []string{"19am1000000001"}})
	g.historyID = "101"
	got := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{})))
	if len(got) != 1 || len(got[0].Body) > arrivalText+800 || !strings.Contains(got[0].Body, "more bytes: gmail.read message_id=19am1000000001 offset=") {
		t.Errorf("a %d byte message: %d bytes offered: …%s", len(long), len(got[0].Body), got[0].Body[max(0, len(got[0].Body)-160):])
	}
}

// A delivery report is parsed by RFC 3464 and classified by RFC 3463's first
// digit; one this code cannot read is passed on as text and said so.
func TestADeliveryReportIsClassifiedByItsStatusNeverGuessed(t *testing.T) {
	for _, tc := range []struct {
		name, dsn string
		says      []string
		notSays   []string
	}{
		{"a permanent failure", "Reporting-MTA: dns; googlemail.com\r\n\r\nFinal-Recipient: rfc822; recipient@example.net\r\nAction: failed\r\nStatus: 5.1.1\r\nDiagnostic-Code: smtp; 550 5.1.1 The email account that you tried to reach does not exist.\r\n",
			[]string{"Delivery report for «Thursday lunch»: the mail to recipient@example.net was NOT delivered — a permanent failure (status 5.1.1)", "The mail system said: smtp; 550 5.1.1 The email account that you tried to reach does not exist."}, []string{"could not classify"}},
		{"a transient delay", "Reporting-MTA: dns; googlemail.com\r\n\r\nFinal-Recipient: rfc822; recipient@example.net\r\nAction: delayed\r\nStatus: 4.4.1\r\nDiagnostic-Code: smtp; connection timed out\r\n",
			[]string{"the mail to recipient@example.net is delayed — the mail system is still trying (status 4.4.1)"}, []string{"NOT delivered"}},
		{"two recipients", "Reporting-MTA: dns; x\r\n\r\nFinal-Recipient: rfc822; a@example.net\r\nAction: failed\r\nStatus: 5.2.2\r\n\r\nFinal-Recipient: rfc822; b@example.net\r\nAction: delivered\r\nStatus: 2.0.0\r\n",
			[]string{"a@example.net was NOT delivered", "b@example.net was delivered to the recipient's mail system"}, nil},
		{"a folded diagnostic", "Reporting-MTA: dns; x\r\n\r\nFinal-Recipient: rfc822; a@example.net\r\nAction: failed\r\nStatus: 5.7.1\r\nDiagnostic-Code: smtp; 550-5.7.1 blocked\r\n 550 5.7.1 by policy\r\n",
			[]string{"The mail system said: smtp; 550-5.7.1 blocked 550 5.7.1 by policy"}, nil},
		{"a report that cannot be read", "Reporting-MTA: dns; googlemail.com\r\nArrival-Date: Thu, 1 Oct 2026 10:00:00 -0400\r\n",
			[]string{"A delivery report this plugin could not classify (no recipient with a status could be read)"}, []string{"NOT delivered", "was delivered"}},
	} {
		g, h, p := inbox(t)
		payload := part("multipart/report", "", []field{{"Content-Type", "multipart/report; report-type=delivery-status"}}, "",
			part("text/plain", "", nil, "Delivery incomplete"),
			part("message/delivery-status", "", nil, tc.dsn),
			part("text/rfc822-headers", "", nil, "To: recipient@example.net\r\nSubject: Thursday lunch\r\n"))
		g.received("19am1000000001", []field{{"From", "Mail Delivery Subsystem <mailer-daemon@googlemail.com>"}, {"To", me}, {"Subject", "Delivery Status Notification (Failure)"}, {"Date", "Thu, 1 Oct 2026 10:00:00 -0400"}, {"Authentication-Results", verifiedA}}, "Delivery incomplete", payload)
		g.history = append(g.history, histRec{"101", []string{"19am1000000001"}})
		g.historyID = "101"
		got := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{})))
		if len(got) != 1 || got[0].From != "mailer-daemon@googlemail.com" {
			t.Errorf("%s: %+v", tc.name, got)
			continue
		}
		for _, s := range tc.says {
			if !strings.Contains(got[0].Body, s) {
				t.Errorf("%s: the arrival does not say %q:\n%s", tc.name, s, got[0].Body)
			}
		}
		for _, s := range tc.notSays {
			if strings.Contains(got[0].Body, s) {
				t.Errorf("%s: the arrival says %q:\n%s", tc.name, s, got[0].Body)
			}
		}
	}
}

// A channel send is a mail to a contact — a draft sent by its id, as a tool's
// send is — with no card: the contact is the consent. Its subject is the body's
// first line.
func TestAChannelSendMailsAContactWithoutACard(t *testing.T) {
	g, h, p := inbox(t)
	got := p.invoke(h, "send", map[string]any{"address": recipient, "body": "\n  Thursday at ten still suits me.  \nIdentity"})
	if got.status != "succeeded" || !strings.HasPrefix(got.text("receipt"), "gmail:19a") {
		t.Fatalf("send: %s", got.line())
	}
	sent := g.labelled("SENT")
	if len(sent) != 1 || sent[0].header("To") != recipient || strings.TrimSpace(readLetter(t, sent[0].raw).subject(t)) != "Thursday at ten still suits me." {
		t.Errorf("the mail: %+v", sent)
	}
	if want := []string{"draft", "send", "get"}; !reflect.DeepEqual(g.ops(), want) {
		t.Errorf("Gmail was asked %v, want %v", g.ops(), want)
	}
	long := "x " + strings.Repeat("y", 200)
	p.invoke(h, "send", map[string]any{"address": recipient, "body": long})
	if s := readLetter(t, g.newest().raw).subject(t); len([]rune(s)) != 78 || !strings.HasSuffix(s, "…") {
		t.Errorf("a long first line is a subject of %d characters: %q", len([]rune(s)), s)
	}
	for name, args := range map[string]map[string]any{
		"not an address": {"address": "recipient", "body": "hi"},
		"a header":       {"address": recipient + "\r\nBcc: attacker@evil.example", "body": "hi"},
		"no body":        {"address": recipient, "body": "  \n"},
	} {
		if r := p.invoke(h, "send", args); r.status != "failed" || r.code != argInvalid || !strings.Contains(r.said(), "nothing was sent") {
			t.Errorf("%s: %s", name, r.line())
		}
	}
	// A refusal is the tool send's own: nothing accepted is NET_REMOTE_FAILED.
	g2, h2, p2 := inbox(t)
	g2.at[2] = replies(gave(403, gerr(403, "dailyLimitExceeded", "Daily Limit Exceeded"), -1))
	if r := p2.invoke(h2, "send", map[string]any{"address": recipient, "body": "hi"}); r.status != "failed" || r.code != remoteFailed || !strings.Contains(r.said(), "nothing was sent") {
		t.Errorf("a refused send: %s", r.line())
	}
}

// The backfill starts after the last message the plugin took in, so mail it
// already handed over is not searched for again.
func TestTheBackfillStartsAfterTheLastMessageTakenIn(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "taken in")
	got := arrivalsOf(t, p.invoke(h, "gmail.sync", map[string]any{"recorded": []any{}}))
	g.gone = true
	g.historyID = "800"
	again := arrivalsOf(t, p.invoke(h, "gmail.sync", map[string]any{"recorded": ids(got)}))
	if len(again) == 0 || !strings.Contains(again[0].Body, "in:inbox after:1790000000") || !strings.Contains(again[0].Body, "since the last message this plugin took in") {
		t.Errorf("the gap notice after a message was taken in: %v", again)
	}
	if h.kv[lastKey] != "1790000000000" {
		t.Errorf("the newest message's time is %q", h.kv[lastKey])
	}
}

// A history record that adds more than one call takes in cannot be finished in
// pieces: the first six are offered, the rest is said not to have been read, with
// the search that finds it, and the record is done — it would otherwise stall the
// cursor for ever.
func TestOneRecordOverTheBoundIsCutAndSaid(t *testing.T) {
	g, h, p := inbox(t)
	var added []string
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("19am10000000%02d", i)
		g.received(id, fromRecipient(fmt.Sprintf("bulk %d", i), field{"Authentication-Results", verifiedA}), "x", nil)
		added = append(added, id)
	}
	g.history = append(g.history, histRec{"101", added})
	g.historyID = "101"
	got := arrivalsOf(t, p.invoke(h, "gmail.notify", pushArgs([]string{})))
	if len(got) != maxArrivals+1 || got[maxArrivals].ID != "cut-101" || !strings.Contains(got[maxArrivals].Body, "added 9 messages to the inbox at once") || !strings.Contains(got[maxArrivals].Body, "3 of them were not read") || !strings.Contains(got[maxArrivals].Body, "in:inbox") {
		t.Fatalf("the cut record: %v", ids(got))
	}
	done := p.invoke(h, "gmail.notify", pushArgs(ids(got)))
	if len(arrivalsOf(t, done)) != 0 || h.kv[cursorKey] != "101" {
		t.Errorf("the record is done, not read again: arrivals %v, cursor %q", ids(arrivalsOf(t, done)), h.kv[cursorKey])
	}
}

// A call finishes whole records: with two messages a record the first call takes
// three, and the fourth waits for the next.
func TestACallDoesNotSplitARecord(t *testing.T) {
	g, h, p := inbox(t)
	for r := 1; r <= 4; r++ {
		a, b := fmt.Sprintf("19am10000000%d1", r), fmt.Sprintf("19am10000000%d2", r)
		g.received(a, fromRecipient("a", field{"Authentication-Results", verifiedA}), "x", nil)
		g.received(b, fromRecipient("b", field{"Authentication-Results", verifiedA}), "x", nil)
		g.history = append(g.history, histRec{fmt.Sprint(100 + r), []string{a, b}})
		g.historyID = fmt.Sprint(100 + r)
	}
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	got := arrivalsOf(t, first)
	if len(got) != 6 || statusOf(t, first) != 503 {
		t.Fatalf("the first call: %v (status %d)", ids(got), statusOf(t, first))
	}
	rest := p.invoke(h, "gmail.notify", pushArgs(ids(got)))
	if more := arrivalsOf(t, rest); !reflect.DeepEqual(ids(more), []string{"19am1000000041", "19am1000000042"}) || statusOf(t, rest) != 200 || h.kv[cursorKey] != "103" {
		t.Errorf("the rest: %v (status %d), cursor %q", ids(more), statusOf(t, rest), h.kv[cursorKey])
	}
}

// More history than one page holds: what the page names is offered, the cursor
// stops at the last record read, and Pub/Sub is asked to deliver again.
func TestMoreHistoryThanOnePageAsksForMore(t *testing.T) {
	g, h, p := inbox(t)
	g.arrive("101", "19am1000000001", "one")
	g.pageMore = true
	g.historyID = "500"
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	if statusOf(t, first) != 503 || len(arrivalsOf(t, first)) != 1 {
		t.Fatalf("a page with more behind it: %d arrivals (status %d)", len(arrivalsOf(t, first)), statusOf(t, first))
	}
	g.pageMore = false
	next := p.invoke(h, "gmail.notify", pushArgs([]string{"19am1000000001"}))
	if len(arrivalsOf(t, next)) != 0 || h.kv[cursorKey] != "500" || statusOf(t, next) != 200 {
		t.Errorf("the last page: arrivals %v, cursor %q", ids(arrivalsOf(t, next)), h.kv[cursorKey])
	}
}

// A call that cannot take a whole record leaves it for the next: two records of
// four messages — the first call takes the first, not a piece of the second.
func TestARecordThatDoesNotFitWaitsWhole(t *testing.T) {
	g, h, p := inbox(t)
	for r := 1; r <= 2; r++ {
		var added []string
		for i := 1; i <= 4; i++ {
			id := fmt.Sprintf("19am1000000%d%d", r, i)
			g.received(id, fromRecipient("m", field{"Authentication-Results", verifiedA}), "x", nil)
			added = append(added, id)
		}
		g.history = append(g.history, histRec{fmt.Sprint(100 + r), added})
		g.historyID = fmt.Sprint(100 + r)
	}
	first := p.invoke(h, "gmail.notify", pushArgs([]string{}))
	got := arrivalsOf(t, first)
	if len(got) != 4 || statusOf(t, first) != 503 || strings.Contains(fmt.Sprint(got), "cut-") {
		t.Fatalf("the first call: %v (status %d), want the first record whole and 503", ids(got), statusOf(t, first))
	}
}

// A channel's sends are bounded: send_limit in any send_window_minutes, both
// settings the operator sets on the card. The send over the limit is refused
// before Gmail is asked, in the code the host reads as nothing accepted, saying
// which limit, where to change it and when the next may go; a send Gmail refused
// took no place, one whose outcome is unknown took one, and the operator's own
// confirmed send is not the channel's.
func TestAChannelSendIsBoundedByItsWindow(t *testing.T) {
	g, h, p := inbox(t)
	h.extra = map[string]any{"send_limit": 2, "send_window_minutes": 10}
	mail := func(ms int64, body string) reply {
		return p.invoke(h, "send", map[string]any{"address": recipient, "body": body, "_host_now_ms": ms})
	}
	const min = 60_000
	if r := mail(callClock, "one"); r.status != "succeeded" {
		t.Fatalf("the first: %s", r.line())
	}
	// Gmail refuses the second: it took no place.
	g.at[len(g.calls)+2] = replies(gave(403, gerr(403, "dailyLimitExceeded", "Daily Limit Exceeded"), -1))
	if r := mail(callClock+min, "refused by Gmail"); r.status != "failed" {
		t.Fatalf("Gmail refusing: %s", r.line())
	}
	if r := mail(callClock+2*min, "two"); r.status != "succeeded" {
		t.Fatalf("the second that went: %s", r.line())
	}
	before := len(g.calls)
	r := mail(callClock+3*min, "three")
	leaves := time.UnixMilli(callClock + 10*min).UTC().Format(time.RFC3339) // the first send leaves the window
	check(t, "the third", r, "failed", remoteFailed, "this channel has sent 2 in the last 10 minutes, its limit — the operator sets it as this plugin's send_limit and send_window_minutes settings; the next send can go at "+leaves+" — nothing was sent")
	if len(g.calls) != before || len(g.labelled("SENT")) != 2 {
		t.Errorf("the refused send asked Gmail %v, and %d mails left", g.ops()[before:], len(g.labelled("SENT")))
	}
	// The window slides: when the first has left, one more may go.
	if r := mail(callClock+10*min+1, "four"); r.status != "succeeded" {
		t.Errorf("when the first has left the window: %s", r.line())
	}
	// The operator's own send is confirmed one by one and is not counted.
	if r := p.invoke(h, "gmail.send", act(thursday(map[string]any{"_host_now_ms": callClock + 10*min + 2}))); r.status != "succeeded" {
		t.Errorf("the operator's confirmed send: %s", r.line())
	}
}

// A send whose outcome is not known took a place: it may have gone.
func TestASendThatMayHaveGoneTakesAPlaceInTheWindow(t *testing.T) {
	g, h, p := inbox(t)
	h.extra = map[string]any{"send_limit": 1, "send_window_minutes": 10}
	g.at[2] = thenLost // the draft's send, whose answer is lost: the delete then finds it gone
	first := p.invoke(h, "send", map[string]any{"address": recipient, "body": "one"})
	if first.status != "succeeded" || !strings.Contains(first.said(), "no longer holds its draft") {
		t.Fatalf("a send whose answer was lost and whose draft is gone: %s", first.line())
	}
	g2, h2, p2 := inbox(t)
	h2.extra = map[string]any{"send_limit": 1, "send_window_minutes": 10}
	g2.at[2] = thenLost
	g2.at[3] = replies(backend) // and its settling delete fails: unknown
	if r := p2.invoke(h2, "send", map[string]any{"address": recipient, "body": "one"}); r.status != "failed" || r.code != effectUnknown {
		t.Fatalf("unknown: %s", r.line())
	}
	if r := p2.invoke(h2, "send", map[string]any{"address": recipient, "body": "two"}); r.status != "failed" || !strings.Contains(r.said(), "its limit") {
		t.Errorf("after an unknown send the window is full: %s", r.line())
	}
}

// The window fails closed and says which requirement: no storage, no clock.
func TestAChannelSendWithoutItsWindowSendsNothing(t *testing.T) {
	g, h, p := inbox(t)
	h.kv = nil
	if r := p.invoke(h, "send", map[string]any{"address": recipient, "body": "hi"}); r.status != "denied" || !strings.Contains(r.said(), "grant kv to this plugin on the Plugins page") {
		t.Errorf("no kv: %s", r.line())
	}
	if len(g.calls) != 0 {
		t.Errorf("a send whose window could not be kept asked Gmail %v", g.ops())
	}
}
