package aiiosdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// .
// .
// .
func TestParseSessionAudioAgainstTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/session_topology.json")
	if err != nil {
		t.Fatal(err)
	}
	var vec struct {
		Requests []struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Topology  string          `json:"topology"`
		} `json:"open_requests"`
		Admissions []struct {
			Name      string          `json:"name"`
			Requested string          `json:"requested"`
			Result    json.RawMessage `json:"result"`
			Confirmed bool            `json:"confirmed"`
		} `json:"open_admissions"`
	}
	if err := json.Unmarshal(raw, &vec); err != nil {
		t.Fatal(err)
	}
	if len(vec.Requests) < 10 {
		t.Fatalf("the vector file carries %d requests", len(vec.Requests))
	}
	for _, c := range vec.Requests {
		got, err := ParseSessionAudio(Object(c.Arguments))
		if c.Topology == "invalid" {
			if err == nil {
				t.Errorf("%s: accepted as %s", c.Name, got.Topology)
			} else if !strings.HasPrefix(err.Error(), "open refused: ") {
				t.Errorf("%s: the refusal must read as one: %v", c.Name, err)
			}
			continue
		}
		if err != nil || got.Topology != c.Topology {
			t.Errorf("%s: topology=%q err=%v, want %q", c.Name, got.Topology, err, c.Topology)
			continue
		}
		if got.HasInput() != (c.Topology == TopologyDuplex) || (got.InputHandle != "") != got.HasInput() || (got.Input != SessionFormat{}) != got.HasInput() {
			t.Errorf("%s: an input exists exactly when the session is duplex: %+v", c.Name, got)
		}
	}

	// .
	// .
	in, out := SessionFormat{Rate: 16000, Channels: 1}, SessionFormat{Rate: 24000, Channels: 1}
	for topology, a := range map[string]SessionAudio{
		"duplex":      {Topology: TopologyDuplex},
		"output_only": {Topology: TopologyOutputOnly},
	} {
		built, _ := json.Marshal(map[string]any{"accepted": true, "audio": a.Admission(in, out)})
		matched := false
		for _, c := range vec.Admissions {
			if c.Requested != topology || !c.Confirmed {
				continue
			}
			var want, got any
			_ = json.Unmarshal(c.Result, &want)
			_ = json.Unmarshal(built, &got)
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) == string(gotJSON) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%s: the admission this kit builds is not one the vectors call confirmed: %s", topology, built)
		}
	}
	if (SessionAudio{Topology: TopologyControlOnly}).Admission(in, out) != nil {
		t.Error("a control-only session has no audio to confirm")
	}
}
