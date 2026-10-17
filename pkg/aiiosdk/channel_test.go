package aiiosdk

import (
	"reflect"
	"testing"
)

// A description says only what an adapter promises: an adapter that does not
// acknowledge produces exactly what it always did, so an older host reads it
// unchanged, and one that does says so.
func TestAChannelDescriptionSaysOnlyWhatItPromises(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    ChannelDescription
		want map[string]any
	}{
		{"a name alone", ChannelDescription{Channel: "sms"}, map[string]any{"channel": "sms"}},
		{"a webhook", ChannelDescription{Channel: "sms", Receive: "webhook"}, map[string]any{"channel": "sms", "receive": "webhook"}},
		{"a poll with a budget", ChannelDescription{Channel: "telegram", Receive: "poll", BudgetSeconds: 10},
			map[string]any{"channel": "telegram", "receive": "poll", "budget_seconds": 10}},
		{"one that acknowledges", ChannelDescription{Channel: "telegram", Receive: "poll", BudgetSeconds: 10, Acknowledges: true},
			map[string]any{"channel": "telegram", "receive": "poll", "budget_seconds": 10, "acknowledges": true}},
	} {
		if got := tc.d.Value(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
