package yomi

import "testing"

func TestReadings(t *testing.T) {
	r, err := NewReader()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ title, reading, pron string }{
		{"検索の設計", "けんさくのせっけい", "けんさくのせっけい"},
		{"会議の準備", "かいぎのじゅんび", "かいぎのじゅんび"},
		// Long vowels: the reading spells them out, the pronunciation uses ー,
		// which is dropped
		{"東京", "とうきょう", "ときょ"},
		{"今日は", "きょうは", "きょわ"},
		// Katakana words, with ー dropped
		{"サーバーの再起動", "さばのさいきどう", "さばのさいきど"},
		// An unknown ASCII word keeps its surface, lower-cased
		{"Mermaid対応", "mermaidたいおう", "mermaidたいお"},
		// Symbols and spaces are dropped
		{"検索（設計）", "けんさくせっけい", "けんさくせっけい"},
		{"Go 1.27 の移行", "go127のいこう", "go127のいこ"},
	} {
		rd, pr := r.Readings(c.title)
		if rd != c.reading || pr != c.pron {
			t.Errorf("Readings(%q) = (%q, %q), want (%q, %q)", c.title, rd, pr, c.reading, c.pron)
		}
	}
}

func TestToKana(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"kensaku", "けんさく", true},
		{"KAIGI", "かいぎ", true},
		// Hepburn and Kunrei
		{"shinbunn", "しんぶん", true},
		{"sinbunn", "しんぶん", true},
		{"chizu", "ちず", true},
		{"tizu", "ちず", true},
		{"tsukue", "つくえ", true},
		{"tukue", "つくえ", true},
		{"fune", "ふね", true},
		{"hune", "ふね", true},
		{"jisho", "じしょ", true},
		{"zisyo", "じしょ", true},
		{"jya", "じゃ", true},
		{"zya", "じゃ", true},
		{"cha", "ちゃ", true},
		{"tya", "ちゃ", true},
		// っ
		{"sekkei", "せっけい", true},
		{"matcha", "まっちゃ", true},
		{"kitte", "きって", true},
		// ん
		{"kanji", "かんじ", true},
		{"kannji", "かんじ", true},
		{"kan'i", "かんい", true},
		{"konnichiha", "こんにちは", true},
		{"onna", "おんな", true},
		{"kenn", "けん", true},
		// Small kana
		{"xa", "ぁ", true},
		{"ltu", "っ", true},
		{"xtsu", "っ", true},
		{"lya", "ゃ", true},
		// ー is dropped, as it is from the readings
		{"sa-ba-", "さば", true},
		// The unfinished tail is dropped
		{"kens", "けん", true},
		{"kensh", "けん", true},
		{"kan", "か", true},
		{"kany", "か", true},
		{"sekk", "せっ", true},
		// Not romaji
		{"test", "", false},
		{"http", "", false},
		{"xyz", "", false},
		{"C++", "", false},
		{"kensaku sekkei", "", false},
		{"2024", "", false},
		{"", "", false},
		{"n", "", false},
	} {
		got, ok := ToKana(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ToKana(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
