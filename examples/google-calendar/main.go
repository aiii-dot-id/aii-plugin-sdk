package main

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	api  = "https://www.googleapis.com/calendar/v3"
	host = "net.outbound:www.googleapis.com:443"

	exchange = 15000

	dayPage, dayPages   = 100, 3
	nextPages           = 3
	freePage, freePages = 250, 4

	freeSpan = 14 * 24 * time.Hour

	shownFields = "id,etag,status,summary,start,end,location"
	busyFields  = "status,start,end,transparency,attendees(self,responseStatus)"

	argInvalid    = "OPERATION_ARGUMENT_INVALID"
	remoteFailed  = "NET_REMOTE_FAILED"
	effectUnknown = "NET_EFFECT_UNKNOWN"

	nothingRead = "nothing was read"
)

var plugin = sdk.New("com.aiii.examples.google-calendar")

func init() {
	p := plugin

	p.Describe("calendar.today", sdk.Descriptor{
		Summary:      "What is on the calendar today: the whole day, midnight to midnight in the calendar's own time zone",
		Input:        "schemas/today_in.json",
		Output:       "schemas/events_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host},
		Family:       "calendar",
		Keywords:     []string{"calendar", "today", "schedule", "meetings", "agenda"},
		Examples:     []string{`{}`},
	})
	p.Handle("calendar.today", today)

	p.Describe("calendar.upcoming", sdk.Descriptor{
		Summary:      "The next few events on the calendar",
		Input:        "schemas/upcoming_in.json",
		Output:       "schemas/events_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host},
		Family:       "calendar",
		Keywords:     []string{"calendar", "upcoming", "next", "events"},
		Examples:     []string{`{"limit": 5}`},
	})
	p.Handle("calendar.upcoming", upcoming)

	p.Describe("calendar.availability", sdk.Descriptor{
		Summary:      "Free time on this calendar alone, between two local times: its own events, read; it says nothing about any other person's time or whether they would accept",
		Input:        "schemas/availability_in.json",
		Output:       "schemas/availability_out.json",
		Effects:      sdk.EffectsReadExternal,
		Capabilities: []string{host},
		Family:       "calendar",
		Keywords:     []string{"calendar", "free", "availability", "slot", "find a time"},
		Examples:     []string{`{"from": "2026-10-02T09:00", "to": "2026-10-02T17:00", "minutes": 30}`},
	})
	p.Handle("calendar.availability", availability)

	p.Describe("calendar.create", sdk.Descriptor{
		Summary:          "Create one event once the operator confirms it; the same create asked again finds the event it made instead of making a second",
		Input:            "schemas/create_in.json",
		Output:           "schemas/create_out.json",
		Effects:          sdk.EffectsWriteExternal,
		Capabilities:     []string{host},
		Family:           "calendar",
		Keywords:         []string{"calendar", "create", "add", "schedule", "meeting", "invite"},
		Examples:         []string{`{"title": "Dentist", "start": "2026-10-02T10:00", "end": "2026-10-02T11:00", "time_zone": "America/New_York", "notify": "none"}`},
		OperatorConfirms: true,
	})
	p.Handle("calendar.create", create)

	p.Describe("calendar.update", sdk.Descriptor{
		Summary:          "Change the named fields of one event once the operator confirms it, only if the event is still the version that was read",
		Input:            "schemas/update_in.json",
		Output:           "schemas/update_out.json",
		Effects:          sdk.EffectsWriteExternal,
		Capabilities:     []string{host},
		Family:           "calendar",
		Keywords:         []string{"calendar", "change", "move", "reschedule", "rename", "update"},
		Examples:         []string{`{"event_id": "7kq3m4c1", "event_title": "Dentist", "version": "\"3181161784712000\"", "start": "2026-10-02T11:00", "end": "2026-10-02T12:00", "time_zone": "America/New_York", "notify": "none"}`},
		OperatorConfirms: true,
	})
	p.Handle("calendar.update", update)

	p.Describe("calendar.delete", sdk.Descriptor{
		Summary:          "Delete one event, named by its id and its title, once the operator confirms it; a read afterwards says whether it is gone",
		Input:            "schemas/delete_in.json",
		Output:           "schemas/delete_out.json",
		Effects:          sdk.EffectsWriteExternal,
		Capabilities:     []string{host},
		Family:           "calendar",
		Keywords:         []string{"calendar", "delete", "cancel", "remove"},
		Examples:         []string{`{"event_id": "7kq3m4c1", "event_title": "Dentist", "notify": "none"}`},
		OperatorConfirms: true,
	})
	p.Handle("calendar.delete", remove)

	p.Run()
}

func settings(didNot string) (handle, calendar string, err error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return "", "", sdk.Fail(argInvalid, "the plugin's settings could not be read — "+didNot)
	}
	handle, _ = vals.Handle("google")
	if handle == "" {
		return "", "", sdk.Fail(argInvalid, "the google handle is not set — connect a Google account on the Plugins page and set the google setting to the profile's name; "+didNot)
	}
	calendar, _ = vals.String("calendar_id")
	if calendar == "" {
		calendar = "primary"
	}
	return handle, calendar, nil
}

func confirmed(c sdk.Call, didNot string) error {
	if _, ok := sdk.OperatorAct(c.Args()); !ok {
		return sdk.Deny("POLICY_DENY", "the operator has not confirmed this — a calendar write runs only once the operator confirms it on the Plugins page; "+didNot)
	}
	return nil
}

func hostNow(c sdk.Call) (time.Time, error) {
	ms, ok := c.HostNowMillis()
	if !ok || ms <= 0 {
		return time.Time{}, sdk.Fail(argInvalid, "the call carries no host clock (_host_now_ms), so now cannot be placed — "+nothingRead)
	}
	return time.UnixMilli(ms), nil
}

func eventsURL(calendar string) string {
	return api + "/calendars/" + url.PathEscape(calendar) + "/events"
}

func eventURL(calendar, id string) string {
	return eventsURL(calendar) + "/" + url.PathEscape(id)
}

func opts(handle string) *sdk.HTTPOptions {
	return &sdk.HTTPOptions{AuthProfile: handle, TimeoutMS: exchange}
}

func writing(handle string, body map[string]any, headers map[string]string) *sdk.HTTPOptions {
	b, _ := json.Marshal(body)
	return &sdk.HTTPOptions{Body: string(b), ContentType: "application/json", AuthProfile: handle, TimeoutMS: exchange, Headers: headers}
}

type ending int

const (
	answered ending = iota
	refused
	unreached
	lost
	unread
)

func codeOf(err error) (code, status, said string) {
	if d, ok := sdk.AsDenied(err); ok {
		return d.ReasonCode, "denied", d.Message
	}
	if oe, ok := sdk.AsOperationError(err); ok {
		return oe.ReasonCode, oe.Status, oe.Reason
	}
	return "", "", ""
}

func ended(res sdk.HTTPResult, err error) ending {
	if err == nil {
		if res.Status > 0 {
			return answered
		}
		return unread
	}
	code, status, _ := codeOf(err)
	switch {
	case code == "":
		return unread
	case code == effectUnknown:
		return lost
	case status == "denied":
		return refused
	case code == "NET_REMOTE_OUTCOME_FAILED" && res.Status >= 400:
		return answered
	case code == "NET_REMOTE_OUTCOME_FAILED" && res.Effect == "not-performed":
		return unreached
	}
	return unread
}

func refusal(err error, didNot string) error {
	code, _, said := codeOf(err)
	switch {
	case code == "NET_AUTH_PROFILE_DISCONNECTED":
		return sdk.Deny(code, "the Google account is not connected, or Google no longer accepts it — the operator connects it again on the Plugins page; "+didNot)
	case code == "NET_AUTH_PROFILE_UNAVAILABLE":
		return sdk.Deny(code, "the Google account's access could not be refreshed just now — try again later; "+didNot)
	case strings.HasPrefix(code, "NET_AUTH_PROFILE_"):
		return sdk.Deny(code, "the host could not use the Google account ("+code+") — check the google profile on the Plugins page; "+didNot)
	}
	return sdk.Deny(code, "the host refused the call ("+said+") — "+didNot)
}

func unreadWords(err error) string {
	if code, _, _ := codeOf(err); code != "" {
		return "the host answered " + code
	}
	return "the host's answer could not be read"
}

func why(res sdk.HTTPResult, err error) string {
	switch ended(res, err) {
	case answered:
		return "Google answered " + googleSaid(res)
	case refused:
		code, _, _ := codeOf(err)
		return "the host refused it (" + code + ")"
	case unreached:
		return "Google was not reached"
	case lost:
		return "the answer was lost"
	}
	return unreadWords(err)
}

func googleSaid(res sdk.HTTPResult) string {
	s := strconv.Itoa(res.Status)
	if m, _ := sdk.Object(res.Body).Object("error").String("message"); m != "" {
		s += ": " + clip(m, 200)
	}
	return s
}

func wait(res sdk.HTTPResult) string {
	if res.RetryAfterS != nil {
		return fmt.Sprintf("asks to wait %d s", *res.RetryAfterS)
	}
	return "named no time to wait"
}

func isObject(raw json.RawMessage) bool { return len(raw) > 0 && raw[0] == '{' }

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

type window struct{ from, to string }

type listing struct {
	items []sdk.Object
	zone  string
	next  string
	res   sdk.HTTPResult
}

func readFailed(res sdk.HTTPResult, err error) error {
	e := ended(res, err)
	switch {
	case e == refused:
		return refusal(err, nothingRead)
	case e == answered && res.Status >= 200 && res.Status < 300:
		if isObject(res.Body) {
			return nil
		}
		return sdk.Fail(remoteFailed, fmt.Sprintf("Google answered %d and the answer could not be read — %s", res.Status, nothingRead))
	case e == answered && res.Status == 429:
		return sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") and "+wait(res)+" — "+nothingRead)
	}
	return sdk.Fail(remoteFailed, why(res, err)+" — "+nothingRead)
}

func list(handle, calendar string, w window, token, fields string, size, pages, want int) (listing, error) {
	var l listing
	for page := 0; page < pages; page++ {
		q := url.Values{}
		q.Set("singleEvents", "true")
		q.Set("orderBy", "startTime")
		q.Set("timeMin", w.from)
		if w.to != "" {
			q.Set("timeMax", w.to)
		}
		q.Set("maxResults", strconv.Itoa(size))
		q.Set("fields", "items("+fields+"),nextPageToken,timeZone")
		if token != "" {
			q.Set("pageToken", token)
		}
		res, err := sdk.HTTP.Get(eventsURL(calendar)+"?"+q.Encode(), opts(handle))
		if f := readFailed(res, err); f != nil {
			return l, f
		}
		answer := sdk.Object(res.Body)
		items, ok := sdk.ObjectArray(answer.Raw("items"))
		if !ok && answer.Has("items") {
			return l, sdk.Fail(remoteFailed, "Google's list could not be read — "+nothingRead)
		}
		l.items, l.res = append(l.items, items...), res
		if z, _ := answer.String("timeZone"); z != "" {
			l.zone = z
		}
		token, _ = answer.String("nextPageToken")
		if want > 0 && len(l.items) >= want {
			l.items = l.items[:want]
			return l, nil
		}
		if token == "" {
			return l, nil
		}
	}
	l.next = w.from + "|" + w.to + "|" + token
	return l, nil
}

func (l listing) answer(extra map[string]any) map[string]any {
	items := make([]any, 0, len(l.items))
	for _, it := range l.items {
		items = append(items, json.RawMessage(it))
	}
	a := map[string]any{
		"http_status": l.res.Status,
		"events":      map[string]any{"items": items, "timeZone": l.zone},
		"effect":      l.res.Effect,
		"time_zone":   l.zone,
		"truncated":   l.next != "",
	}
	if l.next != "" {
		a["next_page"] = l.next
	}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func continued(c sdk.Call) (w window, token string, err error) {
	p, _ := c.Args().String("page")
	if p == "" {
		return window{}, "", nil
	}
	parts := strings.SplitN(p, "|", 3)
	if len(parts) != 3 || parts[2] == "" || !isInstant(parts[0]) || parts[1] != "" && !isInstant(parts[1]) {
		return window{}, "", sdk.Fail(argInvalid, "page is not a next_page this plugin gave — "+nothingRead)
	}
	return window{parts[0], parts[1]}, parts[2], nil
}

func isInstant(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func zoneOf(name string) (*time.Location, bool) {
	if name == "" || name == "Local" || len(name) > 64 {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	return loc, err == nil
}

func calendarZone(handle, calendar string) (*time.Location, error) {
	res, err := sdk.HTTP.Get(eventsURL(calendar)+"?maxResults=1&fields=timeZone", opts(handle))
	if f := readFailed(res, err); f != nil {
		return nil, f
	}
	name, _ := sdk.Object(res.Body).String("timeZone")
	loc, ok := zoneOf(name)
	if !ok {
		return nil, sdk.Fail(remoteFailed, fmt.Sprintf("Google named the calendar's time zone %q, which this plugin cannot place — %s", clip(name, 64), nothingRead))
	}
	return loc, nil
}

func today(c sdk.Call) (any, error) {
	handle, calendar, err := settings(nothingRead)
	if err != nil {
		return nil, err
	}
	w, token, err := continued(c)
	if err != nil {
		return nil, err
	}
	zone := ""
	if token == "" {
		now, err := hostNow(c)
		if err != nil {
			return nil, err
		}
		loc, err := calendarZone(handle, calendar)
		if err != nil {
			return nil, err
		}
		d := now.In(loc)
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		end := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
		w, zone = window{start.Format(time.RFC3339), end.Format(time.RFC3339)}, loc.String()
	}
	l, err := list(handle, calendar, w, token, shownFields, dayPage, dayPages, 0)
	if err != nil {
		return nil, err
	}
	if l.zone == "" {
		l.zone = zone
	}
	return l.answer(map[string]any{"day_start": w.from, "day_end": w.to}), nil
}

func upcoming(c sdk.Call) (any, error) {
	handle, calendar, err := settings(nothingRead)
	if err != nil {
		return nil, err
	}
	limit := 5
	if n, ok := c.Args().Int("limit"); ok && n >= 1 && n <= 50 {
		limit = int(n)
	}
	w, token, err := continued(c)
	if err != nil {
		return nil, err
	}
	if token == "" {
		now, err := hostNow(c)
		if err != nil {
			return nil, err
		}
		w = window{from: now.UTC().Format(time.RFC3339)}
	}
	l, err := list(handle, calendar, w, token, shownFields, limit, nextPages, limit)
	if err != nil {
		return nil, err
	}
	return l.answer(nil), nil
}

func availability(c sdk.Call) (any, error) {
	const didNot = "no free time was read"
	a := c.Args()
	minutes, ok := a.Int("minutes")
	if !ok || minutes < 1 || minutes > 1440 {
		return nil, sdk.Fail(argInvalid, "minutes is a whole number from 1 to 1440 — "+didNot)
	}
	handle, calendar, err := settings(didNot)
	if err != nil {
		return nil, err
	}
	loc, err := calendarZone(handle, calendar)
	if err != nil {
		return nil, err
	}
	fromS, _ := a.String("from")
	toS, _ := a.String("to")
	from, problem := wallClock("from", fromS, loc)
	var to time.Time
	if problem == "" {
		to, problem = wallClock("to", toS, loc)
	}
	switch {
	case problem != "":
	case !to.After(from):
		problem = "to is not after from"
	case to.Sub(from) > freeSpan:
		problem = "the window is longer than 14 days"
	}
	if problem != "" {
		return nil, sdk.Fail(argInvalid, problem+" — "+didNot)
	}
	l, err := list(handle, calendar, window{from.Format(time.RFC3339), to.Format(time.RFC3339)}, "", busyFields, freePage, freePages, 0)
	if err != nil {
		return nil, err
	}

	upto := to
	if l.next != "" {
		upto = from
		if n := len(l.items); n > 0 {
			if s, _, ok := span(l.items[n-1], loc); ok && s.After(from) && s.Before(to) {
				upto = s
			}
		}
	}
	held, err := busy(l.items, loc)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"time_zone": loc.String(),
		"from":      from.Format(time.RFC3339),
		"to":        to.Format(time.RFC3339),
		"minutes":   minutes,
		"free":      gaps(from, upto, held, time.Duration(minutes)*time.Minute, loc),
		"truncated": l.next != "",
		"scope":     "the calendar " + calendar + " alone: its own events, read — this says nothing about any other person's time, or whether anyone would accept",
	}
	if l.next != "" {
		out["more"] = "the calendar was read up to " + upto.Format(time.RFC3339) + ", so no free time from then is claimed — ask again with from " + upto.In(loc).Format("2006-01-02T15:04:05")
	}
	return out, nil
}

func busy(items []sdk.Object, loc *time.Location) ([][2]time.Time, error) {
	var held [][2]time.Time
	for _, ev := range items {
		status, _ := ev.String("status")
		shown, _ := ev.String("transparency")
		if status == "cancelled" || shown == "transparent" || declined(ev) {
			continue
		}
		s, e, ok := span(ev, loc)
		if !ok {
			return nil, sdk.Fail(remoteFailed, "an event's times in Google's answer could not be read — no free time was read")
		}
		held = append(held, [2]time.Time{s, e})
	}
	slices.SortFunc(held, func(a, b [2]time.Time) int { return a[0].Compare(b[0]) })
	return held, nil
}

func gaps(from, to time.Time, held [][2]time.Time, need time.Duration, loc *time.Location) []any {
	free := []any{}
	add := func(s, e time.Time) {
		if e.Sub(s) >= need {
			free = append(free, map[string]any{"start": s.In(loc).Format(time.RFC3339), "end": e.In(loc).Format(time.RFC3339), "minutes": int64(e.Sub(s) / time.Minute)})
		}
	}
	cursor := from
	for _, h := range held {
		if !h[0].Before(to) {
			break
		}
		if h[0].After(cursor) {
			add(cursor, h[0])
		}
		if h[1].After(cursor) {
			cursor = h[1]
		}
	}
	if to.After(cursor) {
		add(cursor, to)
	}
	return free
}

func span(ev sdk.Object, loc *time.Location) (start, end time.Time, ok bool) {
	start, ok1 := instant(ev.Object("start"), loc)
	end, ok2 := instant(ev.Object("end"), loc)
	return start, end, ok1 && ok2 && !end.Before(start)
}

func instant(t sdk.Object, loc *time.Location) (time.Time, bool) {
	if dt, ok := t.String("dateTime"); ok {
		v, err := time.Parse(time.RFC3339, dt)
		return v, err == nil
	}
	if d, ok := t.String("date"); ok {
		v, err := time.ParseInLocation("2006-01-02", d, loc)
		return v, err == nil
	}
	return time.Time{}, false
}

func declined(ev sdk.Object) bool {
	guests, _ := sdk.ObjectArray(ev.Raw("attendees"))
	for _, g := range guests {
		if self, _ := g.Bool("self"); self {
			answer, _ := g.String("responseStatus")
			return answer == "declined"
		}
	}
	return false
}

type fields struct {
	title, location, description *string
	start, end                   time.Time
	zone                         string
	attendees                    []string
}

func wallClock(name, s string, loc *time.Location) (time.Time, string) {
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		if t.Format(layout) != s {
			return t, fmt.Sprintf("%s %s does not happen in %s: the clock skips it", name, s, loc)
		}
		for _, d := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour} {
			if t.Add(d).In(loc).Format(layout) == s || t.Add(-d).In(loc).Format(layout) == s {
				return t, fmt.Sprintf("%s %s happens twice in %s: the clock goes back over it", name, s, loc)
			}
		}
		return t, ""
	}
	return time.Time{}, fmt.Sprintf("%s %q is not a local date and time such as 2026-10-02T10:00", name, clip(s, 40))
}

func (f *fields) times(a sdk.Object) (named bool, problem string) {
	s, hasStart := a.String("start")
	e, hasEnd := a.String("end")
	z, hasZone := a.String("time_zone")
	if !hasStart && !hasEnd && !hasZone {
		return false, ""
	}
	if !hasStart || !hasEnd || !hasZone {
		return true, "start, end and time_zone are named together"
	}
	loc, ok := zoneOf(z)
	if !ok {
		return true, fmt.Sprintf("time_zone %q is not a zone this plugin knows (an IANA name such as Europe/Zurich)", clip(z, 64))
	}
	if f.start, problem = wallClock("start", s, loc); problem != "" {
		return true, problem
	}
	if f.end, problem = wallClock("end", e, loc); problem != "" {
		return true, problem
	}
	if !f.end.After(f.start) {
		return true, "end is not after start"
	}
	f.zone = z
	return true, ""
}

func text(a sdk.Object, key string, max int) (*string, string) {
	if !a.Has(key) {
		return nil, ""
	}
	s, ok := a.String(key)
	if !ok || utf8.RuneCountInString(s) > max {
		return nil, fmt.Sprintf("%s is text of at most %d characters", key, max)
	}
	return &s, ""
}

func title(a sdk.Object) (*string, string) {
	t, problem := text(a, "title", 1024)
	if t == nil || problem != "" {
		return nil, problem
	}
	if s := strings.TrimSpace(*t); s != "" {
		return &s, ""
	}
	return nil, "title is 1 to 1024 characters"
}

func guests(a sdk.Object) ([]string, string) {
	if !a.Has("attendees") {
		return []string{}, ""
	}
	list, ok := a.StringArray("attendees")
	if !ok || len(list) > 100 {
		return nil, "attendees is a list of at most 100 email addresses"
	}
	out := []string{}
	for _, g := range list {
		g = strings.ToLower(strings.TrimSpace(g))
		at := strings.IndexByte(g, '@')
		if at < 1 || at != strings.LastIndexByte(g, '@') || at == len(g)-1 || len(g) > 254 || strings.ContainsAny(g, " \t\r\n<>,;\"") {
			return nil, fmt.Sprintf("attendee %q is not an email address", clip(g, 60))
		}
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out, ""
}

func notifyOf(a sdk.Object) (string, string) {
	n, _ := a.String("notify")
	switch n {
	case "none", "externalOnly", "all":
		return n, ""
	}
	return "", "notify is none, externalOnly or all"
}

func notifications(notify, about string) string {
	switch notify {
	case "all":
		return "Google was asked to email every guest about " + about
	case "externalOnly":
		return "Google was asked to email the guests who do not use Google Calendar about " + about
	}
	return "Google was asked to email no one"
}

func (f fields) differences(ev sdk.Object) []string {
	var d []string
	str := func(key string) string { s, _ := ev.String(key); return s }
	if f.title != nil && str("summary") != *f.title {
		d = append(d, "title")
	}
	if !f.start.IsZero() && !sameTime(ev.Object("start"), f.start, f.zone) {
		d = append(d, "start")
	}
	if !f.end.IsZero() && !sameTime(ev.Object("end"), f.end, f.zone) {
		d = append(d, "end")
	}
	if f.location != nil && str("location") != *f.location {
		d = append(d, "location")
	}
	if f.description != nil && str("description") != *f.description {
		d = append(d, "description")
	}
	if f.attendees != nil && !sameGuests(ev, f.attendees) {
		d = append(d, "attendees")
	}
	return d
}

func sameTime(t sdk.Object, want time.Time, zone string) bool {
	dt, _ := t.String("dateTime")
	got, err := time.Parse(time.RFC3339, dt)
	z, _ := t.String("timeZone")
	return err == nil && got.Equal(want) && z == zone
}

func sameGuests(ev sdk.Object, want []string) bool {
	list, _ := sdk.ObjectArray(ev.Raw("attendees"))
	have := []string{}
	for _, g := range list {
		email, _ := g.String("email")
		email = strings.ToLower(email)
		organizer, _ := g.Bool("organizer")
		self, _ := g.Bool("self")
		if organizer && self && !slices.Contains(want, email) {
			continue
		}
		if !slices.Contains(have, email) {
			have = append(have, email)
		}
	}
	if len(have) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func describe(ev sdk.Object) string {
	t, _ := ev.String("summary")
	return fmt.Sprintf("%q from %s to %s", clip(t, 100), when(ev.Object("start")), when(ev.Object("end")))
}

func when(t sdk.Object) string {
	if dt, ok := t.String("dateTime"); ok {
		return dt
	}
	d, _ := t.String("date")
	return d
}

type intent struct {
	fields
	handle, calendar, notify, id string
}

func eventID(calendar string, start, end time.Time, title, distinct string) string {
	h := sha256.New()
	for _, part := range []string{"google-calendar create 1", calendar, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), title, distinct} {
		h.Write([]byte(strconv.Itoa(len(part)) + ":" + part + ";"))
	}
	return strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(h.Sum(nil)))
}

func create(c sdk.Call) (any, error) {
	const didNot = "the event was not created"
	if err := confirmed(c, didNot); err != nil {
		return nil, err
	}
	a := c.Args()
	var ev intent
	var problem string
	if ev.title, problem = title(a); ev.title == nil && problem == "" {
		problem = "create names a title"
	}
	if problem == "" {
		if named, p := ev.times(a); !named {
			problem = "create names start, end and time_zone"
		} else {
			problem = p
		}
	}
	if problem == "" {
		ev.location, problem = text(a, "location", 1024)
	}
	if problem == "" {
		ev.description, problem = text(a, "description", 8192)
	}
	if problem == "" {
		ev.attendees, problem = guests(a)
	}
	distinct, p := text(a, "distinct", 64)
	if problem == "" {
		problem = p
	}
	if problem == "" {
		ev.notify, problem = notifyOf(a)
	}
	if problem != "" {
		return nil, sdk.Fail(argInvalid, problem+" — "+didNot)
	}

	for _, s := range []**string{&ev.location, &ev.description, &distinct} {
		if *s == nil {
			*s = new(string)
		}
	}
	var err error
	if ev.handle, ev.calendar, err = settings(didNot); err != nil {
		return nil, err
	}
	ev.id = eventID(ev.calendar, ev.start, ev.end, *ev.title, *distinct)
	res, err := sdk.HTTP.Post(eventsURL(ev.calendar)+"?sendUpdates="+ev.notify, writing(ev.handle, ev.body(), nil))
	return created(ev, res, err)
}

func (ev intent) body() map[string]any {
	b := map[string]any{
		"id":      ev.id,
		"summary": *ev.title,
		"start":   map[string]any{"dateTime": ev.start.Format(time.RFC3339), "timeZone": ev.zone},
		"end":     map[string]any{"dateTime": ev.end.Format(time.RFC3339), "timeZone": ev.zone},
	}
	if *ev.location != "" {
		b["location"] = *ev.location
	}
	if *ev.description != "" {
		b["description"] = *ev.description
	}
	if len(ev.attendees) > 0 {
		list := []any{}
		for _, g := range ev.attendees {
			list = append(list, map[string]any{"email": g})
		}
		b["attendees"] = list
	}
	return b
}

func (ev intent) receipt(outcome string, got sdk.Object, notified string) map[string]any {
	r := map[string]any{
		"event_id":      ev.id,
		"outcome":       outcome,
		"start":         ev.start.Format(time.RFC3339),
		"end":           ev.end.Format(time.RFC3339),
		"time_zone":     ev.zone,
		"notifications": notified,
	}
	if etag, ok := got.String("etag"); ok {
		r["etag"] = etag
	}
	if link, ok := got.String("htmlLink"); ok {
		r["html_link"] = link
	}
	return r
}

func created(ev intent, res sdk.HTTPResult, err error) (any, error) {
	const didNot = "the event was not created"
	const mayHave = "the event may have been created; the same create asked again finds it by its id"
	switch ended(res, err) {
	case refused:
		return nil, refusal(err, didNot)
	case unreached:
		return nil, sdk.Fail(remoteFailed, "Google was not reached — "+didNot)
	case lost:
		return nil, sdk.Fail(effectUnknown, "the create was written and its answer was lost — "+mayHave)
	case unread:
		return nil, sdk.Fail(effectUnknown, unreadWords(err)+" — "+mayHave)
	}
	switch s := res.Status; {
	case s == 409:
		return settle(ev, googleSaid(res))
	case s == 400:
		return nil, sdk.Fail(argInvalid, "Google refused the event ("+googleSaid(res)+") — "+didNot)
	case s == 429:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") and "+wait(res)+" — "+didNot)
	case s >= 500:
		return nil, sdk.Fail(effectUnknown, "Google answered "+googleSaid(res)+" — "+mayHave)
	case s >= 400:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") — "+didNot)
	}
	got := sdk.Object(res.Body)
	if id, _ := got.String("id"); res.Status < 200 || res.Status >= 300 || id != ev.id {
		return nil, sdk.Fail(effectUnknown, fmt.Sprintf("Google answered %d and the answer does not show the event — %s", res.Status, mayHave))
	}
	return ev.receipt("created", got, notifications(ev.notify, "the new event")), nil
}

func settle(ev intent, said string) (any, error) {
	const earlier = "this call created nothing and emailed no one; whether the earlier create emailed its guests is not known here"
	res, err := sdk.HTTP.Get(eventURL(ev.calendar, ev.id), opts(ev.handle))
	got := sdk.Object(res.Body)
	status, _ := got.String("status")
	if ended(res, err) != answered || res.Status != 200 || !got.Has("id") {
		reason := why(res, err)
		if ended(res, err) == answered && res.Status == 200 {
			reason = "Google's answer could not be read"
		}
		return nil, sdk.Fail(effectUnknown, fmt.Sprintf("Google says an event with this id exists (%s) and it could not be read back (%s) — it may be this event, created earlier; nothing was created now", said, reason))
	}
	if status == "cancelled" {
		return nil, sdk.Fail(argInvalid, "this event's id belongs to an event deleted from this calendar, and Google keeps it — the event was not created; to create it again, give it a distinct word")
	}
	if d := ev.differences(got); len(d) > 0 {
		return nil, sdk.Fail(argInvalid, "an event with this id exists and differs in its "+strings.Join(d, ", ")+" — the event was not created; it may be this event, changed since it was made")
	}
	return ev.receipt("created_earlier", got, earlier), nil
}

type change struct {
	fields
	handle, calendar, notify, id, label, version string
}

func (ch change) named() []string {
	var n []string
	if ch.title != nil {
		n = append(n, "title")
	}
	if !ch.start.IsZero() {
		n = append(n, "start", "end", "time_zone")
	}
	if ch.location != nil {
		n = append(n, "location")
	}
	if ch.description != nil {
		n = append(n, "description")
	}
	return n
}

func (ch change) patch() map[string]any {
	b := map[string]any{}
	if ch.title != nil {
		b["summary"] = *ch.title
	}
	if !ch.start.IsZero() {
		b["start"] = map[string]any{"dateTime": ch.start.Format(time.RFC3339), "timeZone": ch.zone}
		b["end"] = map[string]any{"dateTime": ch.end.Format(time.RFC3339), "timeZone": ch.zone}
	}
	if ch.location != nil {
		b["location"] = *ch.location
	}
	if ch.description != nil {
		b["description"] = *ch.description
	}
	return b
}

func target(a sdk.Object) (id, label, problem string) {
	id, _ = a.String("event_id")
	if id == "" || len(id) > 1024 || strings.ContainsAny(id, " \t\r\n") {
		return "", "", "event_id is the event's id, as a read of the calendar gave it"
	}
	l, p := text(a, "event_title", 1024)
	if l == nil || p != "" {
		return "", "", "event_title is the event's title, as a read of the calendar gave it"
	}
	return id, strings.TrimSpace(*l), ""
}

func update(c sdk.Call) (any, error) {
	const didNot = "nothing was changed"
	if err := confirmed(c, didNot); err != nil {
		return nil, err
	}
	a := c.Args()
	var ch change
	var problem string
	ch.id, ch.label, problem = target(a)
	if problem == "" {
		ch.version, _ = a.String("version")
		if ch.version == "" || len(ch.version) > 256 || strings.ContainsAny(ch.version, "\r\n\x00") {
			problem = "version is the event's etag, as the read that gave its id gave it"
		}
	}
	if problem == "" && a.Has("title") {
		if ch.title, problem = title(a); ch.title == nil && problem == "" {
			problem = "title is 1 to 1024 characters"
		}
	}
	if problem == "" {
		_, problem = ch.times(a)
	}
	if problem == "" {
		ch.location, problem = text(a, "location", 1024)
	}
	if problem == "" {
		ch.description, problem = text(a, "description", 8192)
	}
	if problem == "" {
		ch.notify, problem = notifyOf(a)
	}
	if problem == "" && len(ch.named()) == 0 {
		problem = "update names at least one of title, start with end and time_zone, location or description"
	}
	if problem != "" {
		return nil, sdk.Fail(argInvalid, problem+" — "+didNot)
	}
	var err error
	if ch.handle, ch.calendar, err = settings(didNot); err != nil {
		return nil, err
	}
	cur, gone, err := current(ch.handle, ch.calendar, ch.id, didNot)
	switch {
	case err != nil:
		return nil, err
	case gone:
		return nil, sdk.Fail(argInvalid, "event "+ch.id+" was deleted from this calendar — "+didNot)
	case cur.Has("recurrence"):
		return nil, sdk.Fail(argInvalid, series(ch.id, didNot))
	}
	if etag, _ := cur.String("etag"); etag != ch.version {
		return nil, ch.stale(cur)
	}
	if t, _ := cur.String("summary"); strings.TrimSpace(t) != ch.label {
		return nil, sdk.Fail(argInvalid, fmt.Sprintf("event %s is titled %q, not %q as the change names it — %s", ch.id, clip(t, 100), clip(ch.label, 100), didNot))
	}

	res, err := sdk.HTTP.Patch(eventURL(ch.calendar, ch.id)+"?sendUpdates="+ch.notify, writing(ch.handle, ch.patch(), map[string]string{"If-Match": ch.version}))
	return updated(ch, res, err)
}

func current(handle, calendar, id, didNot string) (ev sdk.Object, gone bool, err error) {
	res, err := sdk.HTTP.Get(eventURL(calendar, id), opts(handle))
	switch ended(res, err) {
	case refused:
		return nil, false, refusal(err, didNot)
	case answered:
	default:
		return nil, false, sdk.Fail(remoteFailed, "the event could not be read first ("+why(res, err)+") — "+didNot)
	}
	ev = sdk.Object(res.Body)
	status, _ := ev.String("status")
	switch {
	case res.Status == 410, res.Status == 200 && status == "cancelled":
		return ev, true, nil
	case res.Status == 404:
		return nil, false, sdk.Fail(argInvalid, "Google has no event "+id+" on this calendar — "+didNot+"; read the calendar for the event's id")
	case res.Status == 429:
		return nil, false, sdk.Fail(remoteFailed, "the event could not be read first (Google refused it: "+googleSaid(res)+", and "+wait(res)+") — "+didNot)
	case res.Status != 200 || !ev.Has("id"):
		return nil, false, sdk.Fail(remoteFailed, "the event could not be read first ("+why(res, err)+") — "+didNot)
	}
	return ev, false, nil
}

func series(id, didNot string) string {
	return "event " + id + " is a repeating series, and this plugin changes one occurrence at a time — name the occurrence's id, as a read of the calendar gives it; " + didNot
}

func (ch change) stale(cur sdk.Object) error {
	etag, _ := cur.String("etag")
	s := "the event changed since it was read: it is now " + describe(cur) + ", version " + etag + " — nothing was changed"
	if len(ch.differences(cur)) == 0 {
		s += ", and it already holds every value this change names"
	}
	return sdk.Fail(argInvalid, s+"; ask again from what it is now")
}

func updated(ch change, res sdk.HTTPResult, err error) (any, error) {
	const didNot = "nothing was changed"
	const mayHave = "the change may have been made; read the event before asking again"
	switch ended(res, err) {
	case refused:
		return nil, refusal(err, didNot)
	case unreached:
		return nil, sdk.Fail(remoteFailed, "Google was not reached — "+didNot)
	case lost:
		return nil, sdk.Fail(effectUnknown, "the change was written and its answer was lost — "+mayHave)
	case unread:
		return nil, sdk.Fail(effectUnknown, unreadWords(err)+" — "+mayHave)
	}
	switch s := res.Status; {
	case s == 412:
		cur, gone, rerr := current(ch.handle, ch.calendar, ch.id, didNot)
		switch {
		case rerr != nil:
			return nil, sdk.Fail(argInvalid, "the event changed since it was read (Google answered "+googleSaid(res)+") and could not be read again — "+didNot+"; read it and ask again")
		case gone:
			return nil, sdk.Fail(argInvalid, "the event was deleted since it was read (Google answered "+googleSaid(res)+") — "+didNot)
		}
		return nil, ch.stale(cur)
	case s == 400:
		return nil, sdk.Fail(argInvalid, "Google refused the change ("+googleSaid(res)+") — "+didNot)
	case s == 404 || s == 410:
		return nil, sdk.Fail(argInvalid, "Google says the event is gone ("+googleSaid(res)+") — "+didNot)
	case s == 429:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") and "+wait(res)+" — "+didNot)
	case s >= 500:
		return nil, sdk.Fail(effectUnknown, "Google answered "+googleSaid(res)+" — "+mayHave)
	case s >= 400:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") — "+didNot)
	}
	got := sdk.Object(res.Body)
	id, _ := got.String("id")
	etag, hasEtag := got.String("etag")
	if res.Status < 200 || res.Status >= 300 || id != ch.id || !hasEtag {
		return nil, sdk.Fail(effectUnknown, fmt.Sprintf("Google answered %d and the answer does not show the event — %s", res.Status, mayHave))
	}
	return map[string]any{
		"event_id":      ch.id,
		"outcome":       "updated",
		"etag":          etag,
		"changed":       ch.named(),
		"notifications": notifications(ch.notify, "this change"),
	}, nil
}

type doomed struct {
	handle, calendar, notify, id string
}

func (d doomed) answer(outcome string, confirmed bool, confirmation, notified string) map[string]any {
	return map[string]any{"event_id": d.id, "outcome": outcome, "confirmed": confirmed, "confirmation": confirmation, "notifications": notified}
}

func remove(c sdk.Call) (any, error) {
	const didNot = "nothing was deleted"
	if err := confirmed(c, didNot); err != nil {
		return nil, err
	}
	a := c.Args()
	var d doomed
	id, label, problem := target(a)
	if problem == "" {
		d.notify, problem = notifyOf(a)
	}
	if problem != "" {
		return nil, sdk.Fail(argInvalid, problem+" — "+didNot)
	}
	d.id = id
	var err error
	if d.handle, d.calendar, err = settings(didNot); err != nil {
		return nil, err
	}
	cur, gone, err := current(d.handle, d.calendar, d.id, didNot)
	switch {
	case err != nil:
		return nil, err
	case gone:
		return d.answer("already_deleted", true, "a read before the delete found the event already deleted", "this call deleted nothing and emailed no one"), nil
	case cur.Has("recurrence"):
		return nil, sdk.Fail(argInvalid, series(d.id, didNot))
	}
	if t, _ := cur.String("summary"); strings.TrimSpace(t) != label {
		return nil, sdk.Fail(argInvalid, fmt.Sprintf("event %s is titled %q, not %q as the delete names it — %s", d.id, clip(t, 100), clip(label, 100), didNot))
	}
	res, err := sdk.HTTP.Delete(eventURL(d.calendar, d.id)+"?sendUpdates="+d.notify, opts(d.handle))
	return deleted(d, res, err)
}

func deleted(d doomed, res sdk.HTTPResult, err error) (any, error) {
	const didNot = "nothing was deleted"
	const mayHave = "the event may have been deleted; the same delete asked again reads it first"
	switch ended(res, err) {
	case refused:
		return nil, refusal(err, didNot)
	case unreached:
		return nil, sdk.Fail(remoteFailed, "Google was not reached — "+didNot)
	case lost:
		return nil, sdk.Fail(effectUnknown, "the delete was written and its answer was lost — "+mayHave)
	case unread:
		return nil, sdk.Fail(effectUnknown, unreadWords(err)+" — "+mayHave)
	}
	switch s := res.Status; {
	case s == 404 || s == 410:
		return d.answer("already_deleted", true, "Google answered "+googleSaid(res)+": the event was gone before this delete", "this call deleted nothing and emailed no one"), nil
	case s == 429:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") and "+wait(res)+" — "+didNot)
	case s >= 500:
		return nil, sdk.Fail(effectUnknown, "Google answered "+googleSaid(res)+" — "+mayHave)
	case s >= 400:
		return nil, sdk.Fail(remoteFailed, "Google refused it ("+googleSaid(res)+") — "+didNot)
	case s < 200 || s >= 300:
		return nil, sdk.Fail(effectUnknown, fmt.Sprintf("Google answered %d — %s", s, mayHave))
	}
	notified := notifications(d.notify, "the cancellation")
	check, cerr := sdk.HTTP.Get(eventURL(d.calendar, d.id), opts(d.handle))
	ev := sdk.Object(check.Body)
	status, _ := ev.String("status")
	if ended(check, cerr) == answered {
		switch {
		case check.Status == 404 || check.Status == 410 || check.Status == 200 && status == "cancelled":
			return d.answer("deleted", true, fmt.Sprintf("Google took the delete (%d), and a read then found the event gone (%s)", res.Status, gone(check.Status, status)), notified), nil
		case check.Status == 200 && ev.Has("id"):
			return nil, sdk.Fail(effectUnknown, fmt.Sprintf("Google took the delete (%d), and a read right after still finds the event — whether it is deleted is not confirmed; read the calendar before asking again", res.Status))
		}
	}
	return d.answer("deleted", false, fmt.Sprintf("Google took the delete (%d); the read to confirm it failed (%s), so the deletion is not confirmed", res.Status, why(check, cerr)), notified), nil
}

func gone(status int, event string) string {
	if status == 200 {
		return "held as " + event
	}
	return strconv.Itoa(status)
}

func main() { sdk.MainDescribe() }
