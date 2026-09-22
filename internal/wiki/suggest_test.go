package wiki_test

import (
	"context"
	"testing"
)

func TestSuggestTitles(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()

	emacs := mustCreate(t, s, "Emacs", "本文")
	mustCreate(t, s, "Emacs Lisp", "本文")
	mustCreate(t, s, "オフィス移転", "本文")
	if err := s.AddAlias(ctx, emacs.ID, "イーマックス"); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}

	pick := func(q string) []string {
		t.Helper()
		got, err := s.SuggestTitles(ctx, q, 10)
		if err != nil {
			t.Fatalf("SuggestTitles(%q): %v", q, err)
		}
		out := make([]string, len(got))
		for i, v := range got {
			out[i] = v.Title
		}
		return out
	}

	// Prefix matches first, then shorter titles.
	if got := pick("emacs"); len(got) != 2 || got[0] != "Emacs" || got[1] != "Emacs Lisp" {
		t.Fatalf("prefix matches must come first: %v", got)
	}
	// Case-insensitive.
	if got := pick("EMACS LISP"); len(got) != 1 || got[0] != "Emacs Lisp" {
		t.Fatalf("case must be ignored: %v", got)
	}
	// Substring match in Japanese.
	if got := pick("移転"); len(got) != 1 || got[0] != "オフィス移転" {
		t.Fatalf("substring matching must work: %v", got)
	}
	// Aliases are offered too. The inserted string is the alias itself, never
	// replaced by the canonical title (DESIGN 2.5).
	got, err := s.SuggestTitles(ctx, "イーマ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "イーマックス" || !got[0].IsAlias ||
		got[0].Canonical != "Emacs" || got[0].Slug != emacs.Slug {
		t.Fatalf("alias candidate: %+v", got)
	}
	// An empty q returns recently updated pages, canonical titles only.
	if got := pick(""); len(got) != 3 {
		t.Fatalf("an empty q must list every canonical title: %v", got)
	}
	// LIKE wildcards must be neutralized.
	if got := pick("%"); len(got) != 0 {
		t.Fatalf("%% must not match everything: %v", got)
	}
	// limit must be honoured.
	if got := pick(""); len(got) == 0 {
		t.Fatal("an empty query returned nothing")
	}
	if got, _ := s.SuggestTitles(ctx, "", 1); len(got) != 1 {
		t.Fatalf("limit=1 returned %d", len(got))
	}
}
