package main

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

const (
	pubsubAPI  = "https://pubsub.googleapis.com/v1/"
	pubsubHost = "net.outbound:pubsub.googleapis.com:443"

	pubsubScope = "pubsub (the pubsub service's consent, chosen when the account is made)"

	pubsubTimeout    = 10000
	gmailPushAccount = "serviceAccount:gmail-api-push@system.gserviceaccount.com"
	publisherRole    = "roles/pubsub.publisher"
)

func topicParts(topic string) (project, id string, ok bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[2] != "topics" || !plainName(parts[1]) || !plainName(parts[3]) {
		return "", "", false
	}
	return parts[1], parts[3], true
}

func plainName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~+:", r)) {
			return false
		}
	}
	return s != "" && s != "." && s != ".."
}

func subscriptionName(project, topicID string) string {
	return "projects/" + project + "/subscriptions/" + topicID + "-push"
}

type setupRun struct {
	handle string
	done   []string
}

func (r *setupRun) said() string {
	if len(r.done) == 0 {
		return "nothing had been done"
	}
	return "already done: " + strings.Join(r.done, "; ")
}

func (r *setupRun) call(step, method, path, body string, tolerated ...int) (sdk.HTTPResult, error) {
	var res sdk.HTTPResult
	var err error
	target := pubsubAPI + path
	switch method {
	case "GET":
		res, err = sdk.HTTP.Get(target, reading(r.handle, pubsubTimeout))
	case "PUT":
		res, err = sdk.HTTP.Put(target, pubsubWriting(r.handle, body))
	case "PATCH":
		res, err = sdk.HTTP.Patch(target, pubsubWriting(r.handle, body))
	default:
		res, err = sdk.HTTP.Post(target, pubsubWriting(r.handle, body))
	}
	stop := func(code, said string) error {
		return sdk.Fail(code, step+": "+said+" — "+r.said()+"; setup is safe to run again")
	}
	switch ended(res, err) {
	case refused:
		return res, refusal(err, "nothing was asked of Pub/Sub for "+step+" — "+r.said())
	case unreached:
		return res, stop(remoteFailed, "Pub/Sub was not reached")
	case wasLost, unread:
		return res, stop(effectUnknown, why(res, err)+", so whether it took effect is not known")
	}
	for _, t := range tolerated {
		if res.Status == t {
			return res, nil
		}
	}
	switch s := res.Status; {
	case s >= 200 && s < 300:
		return res, nil
	case s == 403:
		return res, stop(remoteFailed, pubsubForbidden(res))
	case s == 429:
		return res, stop(remoteFailed, "Pub/Sub refused it ("+gmailSaid(res)+") and "+wait(res))
	case s >= 500:
		return res, stop(effectUnknown, "Pub/Sub answered "+gmailSaid(res)+", so whether it took effect is not known")
	}
	return res, stop(remoteFailed, "Pub/Sub refused it ("+gmailSaid(res)+")")
}

func pubsubForbidden(res sdk.HTTPResult) string {
	said := "Pub/Sub refused it (" + gmailSaid(res) + ")"
	switch denialOf(res) {
	case deniedScope:
		return said + ": the Google account's consent does not include " + pubsubScope + "; the operator connects the account again on the Plugins page with the Pub/Sub service"
	case deniedDisabled:
		return said + ": the Pub/Sub API is not enabled in the Google project the account was connected through; the operator enables it in that project's Cloud console"
	}
	return said + ": the Google account needs permission in this project to do that — Pub/Sub Admin on the topic and its policy, and Service Account User on the push service account"
}

func pubsubWriting(handle, body string) *sdk.HTTPOptions {
	return &sdk.HTTPOptions{Body: body, ContentType: "application/json", AuthProfile: handle, TimeoutMS: pubsubTimeout}
}

func setup(c sdk.Call) (any, error) {
	const none = "nothing was created or changed"
	if err := confirmed(c, none); err != nil {
		return nil, err
	}
	vals, err := sdk.Settings.Load()
	if err != nil {
		return nil, sdk.Fail(argInvalid, "the plugin's settings could not be read — "+none)
	}
	handle, _ := vals.Handle("google")
	topic, _ := vals.String("topic")
	account, _ := vals.String("push_account")
	project, topicID, okTopic := topicParts(topic)
	endpoint, hasEndpoint := c.WebhookURL("notify")
	switch {
	case handle == "":
		return nil, sdk.Fail(argInvalid, "the google handle is not set — connect a Google account on the Plugins page, made with the gmail and pubsub services; "+none)
	case !okTopic:
		return nil, sdk.Fail(argInvalid, "the topic setting is the Pub/Sub topic to create, projects/<project>/topics/<topic>; it is not set or is not one — "+none)
	case !isAddress(account) || !strings.HasSuffix(account, ".gserviceaccount.com"):
		return nil, sdk.Fail(argInvalid, "the push_account setting is the service account the push subscription signs as, <name>@<project>.iam.gserviceaccount.com; it is not set or is not one — "+none)
	case !hasEndpoint:
		return nil, sdk.Fail(remoteFailed, "this identity has no public origin, so there is no address for Pub/Sub to push to — claim a public name under Settings → Public Name; "+none)
	}
	run := &setupRun{handle: handle}
	out := map[string]any{"endpoint": endpoint}
	subscription := subscriptionName(project, topicID)

	res, err := run.call("the topic", "PUT", topic, `{}`, 409)
	if err != nil {
		return nil, err
	}
	if res.Status == 409 {
		out["topic"] = "already there"
		run.done = append(run.done, "the topic "+topic+" was already there")
	} else {
		out["topic"] = "created"
		run.done = append(run.done, "the topic "+topic+" was created")
	}

	granted, err := run.allowGmailToPublish(topic)
	if err != nil {
		return nil, err
	}
	out["publisher"] = granted

	made, err := run.pushSubscription(subscription, topic, endpoint, account)
	if err != nil {
		return nil, err
	}
	out["subscription"] = made
	out["subscription_name"] = subscription

	expiration, err := startWatch(handle, topic, "the watch was not started — "+run.said()+"; the daily renewal will start it, or run setup again")
	if err != nil {
		return nil, err
	}
	out["watch_expires"] = expiration
	return out, nil
}

func (r *setupRun) allowGmailToPublish(topic string) (string, error) {
	const step = "the topic's publisher grant"
	res, err := r.call(step, "GET", topic+":getIamPolicy", "")
	if err != nil {
		return "", err
	}
	var policy map[string]any
	if !isObject(res.Body) || json.Unmarshal(res.Body, &policy) != nil {
		return "", sdk.Fail(remoteFailed, step+": the topic's policy could not be read — "+r.said()+"; setup is safe to run again")
	}
	bindings, _ := policy["bindings"].([]any)
	for _, b := range bindings {
		binding, _ := b.(map[string]any)
		if binding["role"] != publisherRole || binding["condition"] != nil {
			continue
		}
		members, _ := binding["members"].([]any)
		for _, m := range members {
			if m == gmailPushAccount {
				r.done = append(r.done, "Gmail could already publish to the topic")
				return "already granted", nil
			}
		}
		binding["members"] = append(members, gmailPushAccount)
		return r.writePolicy(topic, policy, step)
	}
	policy["bindings"] = append(bindings, map[string]any{"role": publisherRole, "members": []any{gmailPushAccount}})
	return r.writePolicy(topic, policy, step)
}

func (r *setupRun) writePolicy(topic string, policy map[string]any, step string) (string, error) {
	body, _ := json.Marshal(map[string]any{"policy": policy})
	res, err := r.call(step, "POST", topic+":setIamPolicy", string(body), 409, 412)
	if err != nil {
		return "", err
	}
	if res.Status == 409 || res.Status == 412 {
		return "", sdk.Fail(remoteFailed, step+": the topic's policy changed while it was being read ("+gmailSaid(res)+") — "+r.said()+"; setup is safe to run again")
	}
	r.done = append(r.done, "Gmail was allowed to publish to the topic")
	return "granted", nil
}

func (r *setupRun) pushSubscription(name, topic, endpoint, account string) (string, error) {
	const step = "the push subscription"
	pushConfig := map[string]any{"pushEndpoint": endpoint, "oidcToken": map[string]any{"serviceAccountEmail": account, "audience": endpoint}}
	body, _ := json.Marshal(map[string]any{"topic": topic, "pushConfig": pushConfig, "ackDeadlineSeconds": 10})
	res, err := r.call(step, "PUT", name, string(body), 409)
	if err != nil {
		return "", err
	}
	if res.Status != 409 {
		r.done = append(r.done, "the subscription "+name+" was created")
		return "created", nil
	}
	got, err := r.call(step, "GET", name, "")
	if err != nil {
		return "", err
	}
	have := sdk.Object(got.Body)
	if str(have, "topic") != topic {
		return "", sdk.Fail(remoteFailed, step+": "+name+" exists on another topic ("+clip(str(have, "topic"), 200)+"), which this plugin does not move — delete it, or set another topic — "+r.said())
	}
	cfg := have.Object("pushConfig")
	if str(cfg, "pushEndpoint") == endpoint && str(cfg.Object("oidcToken"), "serviceAccountEmail") == account && str(cfg.Object("oidcToken"), "audience") == endpoint {
		r.done = append(r.done, "the subscription "+name+" was already set up as wanted")
		return "already set up", nil
	}
	patch, _ := json.Marshal(map[string]any{"subscription": map[string]any{"pushConfig": pushConfig}, "updateMask": "pushConfig"})
	if _, err := r.call(step, "PATCH", name, string(patch)); err != nil {
		return "", err
	}
	r.done = append(r.done, "the subscription "+name+" was pointed at this identity's endpoint")
	return "updated", nil
}

var describeSetup = fmt.Sprint("Set up Pub/Sub push for this mailbox in the operator's Google project: create the topic and a push subscription named in this plugin's settings if they are not there, let Gmail publish to the topic, point the subscription at this identity's public webhook signed as the push service account, and start the watch. It creates and changes resources in that project; every step is safe to run again")
