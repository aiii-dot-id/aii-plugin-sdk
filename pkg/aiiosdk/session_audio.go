package aiiosdk

import "fmt"

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
const (
	// .
	// .
	// .
	TopologyControlOnly = "control_only"
	// .
	TopologyDuplex = "duplex"
	// .
	TopologyOutputOnly = "output_only"
)

// .
// .
type SessionFormat struct {
	Rate     int
	Channels int
}

// .
type SessionAudio struct {
	Topology     string
	InputHandle  string
	OutputHandle string
	Input        SessionFormat
	Output       SessionFormat
}

// .
func (a SessionAudio) HasInput() bool { return a.Topology == TopologyDuplex }

// .
// .
// .
// .
// .
func ParseSessionAudio(args Object) (SessionAudio, error) {
	if !args.Has("audio") {
		if args.Has("input_handle") || args.Has("output_handle") {
			return SessionAudio{}, fmt.Errorf("open refused: endpoint handles with no audio object — a handle needs its format")
		}
		return SessionAudio{Topology: TopologyControlOnly}, nil
	}
	au := Object(args.Raw("audio"))
	if format, ok := au.String("format"); au.Has("format") && (!ok || format != "s16le") {
		return SessionAudio{}, fmt.Errorf("open refused: audio.format must be s16le")
	}
	output, ok, err := sessionFormat(au, "output")
	if err != nil {
		return SessionAudio{}, err
	}
	if !ok {
		return SessionAudio{}, fmt.Errorf("open refused: audio.output is required — there is no session without an output direction")
	}
	outHandle, _ := args.String("output_handle")
	if outHandle == "" {
		return SessionAudio{}, fmt.Errorf("open refused: an output format with no output_handle")
	}
	if !au.Has("input") {
		return SessionAudio{}, fmt.Errorf("open refused: audio.input is missing — an object for a duplex session, null for an output-only one; omission is not a topology")
	}
	input, hasInput, err := sessionFormat(au, "input")
	if err != nil {
		return SessionAudio{}, err
	}
	if !hasInput {
		if args.Has("input_handle") {
			return SessionAudio{}, fmt.Errorf("open refused: audio.input is null and an input_handle was given — an output-only session names no input at all")
		}
		return SessionAudio{Topology: TopologyOutputOnly, OutputHandle: outHandle, Output: output}, nil
	}
	inHandle, _ := args.String("input_handle")
	if inHandle == "" {
		return SessionAudio{}, fmt.Errorf("open refused: an input format with no input_handle")
	}
	return SessionAudio{Topology: TopologyDuplex, InputHandle: inHandle, OutputHandle: outHandle, Input: input, Output: output}, nil
}

// .
// .
func sessionFormat(au Object, name string) (f SessionFormat, present bool, err error) {
	raw := au.Raw(name)
	if raw == nil || string(raw) == "null" {
		return SessionFormat{}, false, nil
	}
	dir := Object(raw)
	rate, okR := dir.Int("rate")
	ch, okC := dir.Int("channels")
	if !okR || !okC || rate <= 0 || ch <= 0 {
		return SessionFormat{}, false, fmt.Errorf("open refused: audio.%s needs a rate and a channel count", name)
	}
	return SessionFormat{Rate: int(rate), Channels: int(ch)}, true, nil
}

// .
// .
// .
// .
// .
func (a SessionAudio) Admission(input, output SessionFormat) map[string]any {
	out := map[string]any{"rate": output.Rate, "channels": output.Channels}
	switch a.Topology {
	case TopologyDuplex:
		return map[string]any{"input": map[string]any{"rate": input.Rate, "channels": input.Channels}, "output": out}
	case TopologyOutputOnly:
		return map[string]any{"input": nil, "output": out}
	}
	return nil
}
