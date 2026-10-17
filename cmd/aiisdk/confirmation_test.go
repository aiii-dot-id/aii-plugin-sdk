package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// NO WRITE REACHES A THIRD PARTY BY OMISSION. An operation that writes
// externally and declares no operator_confirms is dispatched by the host on the
// identity's call, with no card. Whoever meant that says so, with the reason,
// in one of the operation's own cases; whoever forgot is told.

const account = `[
 {"id":"mail.read","effects":"read.external"},
 {"id":"mail.draft","effects":"write.external"},
 {"id":"mail.send","effects":"write.external","operator_confirms":true},
 {"id":"kv.put","effects":"write.local"}
]`

func TestAnExternalWriteMustDeclareItsConfirmationOrSayWhyNot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		family string
		marks  map[string]string
		want   []string
	}{
		{"a draft marked with its reason", "tool_bridge", map[string]string{"mail.draft": "a draft reaches no one"}, nil},
		{"an unmarked write", "tool_bridge", nil,
			[]string{`mail.draft writes to a third party and declares no operator_confirms — declare it, or mark one of its cases "unconfirmed_by_design" with the reason`}},
		{"a mark with no reason", "tool_bridge", map[string]string{"mail.draft": "  "},
			[]string{"mail.draft is marked unconfirmed_by_design with no reason"}},
		{"a mark on a confirmed operation", "tool_bridge", map[string]string{"mail.draft": "why", "mail.send": "why"},
			[]string{"mail.send is marked unconfirmed_by_design and declares operator_confirms — the mark says the opposite"}},
		{"a mark on a read", "tool_bridge", map[string]string{"mail.draft": "why", "mail.read": "why"},
			[]string{"mail.read is marked unconfirmed_by_design and is not a write.external — the mark excuses nothing"}},
		{"a mark on a local write", "tool_bridge", map[string]string{"mail.draft": "why", "kv.put": "why"},
			[]string{"kv.put is marked unconfirmed_by_design and is not a write.external — the mark excuses nothing"}},
		{"a mark on an operation that is not there", "tool_bridge", map[string]string{"mail.draft": "why", "mail.gone": "why"},
			[]string{"mail.gone is marked unconfirmed_by_design and the plugin declares no such operation"}},
		{"a channel adapter is the host's", "channel_adapter", nil, nil},
	} {
		if got := confirmationStance(tc.family, []byte(account), tc.marks); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

func TestTheMarksAreReadFromTheCaseFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	files := []string{
		write("01.json", `{"name":"a draft","operation":"mail.draft","unconfirmed_by_design":"a draft reaches no one","arguments":{}}`),
		write("02.json", `{"name":"a send","operation":"mail.send","operator_act":true,"arguments":{}}`),
		write("03.json", `{"name":"empty reason","operation":"kv.put","unconfirmed_by_design":"","arguments":{}}`),
		write("04.json", `not json`),
	}
	got := unconfirmedMarks(files)
	want := map[string]string{"mail.draft": "a draft reaches no one", "kv.put": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("marks %v, want %v", got, want)
	}
	if problems := confirmationStance("tool_bridge", []byte(account), got); len(problems) != 1 ||
		!strings.Contains(problems[0], "kv.put is marked unconfirmed_by_design with no reason") {
		t.Fatalf("an empty reason is not a finding: %q", problems)
	}
}
