package aiiospkg

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

// THE CHECKS' VECTORS ARE THE HOST'S, BYTE FOR BYTE
// (vectors/validation_checks.json): the cosine a vector_cosine_at_least is
// decided on, the JSON pointer a $ref and expect.json.path read with,
// validation.json's grammar, and the session lane's normaliser, level and
// judge. This kit's copies reach the host's verdicts
// on the same file, so a check one side passes and the other fails is a
// failing test on whichever side is wrong.
type validationVectors struct {
	Tolerance float64 `json:"tolerance"`
	Cosine    []struct {
		Name      string    `json:"name"`
		Returned  []float64 `json:"returned"`
		Reference []float64 `json:"reference"`
		Min       float64   `json:"min"`
		Pass      bool      `json:"pass"`
		Cosine    float64   `json:"cosine"`
	} `json:"cosine"`
	Pointer struct {
		Doc   interface{} `json:"doc"`
		Cases []struct {
			Pointer string      `json:"pointer"`
			Found   bool        `json:"found"`
			Value   interface{} `json:"value"`
		} `json:"cases"`
	} `json:"pointer"`
	Files []struct {
		Name          string                     `json:"name"`
		Members       map[string]json.RawMessage `json:"members"`
		BinaryMembers map[string]string          `json:"binary_members"`
		Validation    json.RawMessage            `json:"validation"`
		Host          string                     `json:"host"`
		Unresolved    []string                   `json:"unresolved"`
		NotRun        []string                   `json:"not_run"`
	} `json:"files"`
	Normalize []struct {
		Text  string   `json:"text"`
		Words []string `json:"words"`
	} `json:"normalize"`
	Levels struct {
		ToleranceDB float64 `json:"tolerance_db"`
		Cases       []struct {
			Name    string   `json:"name"`
			PCM     string   `json:"pcm"`
			RMSDBFS *float64 `json:"rms_dbfs"`
		} `json:"cases"`
	} `json:"levels"`
	Judge []struct {
		Name   string `json:"name"`
		Input  string `json:"input"`
		Expect struct {
			Order              []string `json:"order"`
			TranscriptContains []string `json:"transcript_contains"`
			AudioAtLeast       *struct {
				MS      int64   `json:"ms"`
				RMSDBFS float64 `json:"rms_dbfs"`
			} `json:"audio_at_least"`
		} `json:"expect"`
		Answer struct {
			Events []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				Withheld     bool   `json:"withheld"`
				SynthesisID  string `json:"synthesis_id"`
				Reason       string `json:"reason"`
				ReasonCode   string `json:"reason_code"`
				RetryMayPass bool   `json:"retry_may_pass"`
			} `json:"events"`
			Complete    bool   `json:"complete"`
			SynthesisID string `json:"synthesis_id"`
			Refused     *struct {
				Control string `json:"control"`
				Words   string `json:"words"`
			} `json:"refused"`
			Audio *struct {
				Samples int64    `json:"samples"`
				Rate    int      `json:"rate"`
				RMSDBFS *float64 `json:"rms_dbfs"`
			} `json:"audio"`
		} `json:"answer"`
		Outcome string `json:"outcome"`
		Words   string `json:"words"`
	} `json:"judge"`
}

func loadValidationVectors(t *testing.T) validationVectors {
	t.Helper()
	raw, err := os.ReadFile("../../vectors/validation_checks.json")
	if err != nil {
		t.Fatal(err)
	}
	var v validationVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cosine) < 8 || len(v.Pointer.Cases) < 10 || len(v.Files) < 60 || len(v.Normalize) < 8 || len(v.Levels.Cases) < 5 || len(v.Judge) < 25 {
		t.Fatalf("the vectors are thin: cosine %d, pointer %d, files %d, normalize %d, levels %d, judge %d",
			len(v.Cosine), len(v.Pointer.Cases), len(v.Files), len(v.Normalize), len(v.Levels.Cases), len(v.Judge))
	}
	return v
}

func TestValidationVectors(t *testing.T) {
	v := loadValidationVectors(t)
	t.Run("cosine", func(t *testing.T) {
		for _, c := range v.Cosine {
			got := make([]float32, len(c.Returned))
			for i, x := range c.Returned {
				got[i] = float32(x)
			}
			cos := FloatCosine(got, c.Reference)
			if math.Abs(cos-c.Cosine) > v.Tolerance {
				t.Errorf("%s: cosine %.17g, the host's %.17g", c.Name, cos, c.Cosine)
			}
			if pass := cos >= c.Min; pass != c.Pass {
				t.Errorf("%s: %.17g against %g passes %v, the host's %v", c.Name, cos, c.Min, pass, c.Pass)
			}
		}
	})
	t.Run("pointer", func(t *testing.T) {
		for _, c := range v.Pointer.Cases {
			got, found := Pointer(v.Pointer.Doc, c.Pointer)
			if found != c.Found || (found && !JSONEqual(got, c.Value)) {
				t.Errorf("pointer %q: %v %v, the host's %v %v", c.Pointer, got, found, c.Value, c.Found)
			}
		}
	})
	t.Run("grammar", func(t *testing.T) {
		for _, f := range v.Files {
			got, err := ParseValidation(f.Validation, func(rel string) ([]byte, bool, error) {
				if b64, ok := f.BinaryMembers[rel]; ok {
					raw, err := base64.StdEncoding.DecodeString(b64)
					if err != nil {
						t.Fatalf("%s: binary member %s: %v", f.Name, rel, err)
					}
					return raw, true, nil
				}
				raw, ok := f.Members[rel]
				return raw, ok, nil
			})
			if (err != nil) != (f.Host == "refuse") {
				t.Errorf("%s: read %v, the host's %s", f.Name, err, f.Host)
				continue
			}
			if err != nil {
				continue
			}
			var unresolved, notRun []string
			for _, c := range got.Checks {
				if c.Unresolved != "" {
					unresolved = append(unresolved, c.Name)
				}
				if !KnownLane(c.Lane) {
					notRun = append(notRun, c.Name)
				}
			}
			if len(unresolved)+len(f.Unresolved) > 0 && !reflect.DeepEqual(unresolved, f.Unresolved) {
				t.Errorf("%s: unresolved %v, the host's %v", f.Name, unresolved, f.Unresolved)
			}
			if len(notRun)+len(f.NotRun) > 0 && !reflect.DeepEqual(notRun, f.NotRun) {
				t.Errorf("%s: not run %v, the host's %v", f.Name, notRun, f.NotRun)
			}
		}
	})
	t.Run("normalize", func(t *testing.T) {
		for _, c := range v.Normalize {
			if got := Normalize(c.Text); len(got)+len(c.Words) > 0 && !reflect.DeepEqual(got, c.Words) {
				t.Errorf("%q: %q, the host's %q", c.Text, got, c.Words)
			}
		}
	})
	t.Run("levels", func(t *testing.T) {
		for _, c := range v.Levels.Cases {
			pcm, err := base64.StdEncoding.DecodeString(c.PCM)
			if err != nil {
				t.Fatal(err)
			}
			got := RMSDBFS(pcm)
			switch {
			case c.RMSDBFS == nil:
				if !math.IsInf(got, -1) {
					t.Errorf("%s: %v, the host's no level at all", c.Name, got)
				}
			case math.Abs(got-*c.RMSDBFS) > v.Levels.ToleranceDB:
				t.Errorf("%s: %.12f dBFS, the host's %.12f", c.Name, got, *c.RMSDBFS)
			}
		}
	})
	t.Run("judge", func(t *testing.T) {
		for _, c := range v.Judge {
			e := &EventsExpect{Order: c.Expect.Order, TranscriptContains: c.Expect.TranscriptContains}
			if f := c.Expect.AudioAtLeast; f != nil {
				e.AudioAtLeast = &AudioFloor{MS: f.MS, RMSDBFS: f.RMSDBFS}
			}
			a := SessionAnswer{Complete: c.Answer.Complete, SynthesisID: c.Answer.SynthesisID, Level: math.Inf(-1)}
			for _, ev := range c.Answer.Events {
				a.Events = append(a.Events, SessionEvent{Type: ev.Type, Text: ev.Text, Withheld: ev.Withheld, SynthesisID: ev.SynthesisID,
					Reason: ev.Reason, ReasonCode: ev.ReasonCode, RetryMayPass: ev.RetryMayPass})
			}
			if r := c.Answer.Refused; r != nil {
				a.Refused = &SessionRefusal{Control: r.Control, Words: r.Words}
			}
			if au := c.Answer.Audio; au != nil {
				a.Samples, a.Rate = au.Samples, au.Rate
				if au.RMSDBFS != nil {
					a.Level = *au.RMSDBFS
				}
			}
			outcome, words := JudgeSession(e, c.Input == "text", a)
			if outcome != c.Outcome || !strings.Contains(words, c.Words) {
				t.Errorf("%s: %s %q, the host's %s %q", c.Name, outcome, words, c.Outcome, c.Words)
			}
		}
	})
}
