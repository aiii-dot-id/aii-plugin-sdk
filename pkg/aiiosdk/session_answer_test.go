//go:build !wasm_unknown

package aiiosdk

// .
// .
// .
// .
func answerWith(fn func(*Session, string, Object) (any, error)) SessionAdmit {
	return func(c *Control) { c.Answer(fn(c.Session, c.Op, c.Args)) }
}
