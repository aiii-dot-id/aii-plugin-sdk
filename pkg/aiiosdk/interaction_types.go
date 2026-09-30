package aiiosdk

import "encoding/json"

type InteractionKind string

const (
	InteractionLegacy     InteractionKind = "legacy"
	InteractionMessage    InteractionKind = "message"
	InteractionOutbound   InteractionKind = "outbound_message"
	InteractionToolCall   InteractionKind = "tool_call"
	InteractionToolResult InteractionKind = "tool_result"
	InteractionNotice     InteractionKind = "notice"
	InteractionAnnotation InteractionKind = "annotation"
)

type InteractionRole string

const (
	InteractionOperator    InteractionRole = "operator"
	InteractionParticipant InteractionRole = "participant"
	InteractionResident    InteractionRole = "resident"
	InteractionSystem      InteractionRole = "system"
)

type InteractionOutcome string

const (
	InteractionSucceeded InteractionOutcome = "succeeded"
	InteractionFailed    InteractionOutcome = "failed"
	InteractionUnknown   InteractionOutcome = "unknown"
	InteractionCancelled InteractionOutcome = "cancelled"
	InteractionRefused   InteractionOutcome = "refused"
)

type InteractionSource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type InteractionWorkChange struct {
	Session          string  `json:"session"`
	State            *string `json:"state,omitempty"`
	Focus            *string `json:"focus,omitempty"`
	NextMove         *string `json:"next_move,omitempty"`
	Plan             *string `json:"plan,omitempty"`
	ExpectedEvidence *string `json:"expected_evidence,omitempty"`
	Falsifier        *string `json:"falsifier,omitempty"`
	DecisionNeeded   *string `json:"decision_needed,omitempty"`
	Result           *string `json:"result,omitempty"`
	Evidence         *string `json:"evidence,omitempty"`
	EvidenceReadback *string `json:"evidence_readback,omitempty"`
	Steps            *int    `json:"steps,omitempty"`
	Independent      *int    `json:"independent,omitempty"`
}
type InteractionAnnotationData struct {
	Kind    string          `json:"kind"`
	Key     string          `json:"key"`
	Payload json.RawMessage `json:"payload"`
}
type InteractionGrade struct {
	Session string `json:"session"`
	Grade   string `json:"grade"`
	Item    string `json:"item,omitempty"`
}
type InteractionDetails struct {
	Project        *InteractionProjectChange  `json:"project,omitempty"`
	Channel        string                     `json:"channel,omitempty"`
	Actor          string                     `json:"actor,omitempty"`
	Model          string                     `json:"model,omitempty"`
	Tool           string                     `json:"tool,omitempty"`
	ProviderCallID string                     `json:"provider_call_id,omitempty"`
	Ordinal        int                        `json:"emission_ordinal,omitempty"`
	WorkSession    string                     `json:"work_session_id,omitempty"`
	SessionReason  string                     `json:"session_reason,omitempty"`
	DurationMS     *int64                     `json:"duration_ms,omitempty"`
	Truncated      bool                       `json:"truncated,omitempty"`
	Reason         string                     `json:"reason,omitempty"`
	RecordingError string                     `json:"recording_error,omitempty"`
	ReasonCode     string                     `json:"reason_code,omitempty"`
	ArgsRecord     string                     `json:"args_record,omitempty"`
	ResultRecord   string                     `json:"result_record,omitempty"`
	LegacySource   string                     `json:"legacy_source,omitempty"`
	OrderBasis     string                     `json:"order_basis,omitempty"`
	Origin         string                     `json:"origin,omitempty"`
	Work           *InteractionWorkChange     `json:"work,omitempty"`
	Annotation     *InteractionAnnotationData `json:"annotation,omitempty"`
	Grade          *InteractionGrade          `json:"grade,omitempty"`
}

type InteractionRecord struct {
	ID         string             `json:"id"`
	Sequence   uint64             `json:"sequence,string"`
	SessionID  string             `json:"session_id"`
	ProjectID  string             `json:"project_id"`
	TurnID     string             `json:"turn_id,omitempty"`
	Kind       InteractionKind    `json:"kind"`
	Role       InteractionRole    `json:"role"`
	Content    string             `json:"content"`
	CreatedAt  string             `json:"created_at"`
	RecordedAt string             `json:"recorded_at,omitempty"`
	OccurredAt string             `json:"occurred_at,omitempty"`
	RelatedID  string             `json:"related_id,omitempty"`
	Source     *InteractionSource `json:"source,omitempty"`
	Outcome    InteractionOutcome `json:"outcome,omitempty"`
	Details    InteractionDetails `json:"details"`

	Deliveries  []InteractionDelivery      `json:"deliveries,omitempty"`
	Annotations map[string]json.RawMessage `json:"annotations,omitempty"`
	ContentRef  *InteractionContentRef     `json:"content_ref,omitempty"`
}
type InteractionContentRef struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Incarnation string `json:"incarnation"`
	SHA256      string `json:"sha256"`
	Bytes       int    `json:"bytes"`
}

type InteractionFilter struct {
	Kinds       []InteractionKind `json:"kinds,omitempty"`
	Role        InteractionRole   `json:"role,omitempty"`
	TurnID      string            `json:"turn_id,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	WorkSession string            `json:"work_session_id,omitempty"`
	ProjectID   string            `json:"project_id,omitempty"`
	RelatedID   string            `json:"related_id,omitempty"`
	From        string            `json:"from,omitempty"`
	Until       string            `json:"until,omitempty"`
	Undated     bool              `json:"undated,omitempty"`
}
type InteractionQuery struct {
	Version int               `json:"version"`
	Source  string            `json:"source,omitempty"`
	Filter  InteractionFilter `json:"filter"`
	Cursor  string            `json:"cursor,omitempty"`
	Limit   int               `json:"limit,omitempty"`
}
type InteractionPage struct {
	Version      int                 `json:"version"`
	Identity     string              `json:"identity"`
	Incarnation  string              `json:"incarnation"`
	Source       string              `json:"source"`
	Filter       InteractionFilter   `json:"filter"`
	Cursor       string              `json:"cursor,omitempty"`
	Rows         []InteractionRecord `json:"rows"`
	NextCursor   string              `json:"next_cursor,omitempty"`
	Fingerprint  string              `json:"fingerprint"`
	Lost         uint64              `json:"lost,omitempty"`
	Availability string              `json:"availability"`
}
type InteractionReadRequest struct {
	Version     int    `json:"version"`
	Source      string `json:"source,omitempty"`
	ID          string `json:"id"`
	Incarnation string `json:"incarnation"`
	SHA256      string `json:"sha256"`
	Offset      int64  `json:"offset"`
	Length      int    `json:"length,omitempty"`
}
type InteractionReadResult struct {
	Version     int    `json:"version"`
	Source      string `json:"source"`
	ID          string `json:"id"`
	Incarnation string `json:"incarnation"`
	SHA256      string `json:"sha256"`
	Data        string `json:"data_b64"`
	Bytes       int    `json:"bytes"`
	Offset      int64  `json:"offset"`
	Size        int64  `json:"size"`
	EOF         bool   `json:"eof"`
}

type InteractionDelivery struct {
	ID        string `json:"id"`
	Delivered bool   `json:"delivered"`
	Via       string `json:"via,omitempty"`
	At        string `json:"at,omitempty"`
	Attempts  int    `json:"attempts"`
	Parked    bool   `json:"parked"`
	Effect    string `json:"effect,omitempty"`
	Error     string `json:"error,omitempty"`
}

type InteractionProjectContract struct {
	Outcome     string   `json:"outcome,omitempty"`
	Acceptance  []string `json:"acceptance,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}
type InteractionProjectChange struct {
	Before *InteractionProjectContract `json:"before,omitempty"`
	After  InteractionProjectContract  `json:"after"`
}
