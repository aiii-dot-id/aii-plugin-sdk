package aiiosdk

type ChannelDescription struct {
	Channel       string
	Receive       string
	BudgetSeconds int

	Acknowledges bool
}

func (d ChannelDescription) Value() map[string]any {
	out := map[string]any{"channel": d.Channel}
	if d.Receive != "" {
		out["receive"] = d.Receive
	}
	if d.BudgetSeconds > 0 {
		out["budget_seconds"] = d.BudgetSeconds
	}
	if d.Acknowledges {
		out["acknowledges"] = true
	}
	return out
}
