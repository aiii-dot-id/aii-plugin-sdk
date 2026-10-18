package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const (
	checkPassed     = "passed"
	checkWrong      = "wrong"
	checkTimedOut   = "timed_out"
	checkUnresolved = "unresolved"
	checkUnanswered = "unanswered"
)

type checkResult struct {
	name    string
	outcome string
	cosine  *float64
	took    time.Duration
	words   string

	ended bool
}

type checkCall func(operation string, args map[string]interface{}) (*workerdrive.Reply, error)

type sessionRun func(n int, c aiiospkg.Check) checkResult

func runChecks(checks []aiiospkg.Check, call checkCall, session sessionRun, dimension int, now func() time.Time) []checkResult {
	out := make([]checkResult, 0, len(checks))
	gone := false
	for i, c := range checks {
		if c.Lane == aiiospkg.LaneSession && c.Unresolved == "" && !gone {
			r := checkResult{name: c.Name, outcome: checkUnanswered, words: "not asked: this plugin answers no session lane here"}
			if session != nil {
				r = session(i+1, c)
			}
			out = append(out, r)
			continue
		}
		r := runCheck(c, call, dimension, now, gone)

		gone = gone || r.ended
		out = append(out, r)
	}
	return out
}

func callDeadlineWords(d time.Duration) string {
	return fmt.Sprintf("did not answer within the %d ms a call is given", d.Milliseconds())
}

func runCheck(c aiiospkg.Check, call checkCall, dimension int, now func() time.Time, gone bool) checkResult {
	r := checkResult{name: c.Name}
	switch {
	case c.Unresolved != "":
		r.outcome, r.words = checkUnresolved, c.Unresolved
		return r
	case gone:
		r.outcome, r.words = checkUnanswered, "not asked: the plugin stopped answering at an earlier check"
		return r
	}
	sent := now()
	var reply *workerdrive.Reply
	var err error
	switch c.Lane {
	case aiiospkg.LaneEmbeddings:
		reply, err = call(aiiospkg.MethodEmbed, map[string]interface{}{"kind": c.Kind, "text": c.Text})
	default:
		args := make(map[string]interface{}, len(c.Arguments))
		for k, v := range c.Arguments {
			args[k] = v
		}
		reply, err = call(c.Operation, args)
	}
	r.took = now().Sub(sent)
	switch {
	case errors.Is(err, workerdrive.ErrNoAnswer):
		r.outcome, r.words, r.ended = checkTimedOut, callDeadlineWords(r.took), true
		return r
	case err != nil:
		r.outcome, r.words = checkUnanswered, "not answered: "+err.Error()
		return r
	}
	if c.Lane == aiiospkg.LaneEmbeddings {
		judgeVector(&r, c, reply, dimension)
	} else {
		judgeAnswer(&r, c, reply)
	}
	if r.outcome == checkPassed && c.Within > 0 && r.took > c.Within {
		r.outcome = checkTimedOut
		r.words = fmt.Sprintf("answered as you say in %d ms, past its bound of %d ms", r.took.Milliseconds(), c.Within.Milliseconds())
	}
	return r
}

func judgeVector(r *checkResult, c aiiospkg.Check, reply *workerdrive.Reply, dimension int) {
	if reply.IsError || (reply.Status != "" && reply.Status != "succeeded") {
		r.outcome, r.words = checkUnanswered, "not answered: it answered with an error in place of a vector"
		return
	}
	got, why := readVector(reply.OperationResult, dimension)
	if why != "" {
		r.outcome, r.words = checkWrong, "answered with something that is not a vector: "+why
		return
	}
	cos := aiiospkg.FloatCosine(got, c.Expect.Cosine.Reference)
	r.cosine = &cos
	if cos >= c.Expect.Cosine.Min {
		r.outcome = checkPassed
		return
	}
	r.outcome = checkWrong
	r.words = fmt.Sprintf("came back at %.3f of its reference, under the %.3f it asks", cos, c.Expect.Cosine.Min)
}

func readVector(raw json.RawMessage, dimension int) ([]float32, string) {
	var reply struct {
		Vector []json.RawMessage `json:"vector"`
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || len(members) != 1 || members["vector"] == nil || json.Unmarshal(raw, &reply) != nil {
		return nil, `the reply is not an object whose one member is "vector", a list of numbers`
	}
	if len(reply.Vector) != dimension {
		return nil, fmt.Sprintf("the reply holds %d numbers, the declared dimension is %d", len(reply.Vector), dimension)
	}
	out := make([]float32, len(reply.Vector))
	var norm float64
	for i, item := range reply.Vector {
		var f float64
		if len(item) == 0 || !(item[0] == '-' || (item[0] >= '0' && item[0] <= '9')) || json.Unmarshal(item, &f) != nil {
			return nil, `the reply is not an object whose one member is "vector", a list of numbers`
		}
		v := float32(f)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, "the reply holds a number that is not finite"
		}
		out[i] = v
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return nil, "the reply is all zeros, which no cosine can be taken against"
	}
	return out, ""
}

func judgeAnswer(r *checkResult, c aiiospkg.Check, reply *workerdrive.Reply) {
	switch {
	case reply.IsError:
		var eo struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(reply.Error, &eo)
		r.outcome, r.words = checkWrong, "answered with an error: "+oneLine(eo.Message)
		return
	case reply.Status != "" && reply.Status != "succeeded":
		r.outcome, r.words = checkWrong, fmt.Sprintf("answered %s: %s", reply.Status, oneLine(firstNonEmpty(reply.Reason, reply.ReasonCode)))
		return
	}
	output := string(reply.OperationResult)
	if words := expectHolds(c.Expect, output); words != "" {
		r.outcome, r.words = checkWrong, words
		return
	}
	r.outcome = checkPassed
}

func expectHolds(e aiiospkg.Expect, output string) string {
	if len(e.Contains) > 0 {
		for _, w := range e.Contains {
			if !strings.Contains(output, w) {
				return fmt.Sprintf("its answer lacks %q: %s", w, oneLine(output))
			}
		}
		return ""
	}
	j := e.JSON
	if j == nil {
		return "the check expects nothing an operation answers"
	}
	var doc interface{}
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		return "its answer is not JSON: " + oneLine(output)
	}
	got, found := aiiospkg.Pointer(doc, j.Path)
	if !found {
		return fmt.Sprintf("its answer holds nothing at %q: %s", j.Path, oneLine(output))
	}
	if j.HasEquals() {
		if !aiiospkg.JSONEqual(got, j.Equals) {
			want, _ := json.Marshal(j.Equals)
			have, _ := json.Marshal(got)
			return fmt.Sprintf("the value at %q is %s, not %s", j.Path, oneLine(string(have)), oneLine(string(want)))
		}
		return ""
	}
	n, isNum := got.(float64)
	switch {
	case !isNum:
		return fmt.Sprintf("the value at %q is not a number", j.Path)
	case !(n >= *j.AtLeast):
		return fmt.Sprintf("the value at %q is %g, under %g", j.Path, n, *j.AtLeast)
	}
	return ""
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= 160 {
		return s
	}
	return string([]rune(s)[:160]) + "…"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func reportChecks(rep *report, results []checkResult) {
	first := ""
	for _, r := range results {
		took := fmt.Sprintf("%d ms", r.took.Milliseconds())
		if r.cosine != nil {
			took += fmt.Sprintf(", cosine %.3f", *r.cosine)
		}
		if r.outcome == checkPassed {
			rep.pass("check "+r.name, "passed ("+took+")")
			continue
		}
		if r.outcome == checkNotRunHere {
			rep.incompleteLocal("check "+r.name, r.words, "a host converts a recording to the engine's input format; record the check at the rate your engine answers")
			continue
		}
		rep.fail("check "+r.name, fmt.Sprintf("%s: %s (%s)", r.outcome, r.words, took))
		if first == "" {
			first = r.name
		}
	}
	if first != "" {
		rep.note("checks", fmt.Sprintf("the first check that did not pass is %q: on a host it decides whether this set is used (a wrong answer or an unresolved value keeps the set off that machine; a timeout leaves it serving until it has timed out at 3 starts within 24 hours)", first))
	}
}

func declaresWasm(cfg *aiiospkg.AuthorConfig) bool {
	return wasmVariant(cfg) != nil
}

func wasmVariant(cfg *aiiospkg.AuthorConfig) *aiiospkg.AuthorVariant {
	platform, arch := hostPlatformArch()
	var first *aiiospkg.AuthorVariant
	for i := range cfg.Variants {
		v := &cfg.Variants[i]
		if v.ExecutionRuntime != "wasm_component" {
			continue
		}
		if v.Platform == platform && v.Arch == arch {
			return v
		}
		if first == nil {
			first = v
		}
	}
	return first
}

func nativeVariant(cfg *aiiospkg.AuthorConfig) *aiiospkg.AuthorVariant {
	platform, arch := hostPlatformArch()
	for i := range cfg.Variants {
		v := &cfg.Variants[i]
		if v.ExecutionRuntime == "native_t3_component" && v.Platform == platform && v.Arch == arch {
			return v
		}
	}
	return nil
}

type invoker func(n int, operation string, args json.RawMessage) (*workerdrive.Reply, error)

func runPackageChecks(rep *report, h *harness, dir string, cfg *aiiospkg.AuthorConfig, variant string, invoke invoker, session sessionRun) {
	tree, err := aiiospkg.ReadTree(stageDir(dir, cfg))
	if err != nil {
		rep.fail("checks", "the staged package could not be read: "+err.Error())
		return
	}
	install := tree.InstallFiles()
	raw, ok := install[aiiospkg.ValidationFile]
	if !ok {
		rep.fail("checks", "validation_file is set and the staged package carries no "+aiiospkg.ValidationFile)
		return
	}
	v, err := aiiospkg.ParseValidation(raw, func(rel string) ([]byte, bool, error) {
		b, present := install[rel]
		return b, present, nil
	})
	if err != nil {
		rep.fail("checks", err.Error())
		return
	}
	dimension := 0
	if b, present := install[aiiospkg.EmbeddingsFile]; present {
		if decl, err := aiiospkg.ParseEmbeddings(b); err == nil {
			dimension = decl.Dimension
		}
	}
	var checks []aiiospkg.Check
	for _, c := range v.Checks {
		if c.For(variant) {
			checks = append(checks, c)
		}
	}
	if len(checks) == 0 {
		rep.note("checks", "no check is for set "+variant+"; a host uses it on its readiness alone")
		return
	}
	n := 0
	call := func(operation string, args map[string]interface{}) (*workerdrive.Reply, error) {
		n++
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return invoke(n, operation, withHostClock(raw))
	}
	h.checking.Store(true)
	results := runChecks(checks, call, session, dimension, time.Now)
	h.checking.Store(false)
	reportChecks(rep, results)
}
