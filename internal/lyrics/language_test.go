package lyrics

import "testing"

// Japanese originals must use Japanese glyphs even when the current line is
// kanji only; translated Japanese and shared punctuation must not change it.
func TestLyricsLanguage(t *testing.T) {
	for _, c := range []struct {
		lines []Line
		want  string
	}{
		{[]Line{{Text: "春日影"}, {Text: "悴んだ心 ふるえる眼差し"}}, "ja"},
		{[]Line{{Text: "カタカナだけ"}}, "ja"},
		{[]Line{{Text: "ｶﾀｶﾅ"}}, "ja"},
		{[]Line{{Text: "春日影"}, {Text: "我想要的只是"}}, ""},
		{[]Line{{Text: "中文原文", Translation: "日本語の翻訳"}}, ""},
		{[]Line{{Text: "Hello ・ world ー"}}, ""},
		{[]Line{{Text: "한글"}}, ""},
		{nil, ""},
	} {
		if got := Lang(c.lines); got != c.want {
			t.Errorf("Lang(%v)=%q, want %q", c.lines, got, c.want)
		}
	}
}

// Font choice uses the script of the originals while leaving ambiguous Han unlabelled.
func TestOriginalScript(t *testing.T) {
	for _, c := range []struct {
		source, translation string
		want                Script
	}{
		{"憧れていた世界へ", "A world of dreams", JapaneseScript},
		{"春日影", "Love you", HanScript},
		{"有你的 Summer", "きみの夏", HanScript},
		{"L’été, we’ll sing", "梦中", LatinScript},
		{"Песня Hello", "", UnknownScript},
		{"한글 漢字", "", UnknownScript},
		{"♪ 2000", "カタカナ", UnknownScript},
	} {
		if got := OriginalScript([]Line{{Text: c.source, Translation: c.translation}}); got != c.want {
			t.Errorf("script of %q = %v, want %v", c.source, got, c.want)
		}
	}
}
