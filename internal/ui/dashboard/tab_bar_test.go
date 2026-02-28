package dashboard

import (
	"strings"
	"testing"
)

func TestNewTabBar(t *testing.T) {
	tb := newTabBar(80)

	if len(tb.tabs) != 1 {
		t.Fatalf("expected 1 default tab, got %d", len(tb.tabs))
	}
	if tb.tabs[0].ID != "diff" {
		t.Fatalf("expected default tab ID 'diff', got %q", tb.tabs[0].ID)
	}
	if tb.ActiveID() != "diff" {
		t.Fatalf("expected active ID 'diff', got %q", tb.ActiveID())
	}
}

func TestSetTabs(t *testing.T) {
	tb := newTabBar(80)
	tb.SetTabs([]string{"host1", "host2", "host3"})

	if len(tb.tabs) != 4 {
		t.Fatalf("expected 4 tabs, got %d", len(tb.tabs))
	}
	if tb.tabs[0].ID != "diff" {
		t.Fatalf("expected first tab to be 'diff', got %q", tb.tabs[0].ID)
	}
	if tb.tabs[1].ID != "host1" {
		t.Fatalf("expected second tab to be 'host1', got %q", tb.tabs[1].ID)
	}
}

func TestSetTabsPreservesActive(t *testing.T) {
	tb := newTabBar(80)
	tb.SetTabs([]string{"host1", "host2"})
	tb.SetActiveByID("host2")

	if tb.ActiveID() != "host2" {
		t.Fatalf("expected active 'host2', got %q", tb.ActiveID())
	}

	// Rebuild tabs with host2 still present.
	tb.SetTabs([]string{"host1", "host2", "host3"})
	if tb.ActiveID() != "host2" {
		t.Fatalf("expected active to be preserved as 'host2', got %q", tb.ActiveID())
	}
}

func TestSetTabsResetsWhenRemoved(t *testing.T) {
	tb := newTabBar(80)
	tb.SetTabs([]string{"host1", "host2"})
	tb.SetActiveByID("host2")

	// Rebuild without host2.
	tb.SetTabs([]string{"host1", "host3"})
	if tb.ActiveID() != "diff" {
		t.Fatalf("expected active to reset to 'diff', got %q", tb.ActiveID())
	}
}

func TestNextPrev(t *testing.T) {
	tb := newTabBar(200)
	tb.SetTabs([]string{"host1", "host2", "host3"})

	// Start at diff (index 0).
	if tb.ActiveIndex() != 0 {
		t.Fatalf("expected index 0, got %d", tb.ActiveIndex())
	}

	tb.Next()
	if tb.ActiveIndex() != 1 {
		t.Fatalf("expected index 1 after Next, got %d", tb.ActiveIndex())
	}

	tb.Next()
	tb.Next()
	if tb.ActiveIndex() != 3 {
		t.Fatalf("expected index 3, got %d", tb.ActiveIndex())
	}

	// Wrap around.
	tb.Next()
	if tb.ActiveIndex() != 0 {
		t.Fatalf("expected wrap to 0, got %d", tb.ActiveIndex())
	}

	// Prev wraps backwards.
	tb.Prev()
	if tb.ActiveIndex() != 3 {
		t.Fatalf("expected wrap to 3 on Prev, got %d", tb.ActiveIndex())
	}
}

func TestSetActive(t *testing.T) {
	tb := newTabBar(200)
	tb.SetTabs([]string{"host1", "host2"})

	// Jump to index 2.
	tb.SetActive(2)
	if tb.ActiveIndex() != 2 {
		t.Fatalf("expected index 2, got %d", tb.ActiveIndex())
	}

	// Clamp to max.
	tb.SetActive(99)
	if tb.ActiveIndex() != 2 {
		t.Fatalf("expected clamped to 2, got %d", tb.ActiveIndex())
	}

	// Clamp to 0.
	tb.SetActive(-5)
	if tb.ActiveIndex() != 0 {
		t.Fatalf("expected clamped to 0, got %d", tb.ActiveIndex())
	}
}

func TestSetActiveByID(t *testing.T) {
	tb := newTabBar(200)
	tb.SetTabs([]string{"host1", "host2"})

	found := tb.SetActiveByID("host2")
	if !found {
		t.Fatal("expected to find host2")
	}
	if tb.ActiveID() != "host2" {
		t.Fatalf("expected active 'host2', got %q", tb.ActiveID())
	}

	found = tb.SetActiveByID("nonexistent")
	if found {
		t.Fatal("expected not to find nonexistent host")
	}
	// Active should remain unchanged.
	if tb.ActiveID() != "host2" {
		t.Fatalf("expected active to remain 'host2', got %q", tb.ActiveID())
	}
}

func TestTruncLabel(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		expect string
	}{
		{"short", 10, "short"},
		{"exactly-sixteen", 16, "exactly-sixteen"},
		{"this-is-a-very-long-hostname", 16, "this-is-a-ver..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"ab", 1, "a"},
	}

	for _, tt := range tests {
		got := truncLabel(tt.input, tt.maxLen)
		if got != tt.expect {
			t.Errorf("truncLabel(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expect)
		}
	}
}

func TestEmptyTabBar(t *testing.T) {
	tb := tabBar{width: 80}
	// No tabs at all.
	if tb.ActiveID() != "diff" {
		t.Fatalf("expected fallback 'diff', got %q", tb.ActiveID())
	}

	// Next/Prev should not panic.
	tb.Next()
	tb.Prev()
	tb.SetActive(0)
}

func TestViewRenders(t *testing.T) {
	tb := newTabBar(80)
	tb.SetTabs([]string{"host1", "host2"})

	view := tb.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestViewWithZeroWidth(t *testing.T) {
	tb := newTabBar(0)
	view := tb.View()
	if view != "" {
		t.Fatalf("expected empty view for zero width, got %q", view)
	}
}

func TestLastTabExactFitNoPhantomArrow(t *testing.T) {
	// Regression: when the last tab fits exactly, the right-arrow reservation
	// should not kick in and hide the tab or show a misleading ▶.
	tb := newTabBar(300) // wide enough for all tabs
	tb.SetTabs([]string{"a", "b", "c"})

	view := tb.View()

	// All tabs should be visible; no right arrow.
	if strings.Contains(view, "▶") {
		t.Fatal("unexpected right arrow when all tabs fit")
	}

	// The last tab should be visible.
	if !strings.Contains(view, "c") {
		t.Fatal("last tab 'c' should be visible when width is sufficient")
	}
}

func TestLastTabExactFitOnActiveTab(t *testing.T) {
	// When the active tab is the last one, ensureVisible should not
	// push the offset forward due to right-arrow reservation.
	tb := newTabBar(300)
	tb.SetTabs([]string{"host1", "host2"})
	tb.SetActive(2) // last tab (host2)

	if tb.offset != 0 {
		t.Fatalf("expected offset 0 for last active tab with sufficient width, got %d", tb.offset)
	}
}
