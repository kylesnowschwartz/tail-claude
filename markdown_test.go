package main

import "testing"

// Alternating widths (Claude cards vs user bubbles) must each keep their own
// cached renderer rather than evicting each other on every call.
func TestRenderMarkdownCachesPerWidth(t *testing.T) {
	r := newMdRenderer(true)

	r.renderMarkdown("hello", 80)
	first := r.renderers[80]
	if first == nil {
		t.Fatal("expected renderer cached for width 80")
	}

	r.renderMarkdown("world", 100)
	if r.renderers[100] == nil {
		t.Fatal("expected renderer cached for width 100")
	}
	if r.renderers[80] != first {
		t.Error("width-80 renderer was evicted by a width-100 render")
	}

	r.renderMarkdown("again", 80)
	if r.renderers[80] != first {
		t.Error("width-80 renderer was rebuilt on a repeat render")
	}
}

func TestRenderMarkdownReset(t *testing.T) {
	r := newMdRenderer(true)
	r.renderMarkdown("hello", 80)
	r.renderMarkdown("world", 100)

	r.reset()
	if len(r.renderers) != 0 {
		t.Errorf("expected empty renderer cache after reset, got %d entries", len(r.renderers))
	}
}

func TestRenderMarkdownNonPositiveWidth(t *testing.T) {
	r := newMdRenderer(true)
	if got := r.renderMarkdown("# raw", 0); got != "# raw" {
		t.Errorf("expected passthrough for width 0, got %q", got)
	}
	if len(r.renderers) != 0 {
		t.Error("width <= 0 should not populate the cache")
	}
}
