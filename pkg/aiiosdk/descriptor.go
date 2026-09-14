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

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// .
const (
	EffectsReadInternal  = "read.internal"
	EffectsReadExternal  = "read.external"
	EffectsWriteLocal    = "write.local"
	EffectsWriteExternal = "write.external"
	EffectsExec          = "exec"
)

// .
// .
// .
// .
type Descriptor struct {
	// .
	// .
	// .
	ID string `json:"id"`
	// .
	Summary string `json:"summary"`
	// .
	// .
	Input string `json:"input"`
	// .
	// .
	Output string `json:"output"`
	// .
	// .
	Effects string `json:"effects"`
	// .
	// .
	Capabilities []string `json:"capabilities"`
	// .
	// .
	MaxResultBytes int `json:"max_result_bytes,omitempty"`

	// .
	// .
	// .
	// .
	// .
	// .
	Family   string   `json:"family,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	Examples []string `json:"examples,omitempty"`

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
	OperatorConfirms bool `json:"operator_confirms,omitempty"`
}

// .
// .
// .
// .
type OperatorActStamp struct {
	ID          string
	ConfirmedAt string
}

// .
// .
// .
// .
func OperatorAct(args Object) (stamp OperatorActStamp, ok bool) {
	act := args.Object("_host_operator_act")
	if act == nil {
		return OperatorActStamp{}, false
	}
	id, _ := act.String("id")
	at, _ := act.String("confirmed_at")
	if id == "" {
		return OperatorActStamp{}, false
	}
	return OperatorActStamp{ID: id, ConfirmedAt: at}, true
}

// .
func (p *Plugin) Describe(op string, d Descriptor) *Plugin {
	if op == "" {
		panic("aiiosdk: Describe requires an operation name")
	}
	if d.ID == "" {
		d.ID = op
	}
	if d.ID != op {
		panic("aiiosdk: descriptor id " + d.ID + " does not match operation " + op)
	}
	if _, dup := p.descriptors[op]; dup {
		panic("aiiosdk: duplicate descriptor for operation " + op)
	}
	if d.Capabilities == nil {
		d.Capabilities = []string{}
	}
	p.descriptors[op] = d
	return p
}

// .
// .
func (p *Plugin) Operations() []string {
	ops := make([]string, 0, len(p.handlers))
	for op := range p.handlers {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// .
// .
// .
func (p *Plugin) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(p.handlers))
	for _, op := range p.Operations() {
		if d, ok := p.descriptors[op]; ok {
			out = append(out, d)
		} else {
			out = append(out, Descriptor{ID: op, Capabilities: []string{}})
		}
	}
	return out
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
func (p *Plugin) DescriptorsJSON() ([]byte, error) {
	if err := p.checkDescriptorDrift(); err != nil {
		return nil, err
	}
	out := []byte{'['}
	for i, d := range p.Descriptors() {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"id":`...)
		out = appendJSONString(out, d.ID)
		out = append(out, `,"summary":`...)
		out = appendJSONString(out, d.Summary)
		out = append(out, `,"input":`...)
		out = appendJSONString(out, d.Input)
		out = append(out, `,"output":`...)
		out = appendJSONString(out, d.Output)
		out = append(out, `,"effects":`...)
		out = appendJSONString(out, d.Effects)
		out = append(out, `,"capabilities":[`...)
		for j, c := range d.Capabilities {
			if j > 0 {
				out = append(out, ',')
			}
			out = appendJSONString(out, c)
		}
		out = append(out, ']')
		if d.MaxResultBytes > 0 {
			out = append(out, `,"max_result_bytes":`...)
			out = appendInt(out, d.MaxResultBytes)
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
		if d.Family != "" {
			out = append(out, `,"family":`...)
			out = appendJSONString(out, d.Family)
		}
		if len(d.Keywords) > 0 {
			out = append(out, `,"keywords":[`...)
			for j, k := range d.Keywords {
				if j > 0 {
					out = append(out, ',')
				}
				out = appendJSONString(out, k)
			}
			out = append(out, ']')
		}
		if len(d.Examples) > 0 {
			out = append(out, `,"examples":[`...)
			for j, e := range d.Examples {
				if j > 0 {
					out = append(out, ',')
				}
				out = appendJSONString(out, e)
			}
			out = append(out, ']')
		}
		if d.OperatorConfirms {
			out = append(out, `,"operator_confirms":true`...)
		}
		out = append(out, '}')
	}
	return append(out, ']'), nil
}

// .
// .
// .
// .
// .
func (p *Plugin) manifestInterfaces(interfaceID string, version int) ([]byte, error) {
	if interfaceID == "" || version < 1 {
		return nil, fmt.Errorf("aiiosdk: manifest interface needs an id and a version >= 1")
	}
	if len(p.handlers) == 0 {
		return nil, fmt.Errorf("aiiosdk: no handled operations; a plugin manifest requires at least one method (manifest.go:333-334)")
	}
	schemaBytes, err := p.DescriptorsJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(schemaBytes)
	// .
	// .
	out := appendJSONString([]byte(`{"core":[{"id":`), interfaceID)
	out = append(out, `,"version":`...)
	out = appendInt(out, version)
	out = append(out, `,"schema_hash":`...)
	out = appendJSONString(out, "sha256:"+hex.EncodeToString(sum[:]))
	out = append(out, `,"methods":[`...)
	for i, op := range p.Operations() {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendJSONString(out, op)
	}
	out = append(out, `]}]}`...)
	return out, nil
}

// .
// .
// .
// .
func (p *Plugin) checkDescriptorDrift() error {
	for op := range p.descriptors {
		if _, ok := p.handlers[op]; !ok {
			return fmt.Errorf("aiiosdk: operation %s is described but has no handler", op)
		}
	}
	return nil
}
