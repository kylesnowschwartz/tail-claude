package parser

import (
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

// uuids extracts UUIDs from a slice of entries for use in test error messages.
func uuids(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.UUID
	}
	return out
}
