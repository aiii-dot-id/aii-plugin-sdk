package aiiosdk

import (
	"encoding/json"
	"fmt"
)

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
var Tools ToolsClient

// .
type ToolsClient struct{}

// .
type PublishSpec struct {
	Name         string
	Summary      string
	Input        json.RawMessage
	Effects      string
	Capabilities []string
	Family       string
}

// .
// .
func (ToolsClient) Publish(spec PublishSpec) (string, error) {
	if spec.Name == "" || spec.Summary == "" || spec.Effects == "" {
		return "", fmt.Errorf("aiiosdk: Publish requires a name, a summary and an effect class")
	}
	args := map[string]any{"name": spec.Name, "summary": spec.Summary, "effects": spec.Effects, "capabilities": stringsToAny(spec.Capabilities)}
	if len(spec.Input) > 0 {
		args["input"] = spec.Input
	}
	if spec.Family != "" {
		args["family"] = spec.Family
	}
	res, err := InvokeCall("tools.publish", nil, args)
	if err != nil {
		return "", err
	}
	name, _ := Object(res.OperationResult).String("tool")
	return name, nil
}

// .
func (ToolsClient) Withdraw(name string) error {
	_, err := InvokeCall("tools.withdraw", map[string]any{"name": name}, nil)
	return err
}

func stringsToAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// .
// .
type Event struct {
	Topic   string
	At      string
	payload Object
}

// .
func ParseEvent(c Call) Event {
	args := c.Args()
	var e Event
	e.Topic, _ = args.String("topic")
	e.At, _ = args.String("at")
	e.payload = args.Object("payload")
	return e
}

// .
func (e Event) Payload() Object {
	if e.payload == nil {
		return Object([]byte("{}"))
	}
	return e.payload
}
