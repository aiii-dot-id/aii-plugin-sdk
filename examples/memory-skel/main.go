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
// .
// .
// .
// .
package main

import (
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// .
// .
// .
// .
func init() {
	p := sdk.New("com.aiii.examples.memory-skel")

	p.Describe("focus.echo", sdk.Descriptor{
		Summary: "Return the arguments unchanged",
		Input:   "schemas/echo_in.json",
		Output:  "schemas/echo_out.json",
		Effects: sdk.EffectsReadInternal,
	})
	p.Handle("focus.echo", func(c sdk.Call) (any, error) {
		// .
		// .
		return c.Arguments, nil
	})

	p.Describe("focus.set", sdk.Descriptor{
		Summary:      "Remember the current focus value",
		Input:        "schemas/set_in.json",
		Output:       "schemas/set_out.json",
		Effects:      sdk.EffectsWriteLocal,
		Capabilities: []string{"ring4.kv"},
	})
	p.Handle("focus.set", func(c sdk.Call) (any, error) {
		// .
		// .
		// .
		value, ok := c.Args().String("value")
		if !ok || value == "" {
			return nil, sdk.Fail("OPERATION_ARGUMENT_INVALID", "focus.set requires arguments.value (string)")
		}
		res, err := sdk.KV.Put("focus", value)
		if err != nil {
			return denialReport(err)
		}
		return map[string]any{"stored": true, "scope": res.Scope, "value_bytes": res.ValueBytes}, nil
	})

	p.Describe("focus.get", sdk.Descriptor{
		Summary:      "Recall the remembered focus value",
		Input:        "schemas/get_in.json",
		Output:       "schemas/get_out.json",
		Effects:      sdk.EffectsReadInternal,
		Capabilities: []string{"ring4.kv"},
	})
	p.Handle("focus.get", func(c sdk.Call) (any, error) {
		value, found, err := sdk.KV.Get("focus")
		if err != nil {
			return denialReport(err)
		}
		return map[string]any{"found": found, "value": value}, nil
	})

	p.Run()
}

// .
// .
// .
// .
// .
func denialReport(err error) (any, error) {
	if d, ok := sdk.AsDenied(err); ok {
		return map[string]any{
			"stored":     false,
			"denied":     true,
			"reasonCode": d.ReasonCode,
			"detail":     "the host denied kv for this activation; grant it in plugins.grants to enable memory",
		}, nil
	}
	return nil, err
}

// .
func main() {}
