package wiki_test

import (
	"context"
	"errors"
	"path/filepath"
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

// リネームの往復。DESIGN 2.5 が「UPSERT が無いと必ず失敗する」と名指ししている経路。
func TestRenameAndRevert(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "Emacs", "本文")

	p2, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "GNU Emacs", Body: "本文", Version: p.Version})
	if err != nil {
		t.Fatalf("リネーム: %v", err)
	}
	if p2.Title != "GNU Emacs" {
		t.Fatalf("title = %q, want GNU Emacs", p2.Title)
	}
	if n := canonicalCount(t, db, p.ID); n != 1 {
		t.Fatalf("リネーム後の正式名が %d 件", n)
	}

	// **元の名前に戻す。** 旧名は自分自身の別名として既に存在するので、
	// 素の INSERT ではここで必ず UNIQUE constraint failed になる。
	p3, err := s.Update(ctx, p2.Slug, wiki.UpdateInput{Title: "Emacs", Body: "本文", Version: p2.Version})
	if err != nil {
		t.Fatalf("元のタイトルに戻す: %v", err)
	}
	if p3.Title != "Emacs" {
		t.Fatalf("title = %q, want Emacs", p3.Title)
	}
	if n := canonicalCount(t, db, p.ID); n != 1 {
		t.Fatalf("戻した後の正式名が %d 件", n)
	}
	if probs, err := store.Doctor(ctx, db); err != nil || len(probs) != 0 {
		t.Fatalf("doctor: %v %v", probs, err)
	}
}

// 他ページのタイトルと衝突するリネームは、**降格より前に**弾かれなければならない。
// 降格が先に走ると、失敗したページが正式名ゼロのまま残る。
func TestRenameConflictLeavesCanonicalIntact(t *testing.T) {
	s, db := newSvc(t)
	ctx := context.Background()
	a := mustCreate(t, s, "Emacs", "A")
	b := mustCreate(t, s, "Vim", "B")

	_, err := s.Update(ctx, b.Slug, wiki.UpdateInput{Title: "Emacs", Body: "B", Version: b.Version})
	var tc *wiki.TitleConflictError
	if !errors.As(err, &tc) {
		t.Fatalf("title_conflict を期待したが %v", err)
	}
	if tc.Conflicting.ID != a.ID {
		// 「どのページと衝突したのか」が分からないと利用者は辿れない(DESIGN 4.2)
		t.Fatalf("衝突相手が返っていない: %+v", tc.Conflicting)
	}
	if n := canonicalCount(t, db, b.ID); n != 1 {
		t.Fatalf("失敗したのに正式名が %d 件になった(降格が先に走っている)", n)
	}
	if probs, _ := store.Doctor(ctx, db); len(probs) != 0 {
		t.Fatalf("doctor: %v", probs)
	}
}

// 別名でも衝突すること(名前空間は page_titles 1枚で表現されている)。
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
		t.Fatalf("別名と同名のページ作成は title_conflict になるはず: %v", err)
	}
}

// タイトルは COLLATE NOCASE。[[emacs]] が「Emacs」に解決されること。
func TestTitleIsCaseInsensitive(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	mustCreate(t, s, "Emacs", "A")

	if _, err := s.Create(ctx, wiki.CreateInput{Title: "emacs"}); err == nil {
		t.Fatal("大小違いの同名ページが作れてしまった")
	}
	p, err := s.ByTitle(ctx, "emacs")
	if err != nil || p.Title != "Emacs" {
		t.Fatalf("ByTitle(emacs) = %v, %v", p, err)
	}
	// 日本語は NOCASE の影響を受けない
	mustCreate(t, s, "ハハ", "x")
	if _, err := s.Create(ctx, wiki.CreateInput{Title: "パパ"}); err != nil {
		t.Fatalf("「ハハ」と「パパ」は別物のはず: %v", err)
	}
}

// 保存のたびにリンク行が増殖しないこと(DESIGN 2.1)。
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
		t.Fatalf("リンク行 = %d, want 2", got)
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
		t.Fatalf("再保存後のリンク行 = %d, want 2(増殖している)", got)
	}
}

// 未解決リンクは、その名前のページが作られた時点で解決される。
func TestUnresolvedLinkResolvesOnCreate(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	src := mustCreate(t, s, "起点", "[[まだ無い記事]]")

	links, _ := s.Links(ctx, src.ID)
	if len(links) != 1 || links[0].Resolved {
		t.Fatalf("最初は未解決のはず: %+v", links)
	}
	un, _ := s.UnresolvedLinks(ctx, 10)
	if len(un) != 1 || un[0].Title != "まだ無い記事" {
		t.Fatalf("未解決リンク一覧: %+v", un)
	}

	dst := mustCreate(t, s, "まだ無い記事", "できた")
	links, _ = s.Links(ctx, src.ID)
	if len(links) != 1 || !links[0].Resolved || *links[0].PageID != dst.ID {
		t.Fatalf("作成後に解決されていない: %+v", links)
	}
	back, _ := s.Backlinks(ctx, dst.ID)
	if len(back) != 1 || back[0].ID != src.ID {
		t.Fatalf("バックリンク: %+v", back)
	}
}

// ページ削除時: そこを指す行は未解決に落とし、そのページ発の行は消す(DESIGN 2.1)。
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
		t.Fatalf("参照元のリンクは未解決リンクとして残るはず: %+v", links)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM links WHERE src_kind='page' AND src_id=?`, dst.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("削除したページ発のリンクが %d 行残っている", n)
	}
}

// version はタグだけの変更でも上がる。リビジョンは本文/タイトルが変わったときだけ(DESIGN 4.2)。
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
		t.Fatalf("タグ変更で version が上がっていない: %d → %d", p.Version, p2.Version)
	}
	revs1, _ := s.Revisions(ctx, p.ID)
	if len(revs1) != len(revs0) {
		t.Fatalf("タグだけの変更でリビジョンが作られた: %d → %d", len(revs0), len(revs1))
	}
}

// 10 分以内の連続保存はリビジョンを上書きする。差分の大小は見ない(DESIGN 4.2)。
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
		t.Fatalf("10 分以内の連続保存は1件に圧縮されるはず: %d 件", len(revs))
	}
	full, err := s.Revision(ctx, revs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Body != "本文 e" {
		t.Fatalf("最新の内容で上書きされていない: %q", full.Body)
	}
}

func TestVersionConflict(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p := mustCreate(t, s, "記事", "本文")

	if _, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "記事", Body: "A", Version: p.Version}); err != nil {
		t.Fatal(err)
	}
	// 古い version での書き戻し
	_, err := s.Update(ctx, p.Slug, wiki.UpdateInput{Title: "記事", Body: "B", Version: p.Version})
	var vc *wiki.VersionConflictError
	if !errors.As(err, &vc) {
		t.Fatalf("version_conflict を期待したが %v", err)
	}
	if vc.Current.Body != "A" {
		t.Fatalf("現行データが返っていない: %+v", vc.Current)
	}
}

func TestParseLinksIgnoresCode(t *testing.T) {
	body := "[[本物]] `[[インラインコード]]`\n```\n[[コードブロック]]\n```\n[[本物2|表示名]]"
	got := wiki.ParseLinks(body)
	if len(got) != 2 {
		t.Fatalf("リンク数 = %d, want 2: %+v", len(got), got)
	}
	if got[1].Title != "本物2" || got[1].Label != "表示名" {
		t.Fatalf("ラベル付きリンク: %+v", got[1])
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
