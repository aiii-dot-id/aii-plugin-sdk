package aiiospkg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const LaneSession = "session"

func KnownLane(lane string) bool {
	return lane == LaneOperation || lane == LaneEmbeddings || lane == LaneSession
}

const (
	MaxCheckTextChars   = 1000
	MaxEventOrder       = 16
	MaxTranscriptWords  = 16
	MaxPhraseChars      = 120
	MaxAudioFloorMS     = 600000
	MinAudioFloorDBFS   = -120
	MaxCheckAudioBytes  = 8 << 20
	MinCheckRecording   = 100 * time.Millisecond
	MaxCheckRecording   = 30 * time.Second
	maxSessionEventName = 64
)

type EventsExpect struct {
	Order              []string
	TranscriptContains []string
	AudioAtLeast       *AudioFloor
}

type AudioFloor struct {
	MS      int64
	RMSDBFS float64
}

var (
	telemetryEvents = map[string]bool{"vad_probability": true, "transcript_partial": true}
	terminalEvents  = map[string]bool{"session_end": true, "cancellation": true, "failure": true}
	inputEvents     = map[string]bool{"transcript_partial": true, "transcript_final": true, "speaker_observation": true, "input_finished": true}
)

func SessionEventClass(typ string) (telemetry, terminal, input bool) {
	return telemetryEvents[typ], terminalEvents[typ], inputEvents[typ]
}

func sessionInput(v interface{}) (audioPath, text string, err error) {
	m, isObj := v.(map[string]interface{})
	if !isObj {
		return "", "", errors.New(`must be an object holding "audio", a recording the package carries, or "text", a text to speak`)
	}
	for k := range m {
		if k != "audio" && k != "text" {
			return "", "", fmt.Errorf("holds %q, which a session check's input does not have (audio, text)", k)
		}
	}
	a, hasAudio := m["audio"]
	t, hasText := m["text"]
	switch {
	case hasAudio == hasText:
		return "", "", errors.New(`holds exactly one of "audio" and "text"`)
	case hasAudio:
		p, isString := a.(string)
		if !isString || p == "" {
			return "", "", errors.New("audio must name a member of the package")
		}
		return p, "", nil
	}
	s, isString := t.(string)
	if !isString || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > MaxCheckTextChars {
		return "", "", fmt.Errorf("text must be 1..%d characters, not white space only", MaxCheckTextChars)
	}
	return "", s, nil
}

func sessionEvents(v interface{}, hearsAudio, speaksText bool) (*EventsExpect, error) {
	m, isObj := v.(map[string]interface{})
	if !isObj || len(m) == 0 {
		return nil, errors.New("must be an object holding at least one of order, transcript_contains, audio_at_least")
	}
	for k := range m {
		if k != "order" && k != "transcript_contains" && k != "audio_at_least" {
			return nil, fmt.Errorf("holds %q, which expect.events does not have (order, transcript_contains, audio_at_least)", k)
		}
	}
	out := &EventsExpect{}
	if o, has := m["order"]; has {
		items, isList := o.([]interface{})
		if !isList || len(items) < 1 || len(items) > MaxEventOrder {
			return nil, fmt.Errorf("order must list 1..%d event types", MaxEventOrder)
		}
		for _, item := range items {
			typ, isString := item.(string)
			if !isString || !validEventName(typ) {
				return nil, fmt.Errorf("order: %v is not an event type", item)
			}
			telemetry, terminal, input := SessionEventClass(typ)
			switch {
			case telemetry:
				return nil, fmt.Errorf("order names %s, which an engine may shed under pressure: nothing may depend on it", typ)
			case terminal:
				return nil, fmt.Errorf("order names %s, which ends a session and never answers one", typ)
			case input && speaksText:
				return nil, fmt.Errorf("order names %s, which a text's check never hears: it has no input", typ)
			}
			out.Order = append(out.Order, typ)
		}
	}
	if p, has := m["transcript_contains"]; has {
		if speaksText {
			return nil, errors.New("transcript_contains is a recording's expectation; a text's check hears nothing")
		}
		items, isList := p.([]interface{})
		if !isList || len(items) < 1 || len(items) > MaxTranscriptWords {
			return nil, fmt.Errorf("transcript_contains must list 1..%d phrases", MaxTranscriptWords)
		}
		for _, item := range items {
			phrase, isString := item.(string)
			if !isString || phrase == "" || utf8.RuneCountInString(phrase) > MaxPhraseChars {
				return nil, fmt.Errorf("transcript_contains: each phrase is 1..%d characters", MaxPhraseChars)
			}
			if len(Normalize(phrase)) == 0 {
				return nil, fmt.Errorf("transcript_contains: %q holds no word to listen for", phrase)
			}
			out.TranscriptContains = append(out.TranscriptContains, phrase)
		}
	}
	if a, has := m["audio_at_least"]; has {
		if hearsAudio {
			return nil, errors.New("audio_at_least is a text's expectation; a recording's check is judged by what the engine hears")
		}
		o, isObj := a.(map[string]interface{})
		ms, isInt := checkMilliseconds(o["ms"])
		db, isNum := o["rms_dbfs"].(float64)
		switch {
		case !isObj || len(o) != 2:
			return nil, errors.New(`audio_at_least must be an object of two members, "ms" and "rms_dbfs"`)
		case !isInt || ms < 1 || ms > MaxAudioFloorMS:
			return nil, fmt.Errorf("audio_at_least.ms must be a whole number of milliseconds, 1..%d", MaxAudioFloorMS)
		case !isNum || db < MinAudioFloorDBFS || db > 0:
			return nil, fmt.Errorf("audio_at_least.rms_dbfs must be a number from %d to 0", MinAudioFloorDBFS)
		}
		out.AudioAtLeast = &AudioFloor{MS: ms, RMSDBFS: db}
	}
	return out, nil
}

func validEventName(s string) bool {
	if s == "" || len(s) > maxSessionEventName || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_') {
			return false
		}
	}
	return true
}

type Recording struct {
	Rate, Channels int
	PCM            []byte
	Length         time.Duration
}

func ReadRecording(b []byte) (Recording, string) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return Recording{}, "is not a WAV recording: not a WAV file"
	}
	var tag uint16
	var rate, channels, bits int
	haveFmt := false
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int64(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(b) {
				return Recording{}, "is not a WAV recording: the WAV's fmt chunk is short"
			}
			tag = binary.LittleEndian.Uint16(b[body:])
			channels = int(binary.LittleEndian.Uint16(b[body+2:]))
			rate = int(binary.LittleEndian.Uint32(b[body+4:]))
			bits = int(binary.LittleEndian.Uint16(b[body+14:]))
			haveFmt = true
		case "data":
			if !haveFmt {
				return Recording{}, "is not a WAV recording: the WAV has samples before its format"
			}
			end := int64(body) + size
			if size == 0 || size == 0xFFFFFFFF || end > int64(len(b)) {
				end = int64(len(b))
			}
			pcm := b[body:end]
			switch {
			case tag != 1 || bits != 16:
				return Recording{}, "is not 16-bit PCM"
			case channels < 1 || channels > 2 || rate < 8000 || rate > 192000:
				return Recording{}, fmt.Sprintf("holds %d channel(s) at %d Hz; a recording is one or two channels at 8 to 192 kHz", channels, rate)
			case size == 0 || size == 0xFFFFFFFF || size != int64(len(pcm)):
				return Recording{}, "does not hold exactly the samples its data chunk declares"
			case len(pcm)%(2*channels) != 0:
				return Recording{}, "ends inside a sample"
			}
			d := time.Duration(len(pcm)/(2*channels)) * time.Second / time.Duration(rate)
			if d < MinCheckRecording || d > MaxCheckRecording {
				return Recording{}, fmt.Sprintf("is %d ms long; a recording is %d ms to %d s", d.Milliseconds(), MinCheckRecording.Milliseconds(), int(MaxCheckRecording.Seconds()))
			}
			return Recording{Rate: rate, Channels: channels, PCM: pcm, Length: d}, ""
		}
		next := int64(body) + size + size%2
		if next > int64(len(b)) {
			break
		}
		off = int(next)
	}
	return Recording{}, "is not a WAV recording: the WAV has no data chunk"
}

var contractions = map[string]string{
	"don't": "do not", "won't": "will not", "can't": "cannot",
	"isn't": "is not", "aren't": "are not", "wasn't": "was not",
	"weren't": "were not", "doesn't": "does not", "didn't": "did not",
	"couldn't": "could not", "shouldn't": "should not",
	"wouldn't": "would not", "haven't": "have not", "hasn't": "has not",
	"it's": "it is", "that's": "that is", "there's": "there is",
	"what's": "what is", "here's": "here is", "he's": "he is",
	"she's": "she is", "who's": "who is",
	"i'm": "i am", "i've": "i have", "i'll": "i will", "i'd": "i would",
	"we're": "we are", "we've": "we have", "we'll": "we will",
	"they're": "they are", "they've": "they have", "they'll": "they will",
	"you're": "you are", "you've": "you have", "you'll": "you will",
	"let's": "let us",
}

var digitWords = map[rune]string{
	'0': "zero", '1': "one", '2': "two", '3': "three", '4': "four",
	'5': "five", '6': "six", '7': "seven", '8': "eight", '9': "nine",
}

func Normalize(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			b.WriteRune(r)
		case r == '\'' || r == '’':
			b.WriteRune('\'')
		default:
			b.WriteRune(' ')
		}
	}
	out := make([]string, 0, 16)
	for _, w := range strings.Fields(b.String()) {
		if exp, ok := contractions[w]; ok {
			out = append(out, strings.Fields(exp)...)
			continue
		}
		w = strings.ReplaceAll(w, "'", "")
		if allASCIIDigits(w) {
			for _, r := range w {
				out = append(out, digitWords[r])
			}
			continue
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func allASCIIDigits(w string) bool {
	if w == "" {
		return false
	}
	for i := 0; i < len(w); i++ {
		if w[i] < '0' || w[i] > '9' {
			return false
		}
	}
	return true
}

type Level struct {
	sum int64
	n   int64
}

func (l *Level) Add(pcm []byte) {
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		l.sum += v * v
		l.n++
	}
}

func (l Level) DBFS() float64 {
	if l.sum == 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(math.Sqrt(float64(l.sum)/float64(l.n))/32768)
}

func RMSDBFS(pcm []byte) float64 {
	var l Level
	l.Add(pcm)
	return l.DBFS()
}

type SessionEvent struct {
	Type         string
	Text         string
	Withheld     bool
	SynthesisID  string
	Reason       string
	ReasonCode   string
	RetryMayPass bool
}

type SessionRefusal struct {
	Control string
	Words   string
}

type SessionAnswer struct {
	Events      []SessionEvent
	Complete    bool
	Refused     *SessionRefusal
	SynthesisID string
	Samples     int64
	Rate        int
	Level       float64
}

const (
	OutcomePassed   = "passed"
	OutcomeWrong    = "wrong"
	OutcomeTimedOut = "timed_out"
)

func JudgeSession(e *EventsExpect, speaksText bool, a SessionAnswer) (outcome, words string) {
	if a.Refused != nil {
		return OutcomeWrong, fmt.Sprintf("the engine refused %s: %s", a.Refused.Control, oneLineText(a.Refused.Words))
	}
	for _, ev := range a.Events {
		cause := ""
		if ev.Reason != "" {
			cause = ": " + oneLineText(ev.Reason)
		}
		if ev.ReasonCode != "" {
			cause += " (" + ev.ReasonCode + ")"
		}
		switch {
		case ev.Type == "failure" && ev.RetryMayPass:
			return OutcomeTimedOut, "the engine failed during the check and says another try may pass" + cause
		case ev.Type == "failure":
			return OutcomeWrong, "the engine failed during the check" + cause
		case (ev.Type == "synthesis_cancelled" || ev.Type == "cancellation") && ev.SynthesisID != "" && ev.SynthesisID == a.SynthesisID:
			return OutcomeWrong, "the engine cancelled the check's own synthesis" + cause
		case ev.Type == "session_end" || (ev.Type == "cancellation" && ev.SynthesisID == ""):
			return OutcomeWrong, "the engine ended the session before its answer was complete" + cause
		}
	}
	if !a.Complete {
		if speaksText {
			return OutcomeTimedOut, "no complete answer within its bound: its reply never ended (synthesis_end and its stream's end)"
		}
		return OutcomeTimedOut, "no complete answer within its bound: the engine never said input_finished"
	}
	if e == nil {
		return OutcomeWrong, "the check expects nothing a session answers"
	}
	k := 0
	var said []string
	for _, ev := range a.Events {
		if ev.Withheld {
			continue
		}
		said = append(said, ev.Type)
		if k < len(e.Order) && ev.Type == e.Order[k] {
			k++
		}
	}
	if k < len(e.Order) {
		return OutcomeWrong, fmt.Sprintf("its events lack %q in the order it asks (%s); the engine said: %s", e.Order[k], strings.Join(e.Order, ", "), oneLineText(strings.Join(said, ", ")))
	}
	if len(e.TranscriptContains) > 0 {
		var texts []string
		withheld := 0
		for _, ev := range a.Events {
			if ev.Type != "transcript_final" {
				continue
			}
			if ev.Withheld {
				withheld++
				continue
			}
			texts = append(texts, ev.Text)
		}
		heard := Normalize(strings.Join(texts, " "))
		for _, p := range e.TranscriptContains {
			if containsRun(heard, Normalize(p)) {
				continue
			}
			words := fmt.Sprintf("its transcript lacks %q: %q", p, oneLineText(strings.Join(heard, " ")))
			switch {
			case withheld == 1:
				words += "; 1 final transcript was withheld because it did not decode"
			case withheld > 1:
				words += fmt.Sprintf("; %d final transcripts were withheld because they did not decode", withheld)
			}
			return OutcomeWrong, words
		}
	}
	if f := e.AudioAtLeast; f != nil {
		switch {
		case a.Rate <= 0 || a.Samples*1000 < f.MS*int64(a.Rate):
			ms := int64(0)
			if a.Rate > 0 {
				ms = a.Samples * 1000 / int64(a.Rate)
			}
			return OutcomeWrong, fmt.Sprintf("its reply was %d ms long, under the %d ms it asks", ms, f.MS)
		case math.IsInf(a.Level, -1):
			return OutcomeWrong, fmt.Sprintf("its reply was silent, under the %g dBFS it asks", f.RMSDBFS)
		case !(a.Level >= f.RMSDBFS):
			return OutcomeWrong, fmt.Sprintf("its reply's level was %.1f dBFS, under the %g dBFS it asks", a.Level, f.RMSDBFS)
		}
	}
	return OutcomePassed, ""
}

func containsRun(heard, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for i := 0; i+len(want) <= len(heard); i++ {
		match := true
		for j := range want {
			if heard[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func oneLineText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= 160 {
		return s
	}
	return string([]rune(s)[:160]) + "…"
}
