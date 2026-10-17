package aiiospkg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionDeclarationsAreHeldToTheRules(t *testing.T) {
	good := []SubscriptionDecl{{Topic: TopicToolCalled, Operation: "log.event", Filter: map[string]string{"tool": "read"}}, {Topic: TopicTurnEnded, Operation: "log.flush"}}
	if err := ValidateSubscriptions(good); err != nil {
		t.Fatal(err)
	}
	out, err := SubscriptionsJSON(good)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `[{"filter":{"tool":"read"},"operation":"log.event","topic":"tool.called"},{"operation":"log.flush","topic":"turn.ended"}]` {
		t.Fatalf("canonical member: %s", out)
	}
	if err := CheckSubscriptionOperations(good, []string{"log.event"}); err == nil || !strings.Contains(err.Error(), "not one this plugin describes") {
		t.Fatalf("an undescribed operation: %v", err)
	}
	for name, tc := range map[string]struct {
		decl []SubscriptionDecl
		want string
	}{
		"undeclared topic": {[]SubscriptionDecl{{Topic: "disk.full", Operation: "x"}}, "not one the host emits"},
		"no operation":     {[]SubscriptionDecl{{Topic: TopicToolCalled}}, "operation is required"},
		"bad filter":       {[]SubscriptionDecl{{Topic: TopicToolCalled, Operation: "x", Filter: map[string]string{"Tool Name": "y"}}}, "filter"},
	} {
		if err := ValidateSubscriptions(tc.decl); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v (want %q)", name, err, tc.want)
		}
	}
}

func TestWorkTopicsAreOnesTheHostEmits(t *testing.T) {
	for _, topic := range []string{TopicWorkStarted, TopicWorkDelivered, TopicWorkHarvested, TopicSubagentSpawned} {
		found := false
		for _, known := range Topics {
			if known == topic {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not in Topics", topic)
		}
	}
	if len(Topics) != 9 {
		t.Fatalf("the closed set has %d topics; the host emits nine", len(Topics))
	}
}

func TestCalibrationDeliverySurvivesPackaging(t *testing.T) {
	d := []SubscriptionDecl{{Topic: TopicToolCalled, Operation: "observe", Delivery: "calibration_v1"}}
	raw, err := SubscriptionsJSON(d)
	if err != nil || !strings.Contains(string(raw), `"delivery":"calibration_v1"`) {
		t.Fatal(string(raw), err)
	}
	for _, delivery := range []string{"unknown", "calibration_v2"} {
		d[0].Delivery = delivery
		if err := ValidateSubscriptions(d); err == nil {
			t.Fatal(delivery)
		}
	}
	d[0].Delivery = "calibration_v1"
	d = append(d, SubscriptionDecl{Topic: TopicTurnEnded, Operation: "observe"})
	if err := ValidateSubscriptions(d); err == nil {
		t.Fatal("shared target accepted")
	}
}

func TestCalibrationProfileCannotSilentlyDowngrade(t *testing.T) {
	for _, value := range []string{`null`, `""`, `"other"`} {
		var d SubscriptionDecl
		if err := json.Unmarshal([]byte(`{"topic":"tool.called","operation":"observe","delivery":`+value+`}`), &d); err == nil {
			t.Fatal(value)
		}
	}
}
