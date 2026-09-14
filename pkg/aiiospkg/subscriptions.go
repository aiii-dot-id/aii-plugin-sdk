package aiiospkg

// .
// .
// .
// .
// .
// .

import "fmt"

// .
const SubscriptionsFile = "subscriptions.json"

// .
const MaxSubscriptions = 16

// .
const (
	TopicToolCalled     = "tool.called"
	TopicTurnStarted    = "turn.started"
	TopicTurnEnded      = "turn.ended"
	TopicLedgerAppended = "ledger.appended"
	TopicAlarmFired     = "alarm.fired"
	// .
	// .
	// .
	// .
	// .
	TopicWorkStarted     = "work.started"
	TopicWorkDelivered   = "work.delivered"
	TopicWorkHarvested   = "work.harvested"
	TopicSubagentSpawned = "subagent.spawned"
)

// .
var Topics = []string{TopicToolCalled, TopicTurnStarted, TopicTurnEnded, TopicLedgerAppended, TopicAlarmFired,
	TopicWorkStarted, TopicWorkDelivered, TopicWorkHarvested, TopicSubagentSpawned}

// .
type SubscriptionDecl struct {
	Topic     string            `json:"topic"`
	Operation string            `json:"operation"`
	Filter    map[string]string `json:"filter,omitempty"`
}

// .
func ValidateSubscriptions(decls []SubscriptionDecl) error {
	if len(decls) > MaxSubscriptions {
		return fmt.Errorf("%d subscriptions; at most %d", len(decls), MaxSubscriptions)
	}
	for i, d := range decls {
		ok := false
		for _, t := range Topics {
			if d.Topic == t {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("subscription %d: topic %q is not one the host emits (%v)", i, d.Topic, Topics)
		}
		if d.Operation == "" {
			return fmt.Errorf("subscription %d (%s): operation is required", i, d.Topic)
		}
		if len(d.Filter) > 8 {
			return fmt.Errorf("subscription %d (%s): at most 8 filter fields", i, d.Topic)
		}
		for k, v := range d.Filter {
			if !reSettingKey.MatchString(k) || len(v) > 256 {
				return fmt.Errorf("subscription %d (%s): filter %q is not a short payload field", i, d.Topic, k)
			}
		}
	}
	return nil
}

// .
// .
func CheckSubscriptionOperations(decls []SubscriptionDecl, operations []string) error {
	known := map[string]bool{}
	for _, op := range operations {
		known[op] = true
	}
	for _, d := range decls {
		if !known[d.Operation] {
			return fmt.Errorf("subscription %s: operation %q is not one this plugin describes", d.Topic, d.Operation)
		}
	}
	return nil
}

// .
func SubscriptionsJSON(decls []SubscriptionDecl) ([]byte, error) {
	if err := ValidateSubscriptions(decls); err != nil {
		return nil, err
	}
	list := make([]interface{}, 0, len(decls))
	for _, d := range decls {
		entry := map[string]interface{}{"topic": d.Topic, "operation": d.Operation}
		if len(d.Filter) > 0 {
			f := map[string]interface{}{}
			for k, v := range d.Filter {
				f[k] = v
			}
			entry["filter"] = f
		}
		list = append(list, entry)
	}
	return marshalCanonical(list)
}
