package main

// The removal of a file's stale chunks, against a store that fails
// part-way. A case under tests/ proves what a whole ingest reports; what
// no case can show is an ingest cut short while it removes, and that is
// what the order of the removal is for.

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// store is RING4 as the removal sees it. cutAt is the delete that fails,
// counted from 1; 0 is none.
type store struct {
	keys    map[string]string
	deletes int
	cutAt   int
	order   []string
}

var errCut = errors.New("cut short")

func (s *store) Get(key string) (string, bool, error) {
	v, ok := s.keys[key]
	return v, ok, nil
}

func (s *store) Delete(key string) (bool, error) {
	s.deletes++
	if s.deletes == s.cutAt {
		return false, errCut
	}
	_, ok := s.keys[key]
	delete(s.keys, key)
	s.order = append(s.order, key)
	return ok, nil
}

// holding is a store that holds n chunks of each named path.
func holding(n int, paths ...string) *store {
	s := &store{keys: map[string]string{}}
	for _, p := range paths {
		for i := 0; i < n; i++ {
			s.keys[chunkKey(p, i)] = "text"
		}
	}
	return s
}

// chunksHeld is the chunk numbers the store holds for path, sorted.
func chunksHeld(s *store, path string) []int {
	var out []int
	for key := range s.keys {
		rest, ok := strings.CutPrefix(key, prefix+path+":")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(rest); err == nil && strconv.Itoa(n) == rest {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func TestStaleChunksAreRemovedAndTheKeptOnesStay(t *testing.T) {
	s := holding(6, "a.md", "b.md", "a.md:1")
	removed, err := removeStale(s, "a.md", 2)
	if err != nil || removed != 4 {
		t.Fatalf("removeStale = %d, %v; want 4 removed and no error", removed, err)
	}
	if got := chunksHeld(s, "a.md"); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("a.md holds chunks %v; want 0 and 1", got)
	}
	// Another file's chunks are not this file's, whatever its name
	// begins with.
	for _, other := range []string{"b.md", "a.md:1"} {
		if got := chunksHeld(s, other); len(got) != 6 {
			t.Fatalf("%s holds chunks %v; want all six", other, got)
		}
	}
	if len(s.keys) != 14 {
		t.Fatalf("the store holds %d keys; want 14", len(s.keys))
	}
}

func TestNothingStaleRemovesNothing(t *testing.T) {
	s := holding(3, "a.md")
	removed, err := removeStale(s, "a.md", 3)
	if err != nil || removed != 0 || s.deletes != 0 {
		t.Fatalf("removeStale = %d, %v after %d deletes; want none of each", removed, err, s.deletes)
	}
	// A file that grew: there is nothing past its end either.
	if removed, err := removeStale(s, "a.md", 5); err != nil || removed != 0 || s.deletes != 0 {
		t.Fatalf("removeStale past the end = %d, %v after %d deletes; want none of each", removed, err, s.deletes)
	}
}

// TestARemovalCutShortIsFinishedByTheNext is the reason for the order. An
// ingest that ends while it removes must leave the file's keys in one run
// from 0, wherever it was cut: the next ingest looks from its own end
// upward until a key is missing, and a key beyond a gap is never looked
// for again.
func TestARemovalCutShortIsFinishedByTheNext(t *testing.T) {
	const held, kept = 9, 2
	for cut := 1; cut <= held-kept; cut++ {
		s := holding(held, "a.md")
		s.cutAt = cut
		removed, err := removeStale(s, "a.md", kept)
		if !errors.Is(err, errCut) {
			t.Fatalf("cut at delete %d: err = %v; want the store's failure", cut, err)
		}
		if removed != cut-1 {
			t.Fatalf("cut at delete %d: removed = %d; want %d", cut, removed, cut-1)
		}
		left := chunksHeld(s, "a.md")
		for i, n := range left {
			if n != i {
				t.Fatalf("cut at delete %d: the file's chunks are %v: not one run from 0", cut, left)
			}
		}
		if len(left) != held-(cut-1) {
			t.Fatalf("cut at delete %d: %d chunks left; want %d", cut, len(left), held-(cut-1))
		}

		s.cutAt = 0
		again, err := removeStale(s, "a.md", kept)
		if err != nil {
			t.Fatalf("cut at delete %d: the next removal failed: %v", cut, err)
		}
		if left := chunksHeld(s, "a.md"); len(left) != kept {
			t.Fatalf("cut at delete %d: after the next removal the file holds chunks %v; want %d (it removed %d)", cut, left, kept, again)
		}
		if removed+again != held-kept {
			t.Fatalf("cut at delete %d: %d + %d removed; want %d in all", cut, removed, again, held-kept)
		}
	}
}

func TestTheFarEndGoesFirst(t *testing.T) {
	s := holding(5, "a.md")
	if _, err := removeStale(s, "a.md", 2); err != nil {
		t.Fatal(err)
	}
	want := []string{chunkKey("a.md", 4), chunkKey("a.md", 3), chunkKey("a.md", 2)}
	if strings.Join(s.order, " ") != strings.Join(want, " ") {
		t.Fatalf("removed in the order %v; want %v", s.order, want)
	}
}
