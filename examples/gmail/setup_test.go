package main

import (
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// gmail.setup against a fake Pub/Sub that keeps what its REST reference
// documents: a create of what exists answers 409, getIamPolicy answers an etag
// and setIamPolicy refuses a stale one, a subscription is read back whole, and a
// patch names the fields it changes. Nothing here has met Google.

const (
	pushAccount = "aii-gmail-push@aii-mail.iam.gserviceaccount.com"
	notifyURL   = "https://identity.example.invalid/hooks/com.aiii.examples.gmail/notify"
	subName     = "projects/aii-mail/subscriptions/gmail-push"
	gmailPush   = "serviceAccount:gmail-api-push@system.gserviceaccount.com"
)

var (
	psDisabled = gave(403, `{"error":{"code":403,"message":"Cloud Pub/Sub API has not been used in project 1234 before or it is disabled.","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED","domain":"googleapis.com"}]}}`, -1)
	psDenied   = gave(403, gerr(403, "forbidden", "User not authorized to perform this action."), -1)
)

type psStep func(p *pubsub, c httpCall) brokerReply

type pubsub struct {
	t        *testing.T
	topics   map[string]bool
	policies map[string]map[string]any // by topic
	subs     map[string]map[string]any // by subscription name
	etags    int
	calls    []httpCall
	at       map[int]psStep
}

func newPubsub(t *testing.T) *pubsub {
	return &pubsub{t: t, topics: map[string]bool{}, policies: map[string]map[string]any{}, subs: map[string]map[string]any{}, at: map[int]psStep{}}
}

func (p *pubsub) ops() []string {
	var out []string
	for _, c := range p.calls {
		out = append(out, strings.TrimPrefix(c.op, "http.")+" "+strings.TrimPrefix(c.path(), "/v1/"))
	}
	return out
}

func (p *pubsub) handle(c httpCall) brokerReply {
	p.calls = append(p.calls, c)
	if f := p.at[len(p.calls)]; f != nil {
		return f(p, c)
	}
	return p.serve(c)
}

func lostAfter(p *pubsub, c httpCall) brokerReply { p.serve(c); return lostAnswer }

var exists = gave(409, gerr(409, "alreadyExists", "Resource already exists in the project."), -1)

func (p *pubsub) serve(c httpCall) brokerReply {
	u, _ := url.Parse(c.url)
	name, ok := strings.CutPrefix(u.EscapedPath(), "/v1/")
	if !ok || u.Scheme != "https" || u.Host != "pubsub.googleapis.com" {
		p.t.Errorf("a call outside Pub/Sub's API: %s", c.url)
		return notFound
	}
	body := c.body()
	switch {
	case c.op == "http.put" && strings.Contains(name, "/topics/"):
		if p.topics[name] {
			return exists
		}
		p.topics[name] = true
		return gave(200, marshal(map[string]any{"name": name}), -1)
	case c.op == "http.get" && strings.HasSuffix(name, ":getIamPolicy"):
		topic := strings.TrimSuffix(name, ":getIamPolicy")
		if !p.topics[topic] {
			return notFound
		}
		if p.policies[topic] == nil {
			p.policies[topic] = map[string]any{"version": 1, "etag": "ACAB"}
		}
		return gave(200, marshal(p.policies[topic]), -1)
	case c.op == "http.post" && strings.HasSuffix(name, ":setIamPolicy"):
		topic := strings.TrimSuffix(name, ":setIamPolicy")
		policy, _ := body["policy"].(map[string]any)
		if p.policies[topic] == nil || policy == nil || policy["etag"] != p.policies[topic]["etag"] {
			return gave(409, gerr(409, "aborted", "There were concurrent policy changes."), -1)
		}
		p.etags++
		policy["etag"] = "E" + string(rune('0'+p.etags))
		p.policies[topic] = policy
		return gave(200, marshal(policy), -1)
	case c.op == "http.put" && strings.Contains(name, "/subscriptions/"):
		if !p.topics[str2(body["topic"])] {
			return notFound
		}
		if p.subs[name] != nil {
			return exists
		}
		body["name"] = name
		p.subs[name] = body
		return gave(200, marshal(body), -1)
	case c.op == "http.get" && strings.Contains(name, "/subscriptions/"):
		if p.subs[name] == nil {
			return notFound
		}
		return gave(200, marshal(p.subs[name]), -1)
	case c.op == "http.patch" && strings.Contains(name, "/subscriptions/"):
		sub, _ := body["subscription"].(map[string]any)
		if p.subs[name] == nil || body["updateMask"] != "pushConfig" || sub == nil {
			return notFound
		}
		p.subs[name]["pushConfig"] = sub["pushConfig"]
		return gave(200, marshal(p.subs[name]), -1)
	}
	p.t.Errorf("an exchange the Pub/Sub API has not been asked for here: %s %s", c.op, c.url)
	return gave(400, gerr(400, "badRequest", "Bad Request"), -1)
}

func str2(v any) string { s, _ := v.(string); return s }

// mailbox is Gmail and Pub/Sub behind the one host the plugin calls.
func mailbox(g *gmail, p *pubsub) func(httpCall) brokerReply {
	return func(c httpCall) brokerReply {
		if u, err := url.Parse(c.url); err == nil && u.Host == "pubsub.googleapis.com" {
			return p.handle(c)
		}
		return g.handle(c)
	}
}

func setupRig(t *testing.T) (*gmail, *pubsub, *standIn, *proc) {
	t.Helper()
	g, p := newGmail(t), newPubsub(t)
	g.historyID = "500"
	h := &standIn{t: t, handle: handle, gmail: mailbox(g, p), topic: topic, extra: map[string]any{"push_account": pushAccount}, kv: map[string]string{}}
	return g, p, h, start(t)
}

// setupArgs is the operator's confirmed dispatch, with the host's word on
// where this identity's webhooks are.
func setupArgs() map[string]any {
	return act(map[string]any{"_host_webhooks": map[string]any{"notify": notifyURL}})
}

func wantOps(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n  got  %v\n  want %v", what, got, want)
	}
}

func TestSetupMakesTheTopicTheGrantTheSubscriptionAndTheWatch(t *testing.T) {
	g, p, h, proc := setupRig(t)
	got := proc.invoke(h, "gmail.setup", setupArgs())
	if got.status != "succeeded" {
		t.Fatalf("setup: %s", got.line())
	}
	for k, want := range map[string]string{"endpoint": notifyURL, "topic": "created", "publisher": "granted", "subscription": "created", "subscription_name": subName, "watch_expires": "1791000000000"} {
		if got.text(k) != want {
			t.Errorf("%s is %q, want %q", k, got.text(k), want)
		}
	}
	wantOps(t, "the Pub/Sub calls, in order", p.ops(),
		"put projects/aii-mail/topics/gmail",
		"get projects/aii-mail/topics/gmail:getIamPolicy",
		"post projects/aii-mail/topics/gmail:setIamPolicy",
		"put "+strings.TrimPrefix(subName, "/"))
	if len(g.watches) != 1 || g.watches[0]["topicName"] != topic {
		t.Errorf("the watch asks for %v", g.watches)
	}
	policy := p.policies[topic]
	if !reflect.DeepEqual(policy["bindings"], []any{map[string]any{"role": "roles/pubsub.publisher", "members": []any{gmailPush}}}) {
		t.Errorf("the topic's policy is %v", policy)
	}
	want := map[string]any{"topic": topic, "ackDeadlineSeconds": float64(10), "name": subName, "pushConfig": map[string]any{
		"pushEndpoint": notifyURL, "oidcToken": map[string]any{"serviceAccountEmail": pushAccount, "audience": notifyURL}}}
	if !reflect.DeepEqual(p.subs[subName], want) {
		t.Errorf("the subscription is %v\n  want %v", p.subs[subName], want)
	}
	if h.kv[cursorKey] != "500" {
		t.Errorf("the cursor is %q, want the mailbox's place at the watch", h.kv[cursorKey])
	}
}

func TestSetupAgainChangesNothing(t *testing.T) {
	g, p, h, proc := setupRig(t)
	if got := proc.invoke(h, "gmail.setup", setupArgs()); got.status != "succeeded" {
		t.Fatalf("first: %s", got.line())
	}
	before := len(p.calls)
	got := proc.invoke(h, "gmail.setup", setupArgs())
	if got.status != "succeeded" {
		t.Fatalf("again: %s", got.line())
	}
	for k, want := range map[string]string{"topic": "already there", "publisher": "already granted", "subscription": "already set up"} {
		if got.text(k) != want {
			t.Errorf("%s is %q, want %q", k, got.text(k), want)
		}
	}
	wantOps(t, "the second run's calls", p.ops()[before:],
		"put projects/aii-mail/topics/gmail",
		"get projects/aii-mail/topics/gmail:getIamPolicy",
		"put projects/aii-mail/subscriptions/gmail-push",
		"get projects/aii-mail/subscriptions/gmail-push")
	if len(g.watches) != 2 {
		t.Errorf("the watch was asked for %d times, want each run", len(g.watches))
	}
}

// The topic's policy is the operator's: the grant is added to it, and nothing
// else in it is dropped — not a binding, not a field this plugin does not know,
// not the etag that guards the write.
func TestTheGrantIsAddedToThePolicyTheTopicHas(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bindings []any
		want     []any
	}{
		{"other roles", []any{map[string]any{"role": "roles/owner", "members": []any{"user:operator@example.com"}}},
			[]any{map[string]any{"role": "roles/owner", "members": []any{"user:operator@example.com"}}, map[string]any{"role": "roles/pubsub.publisher", "members": []any{gmailPush}}}},
		{"the role held by someone else", []any{map[string]any{"role": "roles/pubsub.publisher", "members": []any{"user:operator@example.com"}}},
			[]any{map[string]any{"role": "roles/pubsub.publisher", "members": []any{"user:operator@example.com", gmailPush}}}},
		{"the role held under a condition", []any{map[string]any{"role": "roles/pubsub.publisher", "members": []any{gmailPush}, "condition": map[string]any{"title": "weekdays"}}},
			[]any{map[string]any{"role": "roles/pubsub.publisher", "members": []any{gmailPush}, "condition": map[string]any{"title": "weekdays"}}, map[string]any{"role": "roles/pubsub.publisher", "members": []any{gmailPush}}}},
	} {
		_, p, h, proc := setupRig(t)
		p.topics[topic] = true
		p.policies[topic] = map[string]any{"version": 3, "etag": "BwX", "bindings": tc.bindings, "auditConfigs": []any{map[string]any{"service": "allServices"}}}
		got := proc.invoke(h, "gmail.setup", setupArgs())
		if got.status != "succeeded" || got.text("publisher") != "granted" || got.text("topic") != "already there" {
			t.Errorf("%s: %s", tc.name, got.line())
			continue
		}
		policy := p.policies[topic]
		if !reflect.DeepEqual(policy["bindings"], tc.want) || policy["version"] != float64(3) || policy["auditConfigs"] == nil {
			t.Errorf("%s: the policy is %v, want bindings %v with its version and audit configs", tc.name, policy, tc.want)
		}
		proc.kill()
	}
}

func TestASubscriptionAlreadyThereIsBroughtToTheShapeWanted(t *testing.T) {
	for _, tc := range []struct{ name, endpoint, account, audience string }{
		{"another endpoint", "https://old.example/hooks/x/notify", pushAccount, "https://old.example/hooks/x/notify"},
		{"another service account", notifyURL, "someone@other.iam.gserviceaccount.com", notifyURL},
		{"another audience", notifyURL, pushAccount, "https://elsewhere.example"},
		{"no token", notifyURL, "", ""},
	} {
		_, p, h, proc := setupRig(t)
		p.topics[topic] = true
		cfg := map[string]any{"pushEndpoint": tc.endpoint}
		if tc.account != "" {
			cfg["oidcToken"] = map[string]any{"serviceAccountEmail": tc.account, "audience": tc.audience}
		}
		p.subs[subName] = map[string]any{"name": subName, "topic": topic, "pushConfig": cfg, "ackDeadlineSeconds": float64(30)}
		got := proc.invoke(h, "gmail.setup", setupArgs())
		if got.status != "succeeded" || got.text("subscription") != "updated" {
			t.Errorf("%s: %s", tc.name, got.line())
			continue
		}
		wantCfg := map[string]any{"pushEndpoint": notifyURL, "oidcToken": map[string]any{"serviceAccountEmail": pushAccount, "audience": notifyURL}}
		if !reflect.DeepEqual(p.subs[subName]["pushConfig"], wantCfg) || p.subs[subName]["ackDeadlineSeconds"] != float64(30) {
			t.Errorf("%s: the subscription is %v", tc.name, p.subs[subName])
		}
		proc.kill()
	}
}

func TestASubscriptionOnAnotherTopicIsNotMoved(t *testing.T) {
	g, p, h, proc := setupRig(t)
	p.topics["projects/aii-mail/topics/other"] = true
	p.subs[subName] = map[string]any{"name": subName, "topic": "projects/aii-mail/topics/other", "pushConfig": map[string]any{"pushEndpoint": "https://x.example"}}
	got := proc.invoke(h, "gmail.setup", setupArgs())
	check(t, "another topic", got, "failed", remoteFailed, "exists on another topic (projects/aii-mail/topics/other), which this plugin does not move")
	if !strings.Contains(got.said(), "already done: the topic "+topic+" was created; Gmail was allowed to publish") {
		t.Errorf("the failure does not say what was done: %s", got.said())
	}
	if len(g.watches) != 0 {
		t.Error("the watch was started though the subscription was refused")
	}
	if p.subs[subName]["topic"] != "projects/aii-mail/topics/other" {
		t.Error("the other topic's subscription was changed")
	}
}

// What the operator did not set, or the identity cannot supply, stops the setup
// before anything is asked of Google — and says which.
func TestSetupWithoutWhatItNeedsAsksNothingOfGoogle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mod   func(h *standIn, args map[string]any)
		stamp bool
		code  string
		says  string
	}{
		{"no google account", func(h *standIn, _ map[string]any) { h.handle = "" }, true, argInvalid, "the google handle is not set"},
		{"no topic", func(h *standIn, _ map[string]any) { h.topic = "" }, true, argInvalid, "the topic setting is the Pub/Sub topic to create"},
		{"a topic that is not one", func(h *standIn, _ map[string]any) { h.topic = "projects/aii-mail/topics/a/b" }, true, argInvalid, "projects/<project>/topics/<topic>"},
		{"no push account", func(h *standIn, _ map[string]any) { delete(h.extra, "push_account") }, true, argInvalid, "the push_account setting is the service account"},
		{"an account that is no service account", func(h *standIn, _ map[string]any) { h.extra["push_account"] = "identity@example.invalid" }, true, argInvalid, "the push_account setting"},
		{"no public origin", func(_ *standIn, a map[string]any) { delete(a, "_host_webhooks") }, true, remoteFailed, "no public origin"},
		{"the operator did not confirm", func(*standIn, map[string]any) {}, false, "POLICY_DENY", "the operator has not confirmed this"},
	} {
		g, p, h, proc := setupRig(t)
		args := setupArgs()
		if !tc.stamp {
			delete(args, "_host_operator_act")
		}
		tc.mod(h, args)
		got := proc.invoke(h, "gmail.setup", args)
		if got.status == "succeeded" || got.code != tc.code || !strings.Contains(got.said(), tc.says) {
			t.Errorf("%s: %s", tc.name, got.line())
		}
		if tc.stamp && !strings.Contains(got.said(), "nothing was created or changed") {
			t.Errorf("%s: it does not say nothing was done: %s", tc.name, got.said())
		}
		if len(p.calls) != 0 || len(g.calls) != 0 {
			t.Errorf("%s: it called Google: %v %v", tc.name, p.ops(), g.ops())
		}
		proc.kill()
	}
}

// Every way a step can end other than well: the code the host reads, the step it
// names, what was already done, and that running it again is safe.
func TestASetupStepThatFailsSaysWhichWhatAndWhy(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   int // the Pub/Sub exchange that fails: 1 topic, 2 policy read, 3 policy write, 4 subscription
		step psStep
		code string
		says []string
	}{
		{"the consent lacks pubsub", 1, replyWith(noScope), remoteFailed, []string{"the topic:", "consent does not include pubsub", "nothing had been done", "safe to run again"}},
		{"the API is off", 1, replyWith(psDisabled), remoteFailed, []string{"the Pub/Sub API is not enabled in the Google project"}},
		{"no permission on the topic", 1, replyWith(psDenied), remoteFailed, []string{"Pub/Sub Admin", "Service Account User"}},
		{"rate limited", 1, replyWith(gave(429, gerr(429, "rateLimitExceeded", "slow"), 30)), remoteFailed, []string{"asks to wait 30 s"}},
		{"Pub/Sub not reached", 1, replyWith(notWritten), remoteFailed, []string{"Pub/Sub was not reached"}},
		{"the account disconnected", 1, replyWith(refusedWith("NET_AUTH_PROFILE_DISCONNECTED")), "NET_AUTH_PROFILE_DISCONNECTED", []string{"connects it again on the Plugins page", "nothing was asked of Pub/Sub for the topic"}},
		{"a lost answer to the topic", 1, lostAfter, effectUnknown, []string{"the topic:", "whether it took effect is not known", "safe to run again"}},
		{"a 500 to the policy read", 2, replyWith(backend), effectUnknown, []string{"the topic's publisher grant:", "already done: the topic " + topic + " was created"}},
		{"a policy that is not readable", 2, replyWith(gave(200, "[]", -1)), remoteFailed, []string{"the topic's policy could not be read"}},
		{"the policy changed meanwhile", 3, replyWith(gave(409, gerr(409, "aborted", "There were concurrent policy changes."), -1)), remoteFailed, []string{"the topic's policy changed while it was being read", "already done: the topic"}},
		{"the subscription refused", 4, replyWith(gave(400, gerr(400, "invalidArgument", "Invalid push endpoint."), -1)), remoteFailed, []string{"the push subscription:", "Pub/Sub refused it (400: Invalid push endpoint.)", "Gmail was allowed to publish to the topic"}},
		{"no permission to act as the account", 4, replyWith(gave(403, gerr(403, "forbidden", "User not authorized to act as service account aii-gmail-push@aii-mail.iam.gserviceaccount.com."), -1)), remoteFailed, []string{"not authorized to act as service account", "Service Account User"}},
	} {
		g, p, h, proc := setupRig(t)
		p.at[tc.at] = tc.step
		got := proc.invoke(h, "gmail.setup", setupArgs())
		if got.status == "succeeded" || got.code != tc.code {
			t.Errorf("%s: %s", tc.name, got.line())
			continue
		}
		for _, s := range tc.says {
			if !strings.Contains(got.said(), s) {
				t.Errorf("%s: the failure does not say %q: %s", tc.name, s, got.said())
			}
		}
		if len(g.watches) != 0 || len(p.calls) != tc.at {
			t.Errorf("%s: %d Pub/Sub calls and %d watches after the step that failed", tc.name, len(p.calls), len(g.watches))
		}
		proc.kill()
	}
}

func replyWith(r brokerReply) psStep { return func(*pubsub, httpCall) brokerReply { return r } }

// The watch is the last step: what refuses it leaves the resources made, and the
// words say so and that the daily renewal, or the same setup, finishes it.
func TestAWatchThatIsRefusedLeavesWhatWasMadeAndSaysSo(t *testing.T) {
	g, p, h, proc := setupRig(t)
	g.at[1] = replies(gave(403, gerr(403, "forbidden", "User not authorized to perform this action."), -1))
	got := proc.invoke(h, "gmail.setup", setupArgs())
	check(t, "watch refused", got, "failed", remoteFailed, "the watch was not started")
	for _, s := range []string{"the subscription " + subName + " was created", "the daily renewal will start it, or run setup again", "must grant publish to gmail-api-push@system.gserviceaccount.com"} {
		if !strings.Contains(got.said(), s) {
			t.Errorf("the failure does not say %q: %s", s, got.said())
		}
	}
	if p.subs[subName] == nil || len(h.kv) != 0 {
		t.Errorf("the subscription is %v and the store %v", p.subs[subName], h.kv)
	}
	if again := proc.invoke(h, "gmail.setup", setupArgs()); again.status != "succeeded" || again.text("subscription") != "already set up" {
		t.Errorf("setup after the watch failed: %s", again.line())
	}
}

// Nothing the plugin can be handed changes where the subscription points: the
// endpoint is the host's word for it, never a setting or an argument.
func TestTheEndpointIsOnlyWhatTheHostSaysThisIdentitysWebhookIs(t *testing.T) {
	_, p, h, proc := setupRig(t)
	h.extra["endpoint"] = "https://evil.example/hooks"
	args := setupArgs()
	args["endpoint"] = "https://evil.example/hooks"
	if got := proc.invoke(h, "gmail.setup", args); got.status != "succeeded" {
		t.Fatal(got.line())
	}
	if cfg, _ := p.subs[subName]["pushConfig"].(map[string]any); cfg["pushEndpoint"] != notifyURL {
		t.Errorf("the subscription points at %v", cfg)
	}
}

// What the manifest declares must carry the setup: the Pub/Sub host in the
// envelope and in every variant, and the operation among the provider's methods.
func TestTheManifestDeclaresWhatSetupCalls(t *testing.T) {
	raw, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Envelope   []string `json:"capability_envelope"`
		Interfaces []struct {
			ID      string   `json:"id"`
			Methods []string `json:"methods"`
		} `json:"interfaces"`
		Variants []struct {
			ID           string   `json:"id"`
			Capabilities []string `json:"variant_capabilities"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	has := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	if !has(m.Envelope, pubsubHost) {
		t.Errorf("the envelope %v lacks %s", m.Envelope, pubsubHost)
	}
	for _, v := range m.Variants {
		if !has(v.Capabilities, pubsubHost) {
			t.Errorf("variant %s %v lacks %s", v.ID, v.Capabilities, pubsubHost)
		}
	}
	for _, i := range m.Interfaces {
		if i.ID == "tools.provider" && !has(i.Methods, "gmail.setup") {
			t.Errorf("the provider's methods %v lack gmail.setup", i.Methods)
		}
	}
}

func TestTopicPartsReadTheOneFormAndNothingThatChangesAnAddress(t *testing.T) {
	for topic, ok := range map[string]bool{
		"projects/aii-mail/topics/gmail": true, "projects/example.com:p/topics/g_m~1": true,
		"": false, "projects/p/topics": false, "projects/p/topics/": false, "projects//topics/t": false,
		"project/p/topics/t": false, "projects/p/subscriptions/t": false, "projects/p/topics/t/x": false,
		"projects/p/topics/t?": false, "projects/p?/topics/t": false, "projects/p/topics/t#": false, "projects/p/topics/t%": false, "projects/p/topics/t u": false, "projects/p q/topics/t": false, "projects/../topics/t": false, "projects/p/topics/..": false, "projects/p/topics/t\n": false,
	} {
		if _, _, got := topicParts(topic); got != ok {
			t.Errorf("topicParts(%q) = %v, want %v", topic, got, ok)
		}
	}
}

// The longest setup — every step made, the subscription found and patched —
// (the topic made, the grant added, the subscription found and patched) is six Pub/Sub calls and the watch, each bounded, inside the worker's
// ninety-second invocation.
func TestTheLongestSetupFitsTheInvocation(t *testing.T) {
	_, p, h, proc := setupRig(t)
	p.subs[subName] = map[string]any{"name": subName, "topic": topic, "pushConfig": map[string]any{"pushEndpoint": "https://old.example"}}
	got := proc.invoke(h, "gmail.setup", setupArgs())
	if got.status != "succeeded" || got.text("subscription") != "updated" {
		t.Fatalf("setup: %s", got.line())
	}
	if len(p.calls) != 6 {
		t.Fatalf("the longest setup made %d Pub/Sub calls: %v", len(p.calls), p.ops())
	}
	total := writeTimeout // the watch
	for _, c := range p.calls {
		if c.args["timeout_ms"] != float64(pubsubTimeout) {
			t.Errorf("%s %s carries timeout %v, not the %d ms the invocation's budget counts", c.op, c.url, c.args["timeout_ms"], pubsubTimeout)
		}
		total += pubsubTimeout
	}
	if total > 90000 {
		t.Errorf("the bounds add to %d ms, over the ninety-second invocation", total)
	}
}

// The package needs what a host gained in 0.1.11 — Google's signed push token as a
// webhook scheme, operations called on a schedule, the arrivals the host recorded
// told back, its own webhook addresses — so it says so, and a 0.1.10 host refuses
// it by name before fetching a byte instead of failing later as something else.
func TestThePackageNamesTheHostItNeeds(t *testing.T) {
	raw, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Min string `json:"aiios_min_version"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Min != "0.1.11" || !aiiospkg.ValidHostVersion(m.Min) {
		t.Errorf("aiios_min_version is %q, want 0.1.11", m.Min)
	}
	if aiiospkg.CompareHostVersion("0.1.10", m.Min) >= 0 {
		t.Error("a 0.1.10 host is not below the floor")
	}
}
