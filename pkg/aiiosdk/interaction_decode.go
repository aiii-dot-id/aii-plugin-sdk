package aiiosdk

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func decodeInteraction(raw json.RawMessage, out any) error {
	if err := ValidateStrict(raw); err != nil {
		return err
	}
	return decodeInteractionValue(raw, out)
}
func decodeInteractionValue(raw []byte, out any) error {
	bad := func() error { return fmt.Errorf("aiiosdk: malformed interaction field") }
	var fields map[string]any
	switch v := out.(type) {
	case *string:
		x, ok := decodeJSONString(raw)
		if !ok {
			return bad()
		}
		*v = x
		return nil
	case *bool:
		switch string(raw) {
		case "true":
			*v = true
		case "false":
			*v = false
		default:
			return bad()
		}
		return nil
	case *int:
		n, e := strconv.ParseInt(string(raw), 10, 32)
		if e != nil {
			return bad()
		}
		*v = int(n)
		return nil
	case *int64:
		n, e := strconv.ParseInt(string(raw), 10, 64)
		if e != nil {
			return bad()
		}
		*v = n
		return nil
	case *uint64:
		text := string(raw)
		if len(raw) > 0 && raw[0] == '"' {
			x, ok := decodeJSONString(raw)
			if !ok {
				return bad()
			}
			text = x
		}
		n, e := strconv.ParseUint(text, 10, 64)
		if e != nil {
			return bad()
		}
		*v = n
		return nil
	case *json.RawMessage:
		*v = append(json.RawMessage(nil), raw...)
		return nil
	case *map[string]json.RawMessage:
		members, ok := objectMembers(raw)
		if !ok {
			return bad()
		}
		*v = make(map[string]json.RawMessage, len(members))
		for _, m := range members {
			(*v)[m.key] = append(json.RawMessage(nil), m.raw...)
		}
		return nil
	case *InteractionKind:
		var x string
		if err := decodeInteractionValue(raw, &x); err != nil {
			return err
		}
		*v = InteractionKind(x)
		return nil
	case *InteractionRole:
		var x string
		if err := decodeInteractionValue(raw, &x); err != nil {
			return err
		}
		*v = InteractionRole(x)
		return nil
	case *InteractionOutcome:
		var x string
		if err := decodeInteractionValue(raw, &x); err != nil {
			return err
		}
		*v = InteractionOutcome(x)
		return nil
	case **InteractionAnnotationData:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionAnnotationData)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **InteractionContentRef:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionContentRef)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **InteractionGrade:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionGrade)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **InteractionSource:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionSource)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **InteractionWorkChange:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionWorkChange)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **int:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(int)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **int64:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(int64)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **string:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(string)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case *[]InteractionDelivery:
		elements, ok := arrayElements(raw)
		if !ok {
			return bad()
		}
		*v = make([]InteractionDelivery, len(elements))
		for i, e := range elements {
			if err := decodeInteractionValue(e, &(*v)[i]); err != nil {
				return err
			}
		}
		return nil
	case *[]InteractionKind:
		elements, ok := arrayElements(raw)
		if !ok {
			return bad()
		}
		*v = make([]InteractionKind, len(elements))
		for i, e := range elements {
			if err := decodeInteractionValue(e, &(*v)[i]); err != nil {
				return err
			}
		}
		return nil
	case *[]InteractionRecord:
		elements, ok := arrayElements(raw)
		if !ok {
			return bad()
		}
		*v = make([]InteractionRecord, len(elements))
		for i, e := range elements {
			if err := decodeInteractionValue(e, &(*v)[i]); err != nil {
				return err
			}
		}
		return nil
	case *InteractionProjectChange:
		fields = map[string]any{"before": &v.Before, "after": &v.After}
	case *InteractionProjectContract:
		fields = map[string]any{"outcome": &v.Outcome, "acceptance": &v.Acceptance, "constraints": &v.Constraints}
	case **InteractionProjectChange:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionProjectChange)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case **InteractionProjectContract:
		if string(raw) == "null" {
			*v = nil
			return nil
		}
		x := new(InteractionProjectContract)
		if err := decodeInteractionValue(raw, x); err != nil {
			return err
		}
		*v = x
		return nil
	case *[]string:
		elements, ok := arrayElements(raw)
		if !ok {
			return bad()
		}
		*v = make([]string, len(elements))
		for i, e := range elements {
			if err := decodeInteractionValue(e, &(*v)[i]); err != nil {
				return err
			}
		}
		return nil
	case *InteractionSource:
		fields = map[string]any{"kind": &v.Kind, "id": &v.ID}
	case *InteractionWorkChange:
		fields = map[string]any{"session": &v.Session, "state": &v.State, "focus": &v.Focus, "next_move": &v.NextMove, "plan": &v.Plan, "expected_evidence": &v.ExpectedEvidence, "falsifier": &v.Falsifier, "decision_needed": &v.DecisionNeeded, "result": &v.Result, "evidence": &v.Evidence, "evidence_readback": &v.EvidenceReadback, "steps": &v.Steps, "independent": &v.Independent}
	case *InteractionAnnotationData:
		fields = map[string]any{"kind": &v.Kind, "key": &v.Key, "payload": &v.Payload}
	case *InteractionGrade:
		fields = map[string]any{"session": &v.Session, "grade": &v.Grade, "item": &v.Item}
	case *InteractionDetails:
		fields = map[string]any{"project": &v.Project, "channel": &v.Channel, "actor": &v.Actor, "model": &v.Model, "tool": &v.Tool, "provider_call_id": &v.ProviderCallID, "emission_ordinal": &v.Ordinal, "work_session_id": &v.WorkSession, "session_reason": &v.SessionReason, "duration_ms": &v.DurationMS, "truncated": &v.Truncated, "reason": &v.Reason, "recording_error": &v.RecordingError, "reason_code": &v.ReasonCode, "args_record": &v.ArgsRecord, "result_record": &v.ResultRecord, "legacy_source": &v.LegacySource, "order_basis": &v.OrderBasis, "origin": &v.Origin, "work": &v.Work, "annotation": &v.Annotation, "grade": &v.Grade}
	case *InteractionRecord:
		fields = map[string]any{"id": &v.ID, "sequence": &v.Sequence, "session_id": &v.SessionID, "project_id": &v.ProjectID, "turn_id": &v.TurnID, "kind": &v.Kind, "role": &v.Role, "content": &v.Content, "created_at": &v.CreatedAt, "recorded_at": &v.RecordedAt, "occurred_at": &v.OccurredAt, "related_id": &v.RelatedID, "source": &v.Source, "outcome": &v.Outcome, "details": &v.Details, "deliveries": &v.Deliveries, "annotations": &v.Annotations, "content_ref": &v.ContentRef}
	case *InteractionContentRef:
		fields = map[string]any{"id": &v.ID, "source": &v.Source, "incarnation": &v.Incarnation, "sha256": &v.SHA256, "bytes": &v.Bytes}
	case *InteractionFilter:
		fields = map[string]any{"kinds": &v.Kinds, "role": &v.Role, "turn_id": &v.TurnID, "session_id": &v.SessionID, "work_session_id": &v.WorkSession, "project_id": &v.ProjectID, "related_id": &v.RelatedID, "from": &v.From, "until": &v.Until, "undated": &v.Undated}
	case *InteractionQuery:
		fields = map[string]any{"version": &v.Version, "source": &v.Source, "filter": &v.Filter, "cursor": &v.Cursor, "limit": &v.Limit}
	case *InteractionPage:
		fields = map[string]any{"version": &v.Version, "identity": &v.Identity, "incarnation": &v.Incarnation, "source": &v.Source, "filter": &v.Filter, "cursor": &v.Cursor, "rows": &v.Rows, "next_cursor": &v.NextCursor, "fingerprint": &v.Fingerprint, "lost": &v.Lost, "availability": &v.Availability}
	case *InteractionReadRequest:
		fields = map[string]any{"version": &v.Version, "source": &v.Source, "id": &v.ID, "incarnation": &v.Incarnation, "sha256": &v.SHA256, "offset": &v.Offset, "length": &v.Length}
	case *InteractionReadResult:
		fields = map[string]any{"version": &v.Version, "source": &v.Source, "id": &v.ID, "incarnation": &v.Incarnation, "sha256": &v.SHA256, "data_b64": &v.Data, "bytes": &v.Bytes, "offset": &v.Offset, "size": &v.Size, "eof": &v.EOF}
	case *InteractionDelivery:
		fields = map[string]any{"id": &v.ID, "delivered": &v.Delivered, "via": &v.Via, "at": &v.At, "attempts": &v.Attempts, "parked": &v.Parked, "effect": &v.Effect, "error": &v.Error}
	default:
		return bad()
	}
	members, ok := objectMembers(raw)
	if !ok {
		return bad()
	}
	for _, m := range members {
		target, known := fields[m.key]
		if !known {
			return fmt.Errorf("aiiosdk: unknown interaction field %q", m.key)
		}
		if err := decodeInteractionValue(m.raw, target); err != nil {
			return fmt.Errorf("aiiosdk: interaction %s: %w", m.key, err)
		}
	}
	return nil
}
