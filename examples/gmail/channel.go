package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	channelName = "gmail"

	maxArrivals = 6

	arrivalText = 4 << 10

	maxDSNRecipients = 10

	cursorKey  = "gmail.cursor"
	pendingKey = "gmail.pending"
	lastKey    = "gmail.last_ms"
)

type pendingBatch struct {
	IDs  []string `json:"ids"`
	Next string   `json:"next"`
}

type mailState struct {
	cursor string
	pend   pendingBatch
	lastMS int64
}

func kvFailed(err error) error {
	if d, ok := sdk.AsDenied(err); ok {
		return sdk.Deny(d.ReasonCode, "the plugin's storage is not granted here — grant kv to this plugin on the Plugins page; it keeps the place in the mailbox that mail has been handed over up to")
	}
	return sdk.Fail(remoteFailed, "the plugin's storage could not be read or written: "+err.Error())
}

func loadState() (mailState, error) {
	var s mailState
	if v, ok, err := sdk.KV.Get(cursorKey); err != nil {
		return s, kvFailed(err)
	} else if ok {
		s.cursor = v
	}
	if v, ok, err := sdk.KV.Get(pendingKey); err != nil {
		return s, kvFailed(err)
	} else if ok && v != "" {
		if json.Unmarshal([]byte(v), &s.pend) != nil {
			s.pend = pendingBatch{}
		}
	}
	if v, ok, err := sdk.KV.Get(lastKey); err != nil {
		return s, kvFailed(err)
	} else if ok {
		s.lastMS, _ = strconv.ParseInt(v, 10, 64)
	}
	return s, nil
}

func (s mailState) save() error {
	pend, _ := json.Marshal(s.pend)
	for _, kv := range [][2]string{{cursorKey, s.cursor}, {pendingKey, string(pend)}, {lastKey, strconv.FormatInt(s.lastMS, 10)}} {
		if _, err := sdk.KV.Put(kv[0], kv[1]); err != nil {
			return kvFailed(err)
		}
	}
	return nil
}

func (s *mailState) confirm(recorded []string) {
	if s.pend.Next == "" {
		return
	}
	held := map[string]bool{}
	for _, id := range recorded {
		held[id] = true
	}
	for _, id := range s.pend.IDs {
		if !held[id] {
			return
		}
	}
	s.cursor, s.pend = s.pend.Next, pendingBatch{}
}

func recordedIn(a sdk.Object) []string {
	list, _ := a.StringArray("recorded")
	return list
}

func describe(sdk.Call) (any, error) {
	return sdk.ChannelDescription{Channel: channelName, Receive: "webhook", Acknowledges: true}.Value(), nil
}

func receive(sdk.Call) (any, error) {
	return nil, sdk.Fail("OPERATION_NOT_SUPPORTED", "this adapter receives by webhook and by its scheduled sync; describe says so and the host does not poll it")
}

func channelSend(c sdk.Call) (any, error) {
	address, _ := c.Args().String("address")
	body, _ := c.Args().String("body")
	if !isAddress(address) {
		return nil, sdk.Fail(argInvalid, "address is one plain email address, name@example.com — "+nothingSent)
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(body) == "" || len(body) > maxBody {
		return nil, sdk.Fail(argInvalid, fmt.Sprintf("the body is 1 to %d bytes — %s", maxBody, nothingSent))
	}
	m := mail{to: []string{address}, subject: subjectOf(body), body: body}
	var err error
	if m.handle, err = settings(nothingSent); err != nil {
		return nil, err
	}

	v, err := sends.Admit(c)
	if err != nil {
		return nil, err
	}
	if !v.OK {
		return nil, v.Refusal(nothingSent)
	}
	out, err := m.sendDraft()
	var kept error
	if sends.Counts(err) {
		kept = sends.Record(c, v)
	}
	if err != nil {
		return nil, err
	}
	receipt, _ := out.(map[string]any)
	id, _ := receipt["message_id"].(string)
	effect := fmt.Sprint(receipt["confirmation"])
	if kept != nil {
		effect += "; the send window could not be updated (" + kept.Error() + ")"
	}
	return map[string]any{"receipt": "gmail:" + id, "effect": effect}, nil
}

var sends = sdk.SendWindow{Key: "gmail.sends"}

func subjectOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.Map(func(r rune) rune {
			if r < ' ' || r == 0x7f {
				return -1
			}
			return r
		}, strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 78 {
			line = string([]rune(line)[:77]) + "…"
		}
		return line
	}
	return "(no subject)"
}

func renew(sdk.Call) (any, error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return nil, sdk.Fail(argInvalid, "the plugin's settings could not be read — the watch was not renewed")
	}
	handle, _ := vals.Handle("google")
	topic, _ := vals.String("topic")
	_, _, okTopic := topicParts(topic)
	switch {
	case handle == "":
		return nil, sdk.Fail(argInvalid, "the google handle is not set — connect a Google account with the gmail service on the Plugins page; the watch was not renewed")
	case !okTopic:
		return nil, sdk.Fail(argInvalid, "the topic setting is the Pub/Sub topic Gmail publishes to, projects/<project>/topics/<topic>; it is not set or is not one — the watch was not renewed")
	}
	expiration, err := startWatch(handle, topic, "the watch was not renewed, and mail may stop arriving when the current one lapses")
	if err != nil {
		return nil, err
	}
	return map[string]any{"expiration": expiration}, nil
}

func startWatch(handle, topic, not string) (string, error) {
	body, _ := json.Marshal(map[string]any{"topicName": topic, "labelIds": []string{"INBOX"}, "labelFilterBehavior": "INCLUDE"})
	res, err := sdk.HTTP.Post(api+"/watch", writing(handle, string(body)))
	switch ended(res, err) {
	case refused:
		return "", refusal(err, not)
	case unreached:
		return "", sdk.Fail(remoteFailed, "Gmail was not reached — "+not)
	case wasLost, unread:
		return "", sdk.Fail(remoteFailed, why(res, err)+" — "+not+"; renewing again is safe")
	}
	switch s := res.Status; {
	case s == 403:
		return "", sdk.Fail(remoteFailed, forbidden(res, readScope)+"; if it names the topic, it must grant publish to gmail-api-push@system.gserviceaccount.com — "+not)
	case s == 429:
		return "", sdk.Fail(remoteFailed, "Gmail refused it ("+gmailSaid(res)+") and "+wait(res)+" — "+not)
	case s >= 400:
		return "", sdk.Fail(remoteFailed, "Gmail refused the watch ("+gmailSaid(res)+") — "+not)
	}
	answer := sdk.Object(res.Body)
	history, expiration := str(answer, "historyId"), str(answer, "expiration")
	if history == "" {
		return "", sdk.Fail(remoteFailed, fmt.Sprintf("Gmail answered %d and the answer does not show the watch — %s", res.Status, not))
	}
	st, err := loadState()
	if err != nil {
		return "", err
	}
	if st.cursor == "" {
		st.cursor = history
		if err := st.save(); err != nil {
			return "", err
		}
	}
	return expiration, nil
}

func notify(c sdk.Call) (any, error) {
	req := sdk.ParseWebhook(c)
	var push struct {
		Message struct {
			Data string `json:"data"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(req.Body), &push); err != nil || push.Message.Data == "" {
		return sdk.WebhookResult{Response: &sdk.WebhookResponse{Status: 200, ContentType: "text/plain", Body: "ignored: not a Pub/Sub push"}}.Value(), nil
	}
	handle, err := settings(nothingRead)
	if err != nil {
		return nil, err
	}
	got, err := syncMailbox(handle, recordedIn(c.Args()))
	if err != nil {
		return nil, err
	}
	status, said := 200, "ok"
	if got.more {

		status, said = 503, "more mail waits"
	}
	return sdk.WebhookResult{Arrivals: got.arrivals, Response: &sdk.WebhookResponse{Status: status, ContentType: "text/plain", Body: said}}.Value(), nil
}

func syncOp(c sdk.Call) (any, error) {
	handle, err := settings(nothingRead)
	if err != nil {
		return nil, err
	}
	got, err := syncMailbox(handle, recordedIn(c.Args()))
	if err != nil {
		return nil, err
	}
	return sdk.WebhookResult{Arrivals: got.arrivals}.Value(), nil
}

type taken struct {
	arrivals []sdk.Arrival
	more     bool
}

func syncMailbox(handle string, recorded []string) (taken, error) {
	st, err := loadState()
	if err != nil {
		return taken{}, err
	}
	st.confirm(recorded)
	if st.cursor == "" {

		who, err := profile(handle)
		if err != nil {
			return taken{}, err
		}
		st.cursor = who.history
		return taken{}, st.save()
	}
	records, current, more, code, err := historySince(handle, st.cursor)
	switch {
	case err != nil:
		return taken{}, err
	case code == 404:
		return st.rebaseline(handle)
	}
	var out taken
	next := st.cursor
	var stopped error
walk:
	for _, r := range records {
		room := maxArrivals - len(out.arrivals)
		if len(r.ids) > room && len(out.arrivals) > 0 {
			out.more = true
			break
		}
		take, cut := r.ids, 0
		if len(take) > room {
			take, cut = take[:room], len(take)-room
		}
		for _, id := range take {
			a, skip, err := arrivalOf(handle, id, &st)
			if err != nil {
				out.more, stopped = true, err
				break walk
			}
			if !skip {
				out.arrivals = append(out.arrivals, a)
			}
		}
		if cut > 0 {
			n, err := noticeArrival(handle, "cut-"+r.id, fmt.Sprintf("Gmail's history record %s added %d messages to the inbox at once, more than one call takes in: %d of them were not read. Search Gmail for `in:inbox` to find them.", r.id, len(r.ids), cut))
			if err != nil {
				out.more, stopped = true, err
				break walk
			}
			out.arrivals = append(out.arrivals, n)
		}
		next = r.id
	}
	if stopped != nil && len(out.arrivals) == 0 {
		return taken{}, stopped
	}
	if !out.more && stopped == nil {
		if more {
			out.more = true
		} else if current != "" {
			next = current
		}
	}
	if len(out.arrivals) == 0 {
		st.cursor, st.pend = next, pendingBatch{}
	} else {
		ids := make([]string, 0, len(out.arrivals))
		for _, a := range out.arrivals {
			ids = append(ids, a.ID)
		}
		st.pend = pendingBatch{IDs: ids, Next: next}
	}
	return out, st.save()
}

type record struct {
	id  string
	ids []string
}

func historySince(handle, cursor string) (records []record, current string, more bool, code int, err error) {
	q := url.Values{}
	q.Set("startHistoryId", cursor)
	q.Set("historyTypes", "messageAdded")
	q.Set("labelId", "INBOX")
	q.Set("maxResults", "100")
	res, herr := sdk.HTTP.Get(api+"/history?"+q.Encode(), reading(handle, listTimeout))
	switch e := ended(res, herr); {
	case e == refused:
		return nil, "", false, 0, refusal(herr, nothingRead)
	case e != answered:
		return nil, "", false, 0, sdk.Fail(remoteFailed, "Gmail's history could not be read ("+why(res, herr)+") — nothing was read; the cursor is unchanged")
	case res.Status == 404:
		return nil, "", false, 404, nil
	case res.Status == 403:
		return nil, "", false, 0, sdk.Fail(remoteFailed, forbidden(res, readScope)+" — nothing was read")
	case res.Status != 200 || !isObject(res.Body):
		return nil, "", false, 0, sdk.Fail(remoteFailed, "Gmail answered "+gmailSaid(res)+" — nothing was read; the cursor is unchanged")
	}
	body := sdk.Object(res.Body)
	list, _ := sdk.ObjectArray(body.Raw("history"))
	for _, h := range list {
		r := record{id: str(h, "id")}
		added, _ := sdk.ObjectArray(h.Raw("messagesAdded"))
		for _, a := range added {
			m := a.Object("message")
			labels := labelsOf(m)

			if id := str(m, "id"); isID(id) && hasLabel(labels, "INBOX") && !hasLabel(labels, "SENT") {
				r.ids = append(r.ids, id)
			}
		}
		if isID(r.id) {
			records = append(records, r)
		}
	}
	return records, str(body, "historyId"), str(body, "nextPageToken") != "", 200, nil
}

func noticeArrival(handle, id, text string) (sdk.Arrival, error) {
	who, err := profile(handle)
	if err != nil {
		return sdk.Arrival{}, err
	}
	return sdk.Arrival{ID: id, From: who.address, Body: "[a notice from the gmail plugin, not a person's mail] " + text}, nil
}

func arrivalOf(handle, id string, st *mailState) (a sdk.Arrival, skip bool, err error) {
	res, ferr := fetch(handle, id, fullTimeout)
	if ended(res, ferr) == answered && (res.Status == 404 || res.Status == 400) {
		return a, true, nil
	}
	if f := readFailed(res, ferr, nothingRead); f != nil {
		return a, false, f
	}
	m := sdk.Object(res.Body)
	if ms, _ := strconv.ParseInt(str(m, "internalDate"), 10, 64); ms > st.lastMS {
		st.lastMS = ms
	}
	return arrivalFrom(m), false, nil
}

func arrivalFrom(m sdk.Object) sdk.Arrival {
	head := m.Object("payload")
	var b parts
	b.walk(head, 0)
	text, _ := b.text()
	fromHeader := textHeader(head, "From")
	from := addressIn(fromHeader)
	if from == "" {
		from = "unknown-sender"
	}
	verified := dmarcPass(head)
	var w strings.Builder
	w.WriteString("From: " + clip(fromHeader, readChars) + "\n")
	w.WriteString("Subject: " + clip(textHeader(head, "Subject"), readChars) + "\n")
	w.WriteString("Date: " + clip(rawHeader(head, "Date"), 100) + "\n")
	if !verified {
		w.WriteString("Sender not verified: Gmail did not report dmarc=pass for this mail, so its From may be forged.\n")
		from = "unverified:" + from
	}
	if b.count > 0 {
		names := []string{}
		for _, at := range b.attachments {
			if o, ok := at.(map[string]any); ok {
				names = append(names, fmt.Sprint(o["name"]))
			}
		}
		w.WriteString(fmt.Sprintf("Attachments: %d, not fetched (%s)\n", b.count, clip(strings.Join(names, ", "), 300)))
	}
	w.WriteString("\n")
	if report := deliveryReport(head, &b); report != "" {
		w.WriteString(report)
	} else {
		w.WriteString(boundedText(text, str(m, "id")))
	}
	return sdk.Arrival{ID: str(m, "id"), From: from, Body: w.String()}
}

func boundedText(text, id string) string {
	if len(text) <= arrivalText {
		return text
	}
	end := arrivalText
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + fmt.Sprintf("\n[… %d more bytes: gmail.read message_id=%s offset=%d reads on]", len(text)-end, id, end)
}

func addressIn(header string) string {
	s := strings.TrimSpace(header)
	if i, j := strings.LastIndexByte(s, '<'), strings.LastIndexByte(s, '>'); i >= 0 && j > i {
		s = s[i+1 : j]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if isAddress(s) {
		return s
	}
	return ""
}

func dmarcPass(head sdk.Object) bool {
	list, _ := sdk.ObjectArray(head.Raw("headers"))
	for _, h := range list {
		if n, _ := h.String("name"); !strings.EqualFold(n, "Authentication-Results") {
			continue
		}
		v, _ := h.String("value")
		v = strings.ToLower(strings.TrimSpace(v))
		if id, _, _ := strings.Cut(v, ";"); strings.TrimSpace(id) == "mx.google.com" {
			return strings.Contains(v, "dmarc=pass")
		}
	}
	return false
}

func deliveryReport(head sdk.Object, b *parts) string {
	mt := strings.ToLower(rawHeader(head, "Content-Type"))
	if !strings.Contains(mt, "delivery-status") && b.dsn == nil {
		return ""
	}
	if b.dsn == nil {
		return "A delivery report this plugin could not classify (its delivery-status part is missing); read as ordinary text is all there is — gmail.read shows it.\n"
	}
	text, _ := partText(b.dsn)
	groups := dsnGroups(text)
	subject := originalSubject(b)
	var lines []string
	for _, g := range groups {
		rcpt := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(g["final-recipient"]), "rfc822;"))
		if rcpt == "" {
			rcpt = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(g["original-recipient"]), "rfc822;"))
		}
		status, action := strings.TrimSpace(g["status"]), strings.ToLower(strings.TrimSpace(g["action"]))
		if rcpt == "" || (status == "" && action == "") {
			continue
		}
		verdict := "could not be classified"
		switch {
		case strings.HasPrefix(status, "5"):
			verdict = "was NOT delivered — a permanent failure"
		case strings.HasPrefix(status, "4"):
			verdict = "is delayed — the mail system is still trying"
		case strings.HasPrefix(status, "2"):
			verdict = "was delivered to the recipient's mail system"
		case action == "failed":
			verdict = "was NOT delivered"
		case action == "delayed":
			verdict = "is delayed — the mail system is still trying"
		case action == "delivered" || action == "relayed" || action == "expanded":
			verdict = "was handed on"
		}
		line := "Delivery report" + forSubject(subject) + ": the mail to " + clip(rcpt, 254) + " " + verdict
		if status != "" {
			line += " (status " + clip(status, 20) + ")"
		}
		if d := strings.TrimSpace(g["diagnostic-code"]); d != "" {
			line += ". The mail system said: " + clip(d, 300)
		}
		lines = append(lines, line+".")
		if len(lines) == maxDSNRecipients {
			break
		}
	}
	if len(lines) == 0 {
		return "A delivery report this plugin could not classify (no recipient with a status could be read); gmail.read shows it as text.\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func forSubject(s string) string {
	if s == "" {
		return ""
	}
	return " for «" + clip(s, 200) + "»"
}

func dsnGroups(text string) []map[string]string {
	var out []map[string]string
	for _, block := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		g := map[string]string{}
		last := ""
		for _, line := range strings.Split(block, "\n") {
			if line == "" {
				continue
			}
			if (line[0] == ' ' || line[0] == '\t') && last != "" {
				g[last] += " " + strings.TrimSpace(line)
				continue
			}
			if k, v, ok := strings.Cut(line, ":"); ok {
				last = strings.ToLower(strings.TrimSpace(k))
				g[last] = strings.TrimSpace(v)
			}
		}
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func originalSubject(b *parts) string {
	if b.original == nil {
		return ""
	}
	text, _ := partText(b.original)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "subject") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type who struct{ address, history string }

func profile(handle string) (who, error) {
	res, err := sdk.HTTP.Get(api+"/profile?fields=emailAddress,historyId", reading(handle, headTimeout))
	if e := ended(res, err); e == refused {
		return who{}, refusal(err, nothingRead)
	} else if e != answered || res.Status != 200 || !isObject(res.Body) {
		return who{}, sdk.Fail(remoteFailed, "the mailbox's place could not be read ("+why(res, err)+") — nothing was read")
	}
	p := sdk.Object(res.Body)
	if str(p, "historyId") == "" {
		return who{}, sdk.Fail(remoteFailed, "Gmail's profile names no history id — nothing was read")
	}
	return who{address: str(p, "emailAddress"), history: str(p, "historyId")}, nil
}

func (st *mailState) rebaseline(handle string) (taken, error) {
	now, err := profile(handle)
	if err != nil {
		return taken{}, err
	}
	q, since := "in:inbox newer_than:7d", "the last week"
	if st.lastMS > 0 {
		q, since = fmt.Sprintf("in:inbox after:%d", st.lastMS/1000), "the last message this plugin took in"
	}
	params := url.Values{}
	params.Set("q", q)
	params.Set("maxResults", strconv.Itoa(maxArrivals+1))
	res, herr := sdk.HTTP.Get(api+"/messages?"+params.Encode(), reading(handle, listTimeout))
	if e := ended(res, herr); e == refused {
		return taken{}, refusal(herr, nothingRead)
	} else if e != answered || res.Status != 200 || !isObject(res.Body) {
		return taken{}, sdk.Fail(remoteFailed, "the inbox's recent mail could not be listed ("+why(res, herr)+") — the cursor is unchanged")
	}
	hits, _ := sdk.ObjectArray(sdk.Object(res.Body).Raw("messages"))
	found := len(hits)
	if found > maxArrivals {
		hits = hits[:maxArrivals]
	}
	var out taken
	for i := len(hits) - 1; i >= 0; i-- {
		id := str(hits[i], "id")
		if !isID(id) {
			continue
		}
		a, skip, err := arrivalOf(handle, id, st)
		if err != nil {
			return taken{}, err
		}
		if !skip {
			out.arrivals = append(out.arrivals, a)
		}
	}
	notice := fmt.Sprintf("[a notice from the gmail plugin, not a person's mail] Gmail no longer holds this mailbox's history from cursor %s (it keeps about a week), so mail could not be read in order. The inbox's mail since %s was searched for instead (%s): %d message(s) follow", st.cursor, since, q, len(out.arrivals))
	if found > maxArrivals {
		notice += fmt.Sprintf(" — there is more than that, and it was not read: search Gmail for `%s` to find it", q)
	}
	out.arrivals = append([]sdk.Arrival{{ID: "gap-" + st.cursor, From: now.address, Body: notice + "."}}, out.arrivals...)
	ids := make([]string, 0, len(out.arrivals))
	for _, a := range out.arrivals {
		ids = append(ids, a.ID)
	}
	st.pend = pendingBatch{IDs: ids, Next: now.history}
	return out, st.save()
}

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
