package parser

import (
	"testing"
	"time"
)

func TestGroupSessionsByDate(t *testing.T) {
	// Fixed "now": 2025-06-15 14:00:00 local time.
	now := time.Date(2025, 6, 15, 14, 0, 0, 0, time.Local)
	todayStart := time.Date(2025, 6, 15, 0, 0, 0, 0, time.Local)

	sessions := []SessionInfo{
		{SessionID: "today1", ModTime: now.Add(-1 * time.Hour)},
		{SessionID: "today2", ModTime: todayStart.Add(5 * time.Minute)},
		{SessionID: "yesterday", ModTime: todayStart.Add(-6 * time.Hour)},
		{SessionID: "thisweek", ModTime: todayStart.AddDate(0, 0, -3)},
		{SessionID: "thismonth", ModTime: todayStart.AddDate(0, 0, -15)},
		{SessionID: "older", ModTime: todayStart.AddDate(0, 0, -60)},
	}

	groups := groupSessionsByDateAt(sessions, now)

	if len(groups) != 5 {
		t.Fatalf("got %d groups, want 5", len(groups))
	}

	want := []struct {
		cat   DateCategory
		count int
		ids   []string
	}{
		{DateToday, 2, []string{"today1", "today2"}},
		{DateYesterday, 1, []string{"yesterday"}},
		{DateThisWeek, 1, []string{"thisweek"}},
		{DateThisMonth, 1, []string{"thismonth"}},
		{DateOlder, 1, []string{"older"}},
	}

	for i, w := range want {
		g := groups[i]
		if g.Category != w.cat {
			t.Errorf("group[%d].Category = %q, want %q", i, g.Category, w.cat)
		}
		if len(g.Sessions) != w.count {
			t.Errorf("group[%d] (%s) has %d sessions, want %d", i, w.cat, len(g.Sessions), w.count)
			continue
		}
		for j, id := range w.ids {
			if g.Sessions[j].SessionID != id {
				t.Errorf("group[%d].Sessions[%d].SessionID = %q, want %q", i, j, g.Sessions[j].SessionID, id)
			}
		}
	}
}

func TestGroupSessionsByDate_EmptyInput(t *testing.T) {
	groups := groupSessionsByDateAt(nil, time.Now())
	if len(groups) != 0 {
		t.Fatalf("got %d groups for nil input, want 0", len(groups))
	}
}

func TestGroupSessionsByDate_SkipsEmptyCategories(t *testing.T) {
	now := time.Date(2025, 6, 15, 14, 0, 0, 0, time.Local)

	// Only "today" sessions -- other categories should be absent.
	sessions := []SessionInfo{
		{SessionID: "a", ModTime: now.Add(-30 * time.Minute)},
		{SessionID: "b", ModTime: now.Add(-2 * time.Hour)},
	}

	groups := groupSessionsByDateAt(sessions, now)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].Category != DateToday {
		t.Errorf("group[0].Category = %q, want %q", groups[0].Category, DateToday)
	}
	if len(groups[0].Sessions) != 2 {
		t.Errorf("group[0] has %d sessions, want 2", len(groups[0].Sessions))
	}
}

func TestGroupSessionsByDate_PreservesInputOrder(t *testing.T) {
	now := time.Date(2025, 6, 15, 14, 0, 0, 0, time.Local)

	// Sessions within a group should keep their input order (caller sorts).
	sessions := []SessionInfo{
		{SessionID: "first", ModTime: now.Add(-3 * time.Hour)},
		{SessionID: "second", ModTime: now.Add(-1 * time.Hour)},
		{SessionID: "third", ModTime: now.Add(-5 * time.Hour)},
	}

	groups := groupSessionsByDateAt(sessions, now)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	ids := make([]string, len(groups[0].Sessions))
	for i, s := range groups[0].Sessions {
		ids[i] = s.SessionID
	}
	if ids[0] != "first" || ids[1] != "second" || ids[2] != "third" {
		t.Errorf("input order not preserved: got %v", ids)
	}
}
