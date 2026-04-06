package parser

import (
	"fmt"
	"testing"
)

// makeEntry creates a minimal Entry for fork detection tests.
func makeEntry(uuid, parentUUID, entryType string) Entry {
	return Entry{
		UUID:       uuid,
		ParentUUID: parentUUID,
		Type:       entryType,
	}
}

// TestFilterForks_NoForks verifies that a linear chain is returned unchanged.
func TestFilterForks_NoForks(t *testing.T) {
	entries := []Entry{
		makeEntry("a", "", "user"),
		makeEntry("b", "a", "assistant"),
		makeEntry("c", "b", "user"),
		makeEntry("d", "c", "assistant"),
	}

	result := FilterForks(entries)

	if len(result) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(result))
	}
	for i, e := range result {
		if e.UUID != entries[i].UUID {
			t.Errorf("entry %d: expected UUID %q, got %q", i, entries[i].UUID, e.UUID)
		}
	}
}

// TestFilterForks_LongestPath verifies that when a fork exists, the longer path
// is kept and the shorter path is dropped.
//
// Graph shape:
//
//	A -> B -> C -> D   (length 4 from root)
//	         B -> E -> F  (branch from B, length 2 from branch point)
//
// FilterForks should keep A, B, C, D and drop E, F.
func TestFilterForks_LongestPath(t *testing.T) {
	entries := []Entry{
		makeEntry("A", "", "user"),
		makeEntry("B", "A", "assistant"),
		makeEntry("C", "B", "user"),   // long branch
		makeEntry("D", "C", "assistant"), // long branch continues
		makeEntry("E", "B", "user"),   // short branch
		makeEntry("F", "E", "assistant"), // short branch continues
	}

	result := FilterForks(entries)

	wantUUIDs := []string{"A", "B", "C", "D"}
	if len(result) != len(wantUUIDs) {
		t.Fatalf("expected %d entries, got %d (UUIDs: %v)", len(wantUUIDs), len(result), uuids(result))
	}
	for i, want := range wantUUIDs {
		if result[i].UUID != want {
			t.Errorf("position %d: expected UUID %q, got %q", i, want, result[i].UUID)
		}
	}
}

// TestFilterForks_EqualLengthPaths verifies that when two branches are equal
// length, one path is returned (the implementation picks consistently).
func TestFilterForks_EqualLengthPaths(t *testing.T) {
	entries := []Entry{
		makeEntry("A", "", "user"),
		makeEntry("B", "A", "assistant"), // fork point
		makeEntry("C", "B", "user"),      // branch 1
		makeEntry("D", "B", "user"),      // branch 2
	}

	result := FilterForks(entries)

	// Should return A, B, and exactly one of C or D.
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d (UUIDs: %v)", len(result), uuids(result))
	}
	if result[0].UUID != "A" {
		t.Errorf("expected first entry A, got %q", result[0].UUID)
	}
	if result[1].UUID != "B" {
		t.Errorf("expected second entry B, got %q", result[1].UUID)
	}
}

// TestFilterForks_EmptyInput verifies the function handles an empty slice.
func TestFilterForks_EmptyInput(t *testing.T) {
	result := FilterForks(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = FilterForks([]Entry{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %v", result)
	}
}

// TestFilterForks_MultipleRoots verifies that a disconnected graph (multiple
// roots) is returned unchanged rather than dropping entries.
func TestFilterForks_MultipleRoots(t *testing.T) {
	entries := []Entry{
		makeEntry("A", "", "user"),
		makeEntry("B", "A", "assistant"),
		makeEntry("X", "", "user"), // second root -- disconnected graph
		makeEntry("Y", "X", "assistant"),
	}

	result := FilterForks(entries)

	// Multiple roots: return unchanged.
	if len(result) != len(entries) {
		t.Fatalf("expected %d entries unchanged, got %d", len(entries), len(result))
	}
}

// TestFilterForks_PreservesOrder verifies that returned entries follow the
// original slice order, not the DAG traversal order.
func TestFilterForks_PreservesOrder(t *testing.T) {
	// Entries stored out of traversal order to catch ordering bugs.
	entries := []Entry{
		makeEntry("D", "C", "assistant"), // index 0 -- deepest
		makeEntry("A", "", "user"),       // index 1 -- root
		makeEntry("C", "B", "user"),      // index 2
		makeEntry("B", "A", "assistant"), // index 3
		makeEntry("E", "B", "user"),      // index 4 -- short branch (dropped)
	}

	result := FilterForks(entries)

	// Long path is A->B->C->D. Short branch E is dropped.
	// Result should preserve original slice order: D, A, C, B.
	wantUUIDs := []string{"D", "A", "C", "B"}
	if len(result) != len(wantUUIDs) {
		t.Fatalf("expected %d entries, got %d (UUIDs: %v)", len(wantUUIDs), len(result), uuids(result))
	}
	for i, want := range wantUUIDs {
		if result[i].UUID != want {
			t.Errorf("position %d: expected UUID %q, got %q", i, want, result[i].UUID)
		}
	}
}

// TestFilterForks_DiamondConvergence verifies that a diamond-shaped DAG
// (where two branches from a fork converge on the same descendant) does not
// cause exponential recursion via subtreeSize. Without memoization this would
// hang; with memoization it completes in O(n) time.
//
// Graph shape (A is root, B is fork, C and D are branches, E is shared):
//
//	A -> B -> C -> E -> F
//	         D -> E -> F  (D also points to E, creating a diamond)
func TestFilterForks_DiamondConvergence(t *testing.T) {
	entries := []Entry{
		makeEntry("A", "", "user"),
		makeEntry("B", "A", "assistant"), // fork point
		makeEntry("C", "B", "user"),      // branch 1
		makeEntry("D", "B", "user"),      // branch 2
		makeEntry("E", "C", "user"),      // shared descendant (from branch 1)
		makeEntry("G", "D", "user"),      // shared descendant (from branch 2)
		makeEntry("F1", "E", "assistant"),
		makeEntry("F2", "F1", "assistant"),
		makeEntry("F3", "F2", "assistant"),
		makeEntry("F4", "F3", "assistant"),
		makeEntry("F5", "F4", "assistant"),
	}

	// Should not hang. Pick whichever branch is longer.
	result := FilterForks(entries)

	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Root and fork node must be present.
	if result[0].UUID != "A" {
		t.Errorf("expected root A, got %q", result[0].UUID)
	}
}

// TestFilterForks_LargeLinearWithFork verifies that a session with many linear
// entries and a single fork near the start completes quickly (no exponential
// recursion). This is the shape that caused the original hang.
func TestFilterForks_LargeLinearWithFork(t *testing.T) {
	// Build a chain of 500 entries with a fork near the start.
	entries := make([]Entry, 0, 502)
	entries = append(entries, makeEntry("root", "", "user"))
	entries = append(entries, makeEntry("fork", "root", "assistant"))
	// Short branch (1 entry)
	entries = append(entries, makeEntry("short", "fork", "user"))
	// Long branch (500 entries)
	prev := "fork"
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("n%d", i)
		entries = append(entries, makeEntry(id, prev, "user"))
		prev = id
	}

	result := FilterForks(entries)

	// Long path should be chosen: root + fork + 500 = 502 entries.
	if len(result) != 502 {
		t.Fatalf("expected 502 entries, got %d", len(result))
	}
}

// uuids extracts UUIDs from a slice of entries for use in test error messages.
func uuids(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.UUID
	}
	return out
}
