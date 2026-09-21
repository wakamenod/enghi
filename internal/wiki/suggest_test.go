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

	// 前方一致が先、次に短い順。
	if got := pick("emacs"); len(got) != 2 || got[0] != "Emacs" || got[1] != "Emacs Lisp" {
		t.Fatalf("前方一致が先に並ぶこと: %v", got)
	}
	// 大小を区別しない。
	if got := pick("EMACS LISP"); len(got) != 1 || got[0] != "Emacs Lisp" {
		t.Fatalf("大小を無視すること: %v", got)
	}
	// 日本語の部分一致。
	if got := pick("移転"); len(got) != 1 || got[0] != "オフィス移転" {
		t.Fatalf("部分一致すること: %v", got)
	}
	// 別名も候補に出る。挿入する文字列は別名そのもので、正式名に置き換えない(DESIGN 2.5)。
	got, err := s.SuggestTitles(ctx, "イーマ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "イーマックス" || !got[0].IsAlias ||
		got[0].Canonical != "Emacs" || got[0].Slug != emacs.Slug {
		t.Fatalf("別名の候補: %+v", got)
	}
	// q が空なら最近更新されたページ(正式名のみ)。
	if got := pick(""); len(got) != 3 {
		t.Fatalf("q が空なら全ページの正式名: %v", got)
	}
	// LIKE のワイルドカードは無効化されていること。
	if got := pick("%"); len(got) != 0 {
		t.Fatalf("%% が全件に当たってはいけない: %v", got)
	}
	// limit が効くこと。
	if got := pick(""); len(got) == 0 {
		t.Fatal("空クエリで 0 件")
	}
	if got, _ := s.SuggestTitles(ctx, "", 1); len(got) != 1 {
		t.Fatalf("limit=1 で %d 件", len(got))
	}
}
