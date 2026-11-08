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
