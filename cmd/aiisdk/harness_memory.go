package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type harnessMemory struct {
	id           string
	text         string
	words        map[string]bool
	created      time.Time
	accesses     int64
	supersededBy string
}

func memoryWords(text string) map[string]bool {
	out := map[string]bool{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out[cur.String()] = true
			cur.Reset()
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return out
}

func memoryNormal(text string) string {
	words := memoryWords(text)
	keys := make([]string, 0, len(words))
	for w := range words {
		keys = append(keys, w)
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

func (h *harness) answerMemory(op string, arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	if !h.memGranted {
		h.observe("%s -> denied (run with -grant memory)", op)
		return nil, deny("memory is not granted in this harness run (aiisdk test -grant memory)", "POLICY_DENY")
	}
	if op == "memory.remember" {
		return h.answerRemember(arguments)
	}
	return h.answerRecall(arguments)
}

func (h *harness) currentMemory(id string) *harnessMemory {
	for _, m := range h.memories {
		if m.id == id {
			return m
		}
	}
	return nil
}

func harnessMemoryArguments(raw json.RawMessage, dst interface{}) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return false
	}
	return d.Decode(new(interface{})) == io.EOF
}

func (h *harness) memoryContextUnavailable(detail string) (json.RawMessage, json.RawMessage) {
	h.observe("memory -> refused: %s", detail)
	return failedSaying("OPERATION_ARGUMENT_INVALID", detail, map[string]interface{}{"detail": detail, "scope": "harness"}), nil
}

func harnessMemoryContext() map[string]interface{} {
	return map[string]interface{}{"applicability": "legacy"}
}

func (h *harness) answerRemember(arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	var a struct {
		Text          string            `json:"text"`
		Supersedes    string            `json:"supersedes"`
		Applicability string            `json:"applicability"`
		Evidence      []json.RawMessage `json:"evidence"`
	}
	if !harnessMemoryArguments(arguments, &a) {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	switch a.Applicability {
	case "", "legacy":
	case "task", "project", "reusable":
		return h.memoryContextUnavailable("the harness has no executing work session; scoped writes require the real host")
	default:
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if len(a.Evidence) != 0 {
		return h.memoryContextUnavailable("the harness has no interaction ledger and cannot validate evidence references; use the real host")
	}
	text := strings.TrimSpace(a.Text)
	if text == "" {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if len(text) > 16<<10 {
		return failed("MEMORY_TEXT_TOO_LARGE", nil), nil
	}
	now := time.Now().UTC()
	result := func(m *harnessMemory, outcome, of string) json.RawMessage {
		h.observe("memory.remember -> %s %s (temporary record, dies with this run; the host decides by similarity, this harness by identical words)", outcome, m.id)
		return succeeded(map[string]interface{}{
			"id": m.id, "outcome": outcome, "of": of,
			"created_at": m.created.Format(time.RFC3339Nano), "scope": "harness",
			"context": harnessMemoryContext(), "evidence": []interface{}{},
		})
	}
	if a.Supersedes != "" {
		old := h.currentMemory(a.Supersedes)
		if old == nil || old.supersededBy != "" {
			h.observe("memory.remember -> MEMORY_NOT_FOUND (%s is not a current memory of this run)", a.Supersedes)
			return failed("MEMORY_NOT_FOUND", nil), nil
		}
		m := h.newMemory(text, now)
		old.supersededBy = m.id
		return result(m, "updated", old.id), nil
	}
	normal := memoryNormal(text)
	for _, m := range h.memories {
		if m.supersededBy == "" && memoryNormal(m.text) == normal {
			m.accesses++
			return result(m, "reinforced", m.id), nil
		}
	}
	m := h.newMemory(text, now)
	return result(m, "created", ""), nil
}

func (h *harness) newMemory(text string, now time.Time) *harnessMemory {
	h.memoryN++
	m := &harnessMemory{id: fmt.Sprintf("hm_%d", h.memoryN), text: text, words: memoryWords(text), created: now}
	h.memories = append(h.memories, m)
	return m
}

var harnessMeaning = map[string]interface{}{
	"status": "source_unavailable", "basis": "",
	"detail": "the harness has no embeddings model; words and substrings answered",
}

func memoryHit(m *harnessMemory, match string, score float64) map[string]interface{} {
	tokens := strings.Fields(m.text)
	snippet := m.text
	if len(tokens) > 64 {
		snippet = strings.Join(tokens[:64], " ") + "…"
	}
	hit := map[string]interface{}{
		"id": m.id, "text": m.text, "snippet": snippet, "match": match,
		"score": score, "strength": 1.0, "fused": score, "attribution": "plugin", "ring": 4,
		"time": m.created.Format(time.RFC3339Nano), "accesses": m.accesses, "class": "operational",
		"superseded_by": m.supersededBy,
		"context":       harnessMemoryContext(), "evidence": []interface{}{},
	}
	if match == "recent" {

		delete(hit, "score")
		delete(hit, "strength")
		delete(hit, "fused")
	}
	return hit
}

func (h *harness) answerRecall(arguments json.RawMessage) (json.RawMessage, json.RawMessage) {
	var a struct {
		Query     string `json:"query"`
		Exact     bool   `json:"exact"`
		Since     string `json:"since"`
		Before    string `json:"before"`
		Limit     int    `json:"limit"`
		ID        string `json:"id"`
		Decay     string `json:"decay"`
		Selection string `json:"selection"`
		Recent    bool   `json:"recent"`
	}
	if !harnessMemoryArguments(arguments, &a) {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	legacySelected := false
	switch a.Selection {
	case "", "legacy":
		legacySelected = true
	case "relevant", "reusable":

	case "task", "project":
		return h.memoryContextUnavailable("the harness has no executing work session; task/project selection requires the real host")
	default:
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	query := strings.TrimSpace(a.Query)
	selectors := 0
	for _, selected := range []bool{query != "", a.ID != "", a.Recent} {
		if selected {
			selectors++
		}
	}
	if selectors != 1 || a.Limit < 0 || a.Limit > 50 || a.Recent && a.Exact {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if a.ID != "" && (a.Exact || a.Since != "" || a.Before != "" || a.Limit != 0 || a.Decay != "") {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	if a.ID != "" {
		m := h.currentMemory(a.ID)
		if m == nil || !legacySelected {
			h.observe("memory.recall id %s -> MEMORY_NOT_FOUND", a.ID)
			return failed("MEMORY_NOT_FOUND", nil), nil
		}
		m.accesses++
		h.observe("memory.recall id %s -> found (temporary record)", a.ID)
		return succeeded(map[string]interface{}{
			"hits": []interface{}{memoryHit(m, "id", 1.0)}, "status": "found", "matched": 1, "shown": 1,
			"policy": "none", "truncated": false, "meaning": harnessMeaning,
		}), nil
	}
	if a.Decay != "" && a.Decay != "carrd" && a.Decay != "actr" && a.Decay != "none" {
		return failed("OPERATION_ARGUMENT_INVALID", nil), nil
	}
	var since, before time.Time
	var err error
	if a.Since != "" {
		if since, err = time.Parse(time.RFC3339, a.Since); err != nil {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
	}
	if a.Before != "" {
		if before, err = time.Parse(time.RFC3339, a.Before); err != nil {
			return failed("OPERATION_ARGUMENT_INVALID", nil), nil
		}
	}
	limit := a.Limit
	if limit == 0 {
		limit = 7
	}
	policy := a.Decay
	if policy == "" {
		policy = "carrd"
	}
	if a.Recent {
		policy = "none"
	}
	qwords := memoryWords(query)
	lowerQuery := strings.ToLower(query)
	type scored struct {
		m     *harnessMemory
		match string
		score float64
	}
	var found []scored
	for _, m := range h.memories {
		if !legacySelected || m.supersededBy != "" {
			continue
		}
		if !since.IsZero() && m.created.Before(since) {
			continue
		}
		if !before.IsZero() && !m.created.Before(before) {
			continue
		}
		lower := strings.ToLower(m.text)
		switch {
		case a.Recent:
			found = append(found, scored{m, "recent", 0})
		case a.Exact:
			if strings.Contains(lower, lowerQuery) {
				found = append(found, scored{m, "exact_words", 1})
			}
		default:
			all := len(qwords) > 0
			for w := range qwords {
				if !m.words[w] {
					all = false
					break
				}
			}
			switch {
			case all && strings.Contains(lower, lowerQuery):
				found = append(found, scored{m, "both", 1})
			case all:
				found = append(found, scored{m, "exact_words", 0.9})
			case strings.Contains(lower, lowerQuery):
				found = append(found, scored{m, "fuzzy", 0.5})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].m.created.After(found[j].m.created)
	})
	matched := len(found)
	truncated := matched > limit
	if truncated {
		found = found[:limit]
	}
	hits := make([]interface{}, 0, len(found))
	for _, f := range found {
		if !a.Recent {
			f.m.accesses++
		}
		hits = append(hits, memoryHit(f.m, f.match, f.score))
	}
	status := "found"
	switch {
	case matched == 0:
		status = "found_nothing"
	case truncated:
		status = "partial"
	}
	if a.Recent {
		h.observe("memory.recall recent -> %s (%d matched, %d shown; temporary legacy records; inspection does not reinforce)", status, matched, len(hits))
	} else {
		h.observe("memory.recall %q -> %s (%d matched, %d shown; words and substrings, no decay — the host ranks by trigram and CARRD)", query, status, matched, len(hits))
	}
	return succeeded(map[string]interface{}{
		"hits": hits, "status": status, "matched": matched, "shown": len(hits),
		"policy": policy, "truncated": truncated, "meaning": harnessMeaning,
	}), nil
}
