package main

import "testing"

// .
// .
func TestReview2PrereqRowIsLocalNotHost(t *testing.T) {
	r := &report{}
	r.incompleteLocal("prerequisite", "tinygo not found", "install it")
	if len(r.entries) != 1 || r.entries[0].Class != "local" {
		t.Fatalf("a local prerequisite gap must be class local, got %+v", r.entries)
	}
}

// .
// .
func TestReview2EveryReportNamesHostBoundary(t *testing.T) {
	r := &report{}
	r.fail("build", "boom")
	r.ensureBoundary()
	byName := map[string]checkEntry{}
	for _, e := range r.entries {
		byName[e.Name] = e
		if e.Class == "host" && e.Status == "pass" {
			t.Fatalf("host row %q must never be pass", e.Name)
		}
	}
	for _, n := range []string{"signature", "host-activation", "host-receipt", "manifest_hash-consumer"} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("a failing report must still name the host boundary; missing %q", n)
		}
	}
	before := len(r.entries)
	r.ensureBoundary()
	if len(r.entries) != before {
		t.Fatalf("ensureBoundary must not double-append (%d -> %d)", before, len(r.entries))
	}
}
