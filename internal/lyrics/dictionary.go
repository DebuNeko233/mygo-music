package lyrics

import (
	"regexp"
	"strings"
	"unicode"
)

const unavailable LineKind = Instrumental + 1

var lyricNotices = map[string]LineKind{
	"纯音乐": Instrumental, "纯音乐请欣赏": Instrumental, "请欣赏纯音乐": Instrumental,
	"純音樂": Instrumental, "純音樂請欣賞": Instrumental, "請欣賞純音樂": Instrumental,
	"instrumental": Instrumental, "instrumentalonly": Instrumental, "インストゥルメンタル": Instrumental, "歌詞なし": unavailable,
	UnavailableText: unavailable, "暫無歌詞": unavailable,
}

var contactAddress = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

func lineKind(text string) LineKind {
	if IsCredit(text) {
		return Credit
	}
	return lyricNoticeKind(text)
}

func lyricNoticeKind(text string) LineKind {
	notice := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || r == '~' || r == '～' {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
	if kind, ok := lyricNotices[notice]; ok {
		return kind
	}
	if strings.TrimRight(strings.TrimSpace(text), ":：") == "合作联系" {
		return Credit
	}
	if contactAddress.MatchString(text) ||
		strings.HasPrefix(text, "【未经允许，") && strings.HasSuffix(text, "】") {
		return Credit
	}
	return Sung
}

// These prose fragments occur with the instrumental notice in a cached
// release. Outside that context they may be spoken or sung.
var instrumentalNotes = []string{
	"这首歌的名字叫《",
	"如果你喜欢，希望你能点击红心",
	"并能与你的朋友分享，",
}

func isInstrumentalNote(text string) bool {
	for _, prefix := range instrumentalNotes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func metadataKey(text string) string {
	if name, value, ok := creditField(text, true); ok {
		return name + ":" + value
	}
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
