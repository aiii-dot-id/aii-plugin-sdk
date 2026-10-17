package aiiospkg

import (
	"strings"
	"testing"
)

// A schedule is held to the floor and the ceiling, the bound and the plugin's
// own operations, as the host holds it.
func TestScheduleDeclarationsAreHeldToTheHostsRules(t *testing.T) {
	good := []ScheduleDecl{{Operation: "mail.renew", EverySeconds: 86400}, {Operation: "mail.sync", EverySeconds: 1800}}
	if err := ValidateSchedule(good); err != nil {
		t.Fatal(err)
	}
	out, err := ScheduleJSON(good)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"every_seconds":86400,"operation":"mail.renew"},{"every_seconds":1800,"operation":"mail.sync"}]`; string(out) != want {
		t.Fatalf("canonical member:\n%s\nwant\n%s", out, want)
	}
	if err := CheckScheduleOperations(good, []string{"mail.renew"}); err == nil || !strings.Contains(err.Error(), "not one this plugin describes") {
		t.Fatalf("an undescribed operation: %v", err)
	}
	if err := CheckScheduleOperations(good, []string{"mail.renew", "mail.sync"}); err != nil {
		t.Fatal(err)
	}
	five := make([]ScheduleDecl, 5)
	for i := range five {
		five[i] = ScheduleDecl{Operation: string(rune('a' + i)), EverySeconds: 600}
	}
	for name, tc := range map[string]struct {
		decls []ScheduleDecl
		want  string
	}{
		"under the floor": {[]ScheduleDecl{{Operation: "x", EverySeconds: 599}}, "every_seconds"},
		"a poll":          {[]ScheduleDecl{{Operation: "x", EverySeconds: 1}}, "every_seconds"},
		"over a week":     {[]ScheduleDecl{{Operation: "x", EverySeconds: MaxScheduleEvery + 1}}, "every_seconds"},
		"no interval":     {[]ScheduleDecl{{Operation: "x"}}, "every_seconds"},
		"no operation":    {[]ScheduleDecl{{EverySeconds: 600}}, "names its operation"},
		"twice":           {[]ScheduleDecl{{Operation: "x", EverySeconds: 600}, {Operation: "x", EverySeconds: 900}}, "twice"},
		"five":            {five, "at most"},
	} {
		if err := ValidateSchedule(tc.decls); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
	for _, every := range []int{MinScheduleEvery, MaxScheduleEvery} {
		if err := ValidateSchedule([]ScheduleDecl{{Operation: "x", EverySeconds: every}}); err != nil {
			t.Errorf("every_seconds %d: %v", every, err)
		}
	}
}
