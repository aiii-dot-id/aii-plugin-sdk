//go:build !wasm_unknown

package aiiosdk

import (
	"fmt"
	"testing"
)

func TestRecallPreservesSelectionContextOnEmptyAndMissingResults(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, tc := range []struct {
			raw  string
			want *MemorySelectionContext
		}{
			{"", nil},
			{`{"selection":""}`, &MemorySelectionContext{}},
			{`{"selection":"relevant","reason":"no_session"}`, &MemorySelectionContext{Selection: "relevant", Reason: "no_session"}},
			{`{"selection":"project","project_id":"project-a"}`, &MemorySelectionContext{Selection: "project", ProjectID: "project-a"}},
			{`{"selection":"task","project_id":"project-a","work_session_id":"work-a"}`, &MemorySelectionContext{Selection: "task", ProjectID: "project-a", WorkSessionID: "work-a"}},
			{`{"selection":"reusable"}`, &MemorySelectionContext{Selection: "reusable"}},
		} {
			withHost(t, func([]byte) ([]byte, error) {
				context := ""
				if tc.raw != "" {
					context = `,"selection_context":` + tc.raw
				}
				if missing {
					return []byte(`{"status":"failed","reasonCode":"MEMORY_NOT_FOUND","operation_result":{"status":"found_nothing"` + context + `}}`), nil
				}
				return []byte(`{"status":"succeeded","operation_result":{"status":"found_nothing","hits":[],"matched":0,"shown":0` + context + `}}`), nil
			})
			var opts []RecallOption
			if missing {
				opts = append(opts, ByID("absent-note"))
			}
			got, err := Memory.Recall("", opts...)
			if err != nil || got.Status != "found_nothing" || len(got.Hits) != 0 {
				t.Fatal(got, err)
			}
			if (got.SelectionContext == nil) != (tc.want == nil) || tc.want != nil && *got.SelectionContext != *tc.want {
				t.Fatalf("missing=%v: context=%+v want=%+v", missing, got.SelectionContext, tc.want)
			}
		}
	}
}

func TestRecallRejectsMalformedSelectionReadbackWithoutGuessing(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"selection":false}`, `{"selection":"project","project_id":42}`, `{"selection":"task","work_session_id":[]}`, `{"selection":"relevant","reason":null}`} {
		for _, missing := range []bool{false, true} {
			withHost(t, func([]byte) ([]byte, error) {
				status, reason := "succeeded", ""
				if missing {
					status = "failed"
					reason = `,"reasonCode":"MEMORY_NOT_FOUND"`
				}
				return []byte(fmt.Sprintf(`{"status":%q%s,"operation_result":{"hits":[],"selection_context":%s}}`, status, reason, raw)), nil
			})
			if _, err := Memory.Recall("query"); err == nil {
				t.Fatalf("malformed readback accepted: %s missing=%v", raw, missing)
			}
		}
	}
}
