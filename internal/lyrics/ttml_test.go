package lyrics

import (
	"testing"
	"time"
)

// A native TTML file keeps singing, Ruby, translations and overlapping vocals separate.
func TestTTMLKeepsVoiceAndLanguageTracks(t *testing.T) {
	const raw = `<tt xmlns="http://www.w3.org/ns/ttml" xml:lang="ja"
	xmlns:tts="http://www.w3.org/ns/ttml#styling" xmlns:m="http://www.w3.org/ns/ttml#metadata"
	xmlns:i="http://music.apple.com/lyric-ttml-internal" xmlns:a="http://www.example.com/ns/amll">
	<head><metadata><a:meta key="ncmMusicId" value="42"/><a:meta key="ttmlAuthorGithubLogin" value="author"/>
	<iTunesMetadata><translations><translation xml:lang="zh-Hans" type="subtitle">
	<text for="L1">译文<span m:role="x-bg">和声翻译</span></text>
	</translation></translations></iTunesMetadata></metadata></head>
	<body><div>
	<p begin="01.010" end="00:03" m:agent="v1" i:key="L1">
	<span tts:ruby="container"><span tts:ruby="base">星</span><span tts:ruby="textContainer"><span tts:ruby="text" begin="1.01" end="1.5">ほし</span></span></span>
	<span begin="1.5" end="2.1">を見る</span><span m:role="x-roman">hoshi o miru</span>
	<span m:role="x-bg"><span begin="1.8" end="2.3">ah</span></span>
	</p>
	<p begin="1.9" end="3.1" m:agent="v2"><span begin="1.9" end="3.1">星</span></p>
	<p begin="00:00:04" end="5.0">一行だけ</p>
	</div></body></tt>`
	display, err := ParseTTML([]byte(raw), 42)
	if err != nil {
		t.Fatal(err)
	}
	if display.Source != AMLL || display.Language != "ja" || len(display.Authors) != 1 || len(display.Lines) != 3 {
		t.Fatalf("lost TTML metadata or rows: %+v", display)
	}
	line := display.Lines[0]
	if !line.Timed || line.At != 1010*time.Millisecond || len(line.Words) != 2 || line.Words[0].Ruby != "ほし" ||
		line.Text != "星を見る" || line.Translation != "译文" || line.Roman != "hoshi o miru" || line.Agent != "v1" {
		t.Fatalf("primary voice was flattened: %+v", line)
	}
	if len(line.Background) != 1 || !line.Background[0].Timed || line.Background[0].Translation != "和声翻译" ||
		display.Lines[1].Agent != "v2" || display.Lines[1].At >= line.End || display.Lines[2].Timed {
		t.Fatalf("lost distinct voices or made up word timing: %+v", display.Lines)
	}
	if _, err := ParseTTML([]byte(raw), 43); err == nil {
		t.Fatal("accepted lyrics for a different recording")
	}
}

// Bad word marks leave a valid line, while an error page cannot become lyrics.
func TestTTMLFallsBackToLineTiming(t *testing.T) {
	data := []byte(`<tt><body><p begin="1" end="3"><span begin="2" end="1">Hello</span> <span begin="2" end="3">world</span></p></body></tt>`)
	d, err := ParseTTML(data, 0)
	if err != nil || !d.HasSinging() || d.Lines[0].Timed || d.Lines[0].Text != "Hello world" {
		t.Fatalf("expected readable line without word timing: %+v, %v", d, err)
	}
	if _, err := ParseTTML([]byte("<html>Unavailable</html>"), 0); err == nil {
		t.Fatal("HTML response passed as TTML")
	}
}
