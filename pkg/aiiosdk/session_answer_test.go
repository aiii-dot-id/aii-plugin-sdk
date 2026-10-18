//go:build !wasm_unknown

package aiiosdk

// answerWith adapts a value-or-error handler to the lane's Control shape.
// It lives in the tests, not the API: one mechanism is the point, and an
// engine that wants this shape writes these three lines itself — as
// examples/voice-skel does.
func answerWith(fn func(*Session, string, Object) (any, error)) SessionAdmit {
	return func(c *Control) { c.Answer(fn(c.Session, c.Op, c.Args)) }
}
