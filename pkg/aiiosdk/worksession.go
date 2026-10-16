package aiiosdk

type WorkSession struct {
	ID     string
	Reason string
	CallID string
}

func (c Call) WorkSession() WorkSession {
	o := Object(c.HostWorkSession)
	var s WorkSession
	s.ID, _ = o.String("id")
	s.Reason, _ = o.String("reason")
	s.CallID, _ = o.String("call_id")
	return s
}
