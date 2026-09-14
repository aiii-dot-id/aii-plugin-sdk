// .
// .
// .
// .
// .
package main

import (
	"fmt"
	"net/url"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	api  = "https://www.googleapis.com/calendar/v3"
	host = "net.outbound:www.googleapis.com:443"
)

func init() {
	p := sdk.New("com.aiii.examples.google-calendar")

	p.Describe("calendar.today", sdk.Descriptor{
		Summary:      "What is on the calendar today, from now to midnight",
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

	p.Run()
}

// .
// .
func settings() (handle, calendar string, err error) {
	vals, err := sdk.Settings.Load()
	if err != nil {
		return "", "", err
	}
	handle, _ = vals.Handle("google")
	if handle == "" {
		return "", "", sdk.Fail("OPERATION_ARGUMENT_INVALID", "the google handle is not set — connect a Google account on the Plugins page and set the google setting to the profile's name")
	}
	calendar, _ = vals.String("calendar_id")
	if calendar == "" {
		calendar = "primary"
	}
	return handle, calendar, nil
}

// .
func now(c sdk.Call) time.Time {
	if ms, ok := c.Args().Int("_host_now_ms"); ok && ms > 0 {
		return time.UnixMilli(ms).UTC()
	}
	return time.Now().UTC()
}

func fetch(handle, calendar string, from, to time.Time, limit int64) (any, error) {
	q := url.Values{}
	q.Set("timeMin", from.Format(time.RFC3339))
	if !to.IsZero() {
		q.Set("timeMax", to.Format(time.RFC3339))
	}
	q.Set("singleEvents", "true")
	q.Set("orderBy", "startTime")
	q.Set("maxResults", fmt.Sprint(limit))
	q.Set("fields", "items(id,summary,start,end,location,status),timeZone")
	res, err := sdk.HTTP.Get(fmt.Sprintf("%s/calendars/%s/events?%s", api, url.PathEscape(calendar), q.Encode()), &sdk.HTTPOptions{AuthProfile: handle, TimeoutMS: 15000})
	if err != nil {
		if d, ok := sdk.AsDenied(err); ok {
			return nil, sdk.Deny(d.ReasonCode, "the host denied "+d.Message+" — grant "+host+" and the google handle in plugins.grants, and connect the profile on the Plugins page")
		}
		return nil, err
	}
	return map[string]any{"http_status": res.Status, "events": res.Body, "effect": res.Effect}, nil
}

func today(c sdk.Call) (any, error) {
	handle, calendar, err := settings()
	if err != nil {
		return nil, err
	}
	t := now(c)
	midnight := time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
	return fetch(handle, calendar, t, midnight, 50)
}

func upcoming(c sdk.Call) (any, error) {
	handle, calendar, err := settings()
	if err != nil {
		return nil, err
	}
	limit := int64(5)
	if n, ok := c.Args().Int("limit"); ok && n >= 1 && n <= 50 {
		limit = n
	}
	return fetch(handle, calendar, now(c), time.Time{}, limit)
}

// .
// .
func main() { sdk.MainDescribe() }
