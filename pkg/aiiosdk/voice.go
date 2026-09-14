package aiiosdk

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
// .
// .
// .
// .
// .
// .

// .
type VoiceClient struct{}

// .
var Voice VoiceClient

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
func (VoiceClient) Observe(text, speaker string) error {
	return Voice.ObserveWithScore(text, speaker, 0)
}

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
// .
// .
func (VoiceClient) ObserveWithScore(text, speaker string, speakerScore float64) error {
	_, err := InvokeCall("voice.observe", nil, map[string]any{
		"text":          text,
		"speaker":       speaker,
		"speaker_score": speakerScore,
	})
	return err
}
