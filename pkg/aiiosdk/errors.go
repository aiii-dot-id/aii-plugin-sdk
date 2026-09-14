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

import (
	"errors"
	"fmt"
)

// .
// .
// .
// .
var ErrNotInGuest = errors.New("aiiosdk: host calls exist only inside a wasm guest build")

// .
// .
// .
// .
// .
type Denied struct {
	Code       int
	Message    string
	ReasonCode string
	DeniedAt   string
}

func (d *Denied) Error() string {
	if d.ReasonCode != "" {
		return fmt.Sprintf("aiiosdk: host denied the call: %s (%d, reasonCode %s)", d.Message, d.Code, d.ReasonCode)
	}
	return fmt.Sprintf("aiiosdk: host error %d: %s", d.Code, d.Message)
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
type OperationError struct {
	Status     string
	ReasonCode string
	Reason     string
}

func (e *OperationError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("aiiosdk: operation %s: %s (%s)", e.Status, e.Reason, e.ReasonCode)
	}
	return fmt.Sprintf("aiiosdk: operation %s: %s", e.Status, e.ReasonCode)
}

// .
// .
// .
// .
// .
// .
func Fail(reasonCode, reason string) *OperationError {
	return &OperationError{Status: "failed", ReasonCode: reasonCode, Reason: reason}
}

// .
// .
// .
// .
func Deny(reasonCode, reason string) *OperationError {
	return &OperationError{Status: "denied", ReasonCode: reasonCode, Reason: reason}
}
