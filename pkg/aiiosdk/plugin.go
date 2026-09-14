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
	"encoding/json"
	"strconv"
)

// .
const (
	// .
	// .
	methodInvokeCall = "invoke.call"

	// .
	msgParseError        = "JSON parse error"
	msgInvalidRequest    = "invalid JSON-RPC 2.0 request"
	msgMethodNotFound    = "method not found"
	msgHandlerFailed     = "plugin handler failed"
	msgOperationRequired = "operation (string) required"
	msgParamsNotObject   = "params must be a JSON object"
	msgNotRegistered     = "no plugin registered: call aiiosdk Run from a package init function"

	// .
	reasonMethodNotFound = "METHOD_NOT_FOUND"
	reasonHandlerFailed  = "PLUGIN_HANDLER_FAILED"
	reasonTargetInvalid  = "OPERATION_TARGET_INVALID"
	reasonArgInvalid     = "OPERATION_ARGUMENT_INVALID"

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
	reasonOperationNotFound = "OPERATION_NOT_FOUND"
	reasonNotRegistered     = "PLUGIN_NOT_REGISTERED"

	// .
	codeParse    = -32700
	codeInvalid  = -32600
	codeMethod   = -32601
	codeParams   = -32602
	codeInternal = -32603
)

// .
// .
// .
type Call struct {
	// .
	Operation string
	// .
	// .
	// .
	// .
	Target json.RawMessage
	// .
	// .
	// .
	Arguments json.RawMessage
}

// .
// .
// .
// .
// .
// .
// .
// .
func (c Call) Args() Object { return Object(c.Arguments) }

// .
// .
func (c Call) TargetObject() Object { return Object(c.Target) }

// .
// .
// .
// .
// .
// .
type Handler func(c Call) (any, error)

// .
// .
// .
// .
type Plugin struct {
	name        string
	handlers    map[string]Handler
	descriptors map[string]Descriptor
	onEvent     func(topic string, payload []byte)
	// .
	// .
	dynamic func(name string, c Call) (any, error)
}

// .
// .
func New(name string) *Plugin {
	return &Plugin{
		name:        name,
		handlers:    map[string]Handler{},
		descriptors: map[string]Descriptor{},
	}
}

// .
func (p *Plugin) Name() string { return p.name }

// .
// .
// .
// .
func (p *Plugin) Handle(op string, fn Handler) *Plugin {
	if op == "" || fn == nil {
		panic("aiiosdk: Handle requires an operation name and a handler")
	}
	if _, dup := p.handlers[op]; dup {
		panic("aiiosdk: duplicate handler for operation " + op)
	}
	p.handlers[op] = fn
	return p
}

// .
// .
// .
// .
func (p *Plugin) HandleDynamic(fn func(name string, c Call) (any, error)) *Plugin {
	p.dynamic = fn
	return p
}

// .
// .
// .
// .
func (p *Plugin) OnEvent(fn func(topic string, payload []byte)) *Plugin {
	p.onEvent = fn
	return p
}

// .
// .
// .
var current *Plugin

// .
// .
// .
func (p *Plugin) Run() {
	if current != nil {
		panic("aiiosdk: Run called twice; one guest module carries one plugin")
	}
	current = p
}

// .
// .
// .
// .
func respondCurrent(frame []byte) []byte {
	if current == nil {
		return errorResponse(bestEffortID(frame), codeInternal, msgNotRegistered, reasonNotRegistered)
	}
	return current.respond(frame)
}

// .
func (p *Plugin) respond(frame []byte) []byte {
	// .
	// .
	// .
	// .
	if err := ValidateStrict(frame); err != nil {
		return errorResponse(nullID, codeParse, msgParseError, "")
	}
	members, isObject := objectMembers(frame)
	if !isObject {
		return errorResponse(nullID, codeInvalid, msgInvalidRequest, "")
	}

	// .
	// .
	// .
	// .
	// .
	// .
	idRaw := memberByKey(members, "id")
	switch {
	case idRaw == nil:
		// .
		// .
		// .
		idRaw = nullID
	case idRaw[0] == '"', idRaw[0] == '-', idRaw[0] >= '0' && idRaw[0] <= '9':
		// .
		// .
	case string(idRaw) == "null":
		// .
	default:
		// .
		// .
		return errorResponse(nullID, codeInvalid, msgInvalidRequest, "")
	}

	// .
	// .
	if string(memberByKey(members, "jsonrpc")) != `"2.0"` {
		return errorResponse(idRaw, codeInvalid, msgInvalidRequest, "")
	}
	method, ok := decodeJSONString(memberByKey(members, "method"))
	if !ok {
		return errorResponse(idRaw, codeInvalid, msgInvalidRequest, "")
	}

	// .
	// .
	// .
	// .
	if method != methodInvokeCall {
		return errorResponse(idRaw, codeMethod, msgMethodNotFound, reasonMethodNotFound)
	}

	// .
	// .
	// .
	paramsRaw := memberByKey(members, "params")
	if paramsRaw == nil {
		paramsRaw = []byte("{}")
	}
	params, isObj := objectMembers(paramsRaw)
	if !isObj {
		return errorResponse(idRaw, codeParams, msgParamsNotObject, "")
	}
	operation, ok := decodeJSONString(memberByKey(params, "operation"))
	if !ok || operation == "" {
		// .
		return errorResponse(idRaw, codeParams, msgOperationRequired, "")
	}

	// .
	// .
	// .
	// .
	// .
	targetRaw := memberByKey(params, "target")
	if targetRaw != nil && targetRaw[0] != '{' {
		return resultResponse(idRaw, failureResult("denied", reasonTargetInvalid, "target must be an object"))
	}
	argsRaw := memberByKey(params, "arguments")
	if argsRaw == nil {
		argsRaw = []byte("{}")
	} else if argsRaw[0] != '{' {
		return resultResponse(idRaw, failureResult("denied", reasonArgInvalid, "arguments must be an object"))
	}

	// .
	fn, found := p.handlers[operation]
	if !found && p.dynamic != nil {
		dyn := p.dynamic
		fn, found = func(c Call) (any, error) { return dyn(operation, c) }, true
	}
	if !found {
		return resultResponse(idRaw, failureResult("failed", reasonOperationNotFound,
			"operation "+operation+" is not implemented by this plugin"))
	}

	// .
	// .
	// .
	call := Call{
		Operation: operation,
		Target:    cloneBytes(targetRaw),
		Arguments: cloneBytes(argsRaw),
	}
	result, err := fn(call)
	var resp []byte
	switch e := classifyHandlerError(err); {
	case e.opErr != nil:
		resp = resultResponse(idRaw, failureResult(e.opErr.Status, e.opErr.ReasonCode, e.opErr.Reason))
	case e.internal:
		// .
		// .
		// .
		// .
		// .
		resp = errorResponse(idRaw, codeInternal, msgHandlerFailed, reasonHandlerFailed)
	default:
		// .
		// .
		payload, merr := marshalValue(result)
		if merr != nil {
			resp = errorResponse(idRaw, codeInternal, msgHandlerFailed, reasonHandlerFailed)
		} else {
			resp = resultResponse(idRaw, successResult(payload))
		}
	}

	// .
	// .
	// .
	// .
	// .
	// .
	// .
	if len(resp) > MaxControlFrameBytes || ValidateStrict(resp) != nil {
		return errorResponse(idRaw, codeInternal, msgHandlerFailed, reasonHandlerFailed)
	}
	return resp
}

// .
type handlerOutcome struct {
	opErr    *OperationError
	internal bool
}

func classifyHandlerError(err error) handlerOutcome {
	if err == nil {
		return handlerOutcome{}
	}
	// .
	// .
	// .
	// .
	for e := err; e != nil; e = unwrapOnce(e) {
		if oe, ok := e.(*OperationError); ok {
			// .
			// .
			// .
			// .
			if oe.Status != "failed" && oe.Status != "denied" {
				oe = &OperationError{Status: "failed", ReasonCode: oe.ReasonCode, Reason: oe.Reason}
			}
			return handlerOutcome{opErr: oe}
		}
		if d, ok := e.(*Denied); ok {
			// .
			// .
			// .
			// .
			return handlerOutcome{opErr: &OperationError{
				Status:     "denied",
				ReasonCode: d.ReasonCode,
				Reason:     d.Message,
			}}
		}
	}
	return handlerOutcome{internal: true}
}

// .
// .
func unwrapOnce(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// .
// .
func dispatchEvent(topic string, payload []byte) {
	if current == nil || current.onEvent == nil {
		return
	}
	current.onEvent(topic, payload)
}

// .
// .
// .
// .
// .

var nullID = []byte("null")

func errorResponse(idRaw []byte, code int, message, reasonCode string) []byte {
	out := make([]byte, 0, 96+len(idRaw)+len(message)+len(reasonCode))
	out = append(out, `{"jsonrpc":"2.0","id":`...)
	out = append(out, idRaw...)
	out = append(out, `,"error":{"code":`...)
	out = appendInt(out, code)
	out = append(out, `,"message":`...)
	out = appendJSONString(out, message)
	if reasonCode != "" {
		// .
		out = append(out, `,"data":{"reasonCode":`...)
		out = appendJSONString(out, reasonCode)
		out = append(out, '}')
	}
	out = append(out, '}', '}')
	return out
}

func resultResponse(idRaw, result []byte) []byte {
	out := make([]byte, 0, 32+len(idRaw)+len(result))
	out = append(out, `{"jsonrpc":"2.0","id":`...)
	out = append(out, idRaw...)
	out = append(out, `,"result":`...)
	out = append(out, result...)
	out = append(out, '}')
	return out
}

// .
// .
// .
// .
// .
func successResult(operationResult []byte) []byte {
	out := make([]byte, 0, 48+len(operationResult))
	out = append(out, `{"status":"succeeded","operation_result":`...)
	out = append(out, operationResult...)
	out = append(out, '}')
	return out
}

// .
// .
// .
// .
// .
func failureResult(status, reasonCode, reason string) []byte {
	if reason == "" {
		reason = reasonCode
	}
	out := make([]byte, 0, 96+len(reason)+2*len(reasonCode))
	out = append(out, `{"status":`...)
	out = appendJSONString(out, status)
	out = append(out, `,"reason":`...)
	out = appendJSONString(out, reason)
	if reasonCode != "" {
		out = append(out, `,"reasonCode":`...)
		out = appendJSONString(out, reasonCode)
		out = append(out, `,"reason_code":`...)
		out = appendJSONString(out, reasonCode)
	}
	out = append(out, '}')
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
func appendJSONString(out []byte, s string) []byte {
	const hexDigits = "0123456789abcdef"
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			out = append(out, '\\', '"')
		case c == '\\':
			out = append(out, '\\', '\\')
		case c < 0x20:
			switch c {
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			default:
				out = append(out, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			}
		default:
			out = append(out, c)
		}
	}
	return append(out, '"')
}

func appendInt(out []byte, v int) []byte {
	return append(out, strconv.Itoa(v)...)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

// .
// .
// .
func bestEffortID(frame []byte) []byte {
	if ValidateStrict(frame) != nil {
		return nullID
	}
	members, ok := objectMembers(frame)
	if !ok {
		return nullID
	}
	idRaw := memberByKey(members, "id")
	if idRaw == nil {
		return nullID
	}
	if idRaw[0] == '"' || idRaw[0] == '-' || (idRaw[0] >= '0' && idRaw[0] <= '9') || string(idRaw) == "null" {
		return cloneBytes(idRaw)
	}
	return nullID
}
