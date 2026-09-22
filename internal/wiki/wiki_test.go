package wiki_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakamenod/enghi/internal/store"
	"github.com/wakamenod/enghi/internal/wiki"
)

func newSvc(t *testing.T) (*wiki.Service, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return wiki.New(db, 10), db
}

func mustCreate(t *testing.T, s *wiki.Service, title, body string, tags ...string) *wiki.Page {
	t.Helper()
	p, err := s.Create(context.Background(), wiki.CreateInput{Title: title, Body: body, Tags: tags})
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return p
}

func canonicalCount(t *testing.T, db *store.DB, pageID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM page_titles WHERE page_id = ? AND is_canonical = 1`, pageID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Rename and rename back. DESIGN 2.5 names this as the path that always fails
// without the UPSERT.
func TestRenameAndRevert(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "Emacs", "本文")

	p2, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "GNU Emacs", Body: "本文", Version: p.Version})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if p2.Title != "GNU Emacs" {
		t.Fatalf("title = %q, want GNU Emacs", p2.Title)
	}
	if n := canonicalCount(t, db, p.ID); n != 1 {
		t.Fatalf("canonical titles after rename = %d", n)
	}

	// **Rename back.** The old name already exists as this page's own alias, so
	// a plain INSERT is guaranteed to hit UNIQUE constraint failed here.
	p3, err := s.Update(ctx, p2.Slug, wiki.UpdateInput{Title: "Emacs", Body: "本文", Version: p2.Version})
	if err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if p3.Title != "Emacs" {
		t.Fatalf("title = %q, want Emacs", p3.Title)
	}
	if n := canonicalCount(t, db, p.ID); n != 1 {
		t.Fatalf("canonical titles after renaming back = %d", n)
	}
	if probs, err := store.Doctor(ctx, db); err != nil || len(probs) != 0 {
		t.Fatalf("doctor: %v %v", probs, err)
	}
}

// A rename colliding with another page's title must be rejected **before** the
// demotion. If demotion runs first, the failed page is left with no canonical
// title at all.
func TestRenameConflictLeavesCanonicalIntact(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	a := mustCreate(t, s, "Emacs", "A")
	b := mustCreate(t, s, "Vim", "B")

	_, err := s.Update(ctx, b.Slug, wiki.UpdateInput{Title: "Emacs", Body: "B", Version: b.Version})
	var tc *wiki.TitleConflictError
	if !errors.As(err, &tc) {
		t.Fatalf("expected title_conflict, got %v", err)
	}
	if tc.Conflicting.ID != a.ID {
		// Without "which page did it collide with", the user cannot follow up
		// (DESIGN 4.2)
		t.Fatalf("the conflicting page was not returned: %+v", tc.Conflicting)
	}
	if n := canonicalCount(t, db, b.ID); n != 1 {
		t.Fatalf("after a failure there are %d canonical titles (demotion ran first)", n)
	}
	if probs, _ := store.Doctor(ctx, db); len(probs) != 0 {
		t.Fatalf("doctor: %v", probs)
	}
}

// Aliases collide too: the namespace is one table, page_titles.
func TestAliasOccupiesNamespace(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	a := mustCreate(t, s, "Emacs", "A")
	if err := s.AddAlias(ctx, a.ID, "イーマックス"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(ctx, wiki.CreateInput{Title: "イーマックス"})
	var tc *wiki.TitleConflictError
	if !errors.As(err, &tc) {
		t.Fatalf("creating a page named like an alias must be title_conflict: %v", err)
	}
}

// Titles are COLLATE NOCASE, so [[emacs]] resolves to "Emacs".
func TestTitleIsCaseInsensitive(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	mustCreate(t, s, "Emacs", "A")

	if _, err := s.Create(ctx, wiki.CreateInput{Title: "emacs"}); err == nil {
		t.Fatal("a page differing only in case was created")
	}
	p, err := s.ByTitle(ctx, "emacs")
	if err != nil || p.Title != "Emacs" {
		t.Fatalf("ByTitle(emacs) = %v, %v", p, err)
	}
	// Japanese is unaffected by NOCASE
	mustCreate(t, s, "ハハ", "x")
	if _, err := s.Create(ctx, wiki.CreateInput{Title: "パパ"}); err != nil {
		t.Fatalf("「ハハ」 and 「パパ」 must be different: %v", err)
	}
}

// Link rows must not multiply on every save (DESIGN 2.1).
func TestLinksDoNotMultiplyOnResave(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "起点", "[[あちら]] と [[あちら]] と [[こちら]]")

	count := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM links WHERE src_kind='page' AND src_id=?`, p.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(); got != 2 {
		t.Fatalf("link rows = %d, want 2", got)
	}
	cur := p
	for i := 0; i < 3; i++ {
		var err error
		cur, err = s.Update(ctx, cur.Slug, wiki.UpdateInput{
			Title: cur.Title, Body: "[[あちら]] と [[あちら]] と [[こちら]] " + string(rune('a'+i)),
			Version: cur.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := count(); got != 2 {
		t.Fatalf("link rows after re-saving = %d, want 2 (they are multiplying)", got)
	}
}

// An unresolved link resolves the moment a page with that name is created.
func TestUnresolvedLinkResolvesOnCreate(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	src := mustCreate(t, s, "起点", "[[まだ無い記事]]")

	links, _ := s.Links(ctx, src.ID)
	if len(links) != 1 || links[0].Resolved {
		t.Fatalf("it should start out unresolved: %+v", links)
	}
	un, _ := s.UnresolvedLinks(ctx, 10)
	if len(un) != 1 || un[0].Title != "まだ無い記事" {
		t.Fatalf("unresolved link list: %+v", un)
	}

	dst := mustCreate(t, s, "まだ無い記事", "できた")
	links, _ = s.Links(ctx, src.ID)
	if len(links) != 1 || !links[0].Resolved || *links[0].PageID != dst.ID {
		t.Fatalf("still unresolved after creation: %+v", links)
	}
	back, _ := s.Backlinks(ctx, dst.ID)
	if len(back) != 1 || back[0].ID != src.ID {
		t.Fatalf("backlinks: %+v", back)
	}
}

// On delete: rows pointing at the page are demoted to unresolved, rows coming
// from it are deleted (DESIGN 2.1).
func TestDeleteDemotesIncomingLinks(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	dst := mustCreate(t, s, "消される記事", "[[どこか]]")
	src := mustCreate(t, s, "参照元", "[[消される記事]]")

	if err := s.Delete(ctx, dst.Slug); err != nil {
		t.Fatal(err)
	}
	links, _ := s.Links(ctx, src.ID)
	if len(links) != 1 || links[0].Resolved {
		t.Fatalf("the referrer's link must remain as an unresolved link: %+v", links)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM links WHERE src_kind='page' AND src_id=?`, dst.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d link row(s) from the deleted page are still there", n)
	}
}

// version goes up even for a tag-only change; a revision is written only when
// the body or title changes (DESIGN 4.2).
func TestTagOnlyChangeBumpsVersionButMakesNoRevision(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "記事", "本文", "仕事")

	revs0, _ := s.Revisions(ctx, p.ID)
	p2, err := s.Update(ctx, p.Slug, wiki.UpdateInput{
		Title: "記事", Body: "本文", Tags: []string{"仕事", "健康"}, Version: p.Version})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Version != p.Version+1 {
		t.Fatalf("version did not go up on a tag change: %d -> %d", p.Version, p2.Version)
	}
	revs1, _ := s.Revisions(ctx, p.ID)
	if len(revs1) != len(revs0) {
		t.Fatalf("a tag-only change created a revision: %d -> %d", len(revs0), len(revs1))
	}
}

// Saves within 10 minutes overwrite the revision, whatever the size of the
// change (DESIGN 4.2).
func TestRevisionCompaction(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "記事", "v1")

	cur := p
	for i := 0; i < 5; i++ {
		var err error
		cur, err = s.Update(ctx, cur.Slug, wiki.UpdateInput{
			Title: "記事", Body: "本文 " + string(rune('a'+i)), Version: cur.Version})
		if err != nil {
			t.Fatal(err)
		}
	}
	revs, _ := s.Revisions(ctx, p.ID)
	if len(revs) != 1 {
		t.Fatalf("saves within 10 minutes must compact into one: %d revisions", len(revs))
	}
	full, err := s.Revision(ctx, revs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Body != "本文 e" {
		t.Fatalf("not overwritten with the latest content: %q", full.Body)
	}
}

func TestVersionConflict(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "記事", "本文")

	if _, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "記事", Body: "A", Version: p.Version}); err != nil {
		t.Fatal(err)
	}
	// Writing back with a stale version
	_, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "記事", Body: "B", Version: p.Version})
	var vc *wiki.VersionConflictError
	if !errors.As(err, &vc) {
		t.Fatalf("expected version_conflict, got %v", err)
	}
	if vc.Current.Body != "A" {
		t.Fatalf("the current data was not returned: %+v", vc.Current)
	}
}

func TestParseLinksIgnoresCode(t *testing.T) {
	body := "[[本物]] `[[インラインコード]]`\n```\n[[コードブロック]]\n```\n[[本物2|表示名]]"
	got := wiki.ParseLinks(body)
	if len(got) != 2 {
		t.Fatalf("links = %d, want 2: %+v", len(got), got)
	}
	if got[1].Title != "本物2" || got[1].Label != "表示名" {
		t.Fatalf("labelled link: %+v", got[1])
	}
}

func TestSlugifyNormalizesToLower(t *testing.T) {
	cases := map[string]string{
		"Emacs Lisp":     "emacs-lisp",
		"../../etc/pass": "etc-pass",
		"  .hidden":      "hidden",
		"日本語のタイトル":       "日本語のタイトル",
	}
	for in, want := range cases {
		if got := wiki.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBigrams(t *testing.T) {
	if got := wiki.Bigrams("オフィス移転"); got != "オフ フィ ィス ス移 移転" {
		t.Fatalf("Bigrams = %q", got)
	}
	if got := wiki.Bigrams("Go"); got != "go" {
		t.Fatalf("Bigrams(Go) = %q", got)
	}
}

// A single newline inside a paragraph must render as a line break
// (html.WithHardWraps). CommonMark's default turns it into a space, which shows
// up as a visible gap mid-sentence in Japanese text.
func TestRenderHardWraps(t *testing.T) {
	r := wiki.NewRenderer(func(string) (string, bool) { return "", false })

	html, err := r.Render("一行目\n二行目")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<br>") {
		t.Fatalf("the newline did not become <br>: %q", html)
	}

	// Paragraphs, lists, code blocks and tables must be unaffected
	cases := []struct {
		name, src, want string
	}{
		{"paragraph", "一段落目\n\n二段落目", "<p>二段落目</p>"},
		{"list", "- 一つ目\n- 二つ目", "<li>二つ目</li>"},
		{"code block", "```\nコード内の\n改行\n```", "<pre>"},
		{"table", "| 表 | も |\n|---|---|\n| 壊れ | ない |", "<table>"},
	}
	for _, c := range cases {
		got, err := r.Render(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q is missing\n%s", c.name, c.want, got)
		}
	}
	// Newlines inside a code block must not become <br>
	code, _ := r.Render("```\nコード内の\n改行\n```")
	if strings.Contains(code, "<br>") {
		t.Errorf("<br> ended up inside a code block:\n%s", code)
	}
}

// macOS hands us Japanese in NFD (decomposed form) through many paths.
// "ビ" written as "ヒ" plus a combining dakuten must still be the same page.
//
// Without normalizing, an identical-looking title cannot be found, and two
// pages with the same name can be created.
func TestUnicodeNormalization(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()

	nfc := "ビデオリンク"                                           // composed form
	nfd := "\u30d2\u3099\u30c6\u3099\u30aa\u30ea\u30f3\u30af" // decomposed form of ビデオリンク

	if nfc == nfd {
		t.Fatal("the premise of this test is broken: NFC and NFD are the same string")
	}

	p := mustCreate(t, s, nfc, "本文")

	// Looking it up in NFD must reach the same page
	got, err := s.ByTitle(ctx, nfd)
	if err != nil {
		t.Fatalf("cannot look it up by the NFD title: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("a different page came back: %d != %d", got.ID, p.ID)
	}

	// The slug must match too: identical-looking means the same URL
	if s2 := wiki.Slugify(nfd); s2 != wiki.Slugify(nfc) {
		t.Errorf("slugs do not match: %q != %q", s2, wiki.Slugify(nfc))
	}
	if _, err := s.BySlug(ctx, wiki.Slugify(nfd)); err != nil {
		t.Errorf("cannot look it up by the NFD-derived slug: %v", err)
	}

	// Creating the same page in NFD must collide, preventing a duplicate
	if _, err := s.Create(ctx, wiki.CreateInput{Title: nfd}); err == nil {
		t.Error("a duplicate page was created in NFD")
	}

	// [[NFD]] in a body must resolve to the NFC page
	src := mustCreate(t, s, "参照元", "[["+nfd+"]] を参照")
	links, _ := s.Links(ctx, src.ID)
	if len(links) != 1 || !links[0].Resolved {
		t.Fatalf("the NFD wikilink did not resolve: %+v", links)
	}
	if *links[0].PageID != p.ID {
		t.Errorf("it resolved to a different page")
	}
}

// Tags are normalized as well.
func TestTagNormalization(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	nfd := "\u30d2\u3099\u30c6\u3099\u30aa" // decomposed form of ビデオ

	mustCreate(t, s, "記事A", "本文", "ビデオ")
	mustCreate(t, s, "記事B", "本文", nfd)

	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		names := []string{}
		for _, tc := range tags {
			names = append(names, tc.Name)
		}
		t.Fatalf("one identical-looking tag split into %d: %v", len(tags), names)
	}
	if tags[0].Count != 2 {
		t.Errorf("tag count = %d, want 2", tags[0].Count)
	}
}
