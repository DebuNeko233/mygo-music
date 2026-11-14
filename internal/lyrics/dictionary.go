package lyrics

import (
	"regexp"
	"strings"
	"unicode"
)

const unavailable LineKind = Instrumental + 1

// Credit aliases share a key for deduplication across LRC and YRC.
var creditFields = func() map[string]string {
	fields := make(map[string]string)
	for _, group := range []string{
		"作词|作词人|词|Lyrics|Lyricist",
		"作曲|作曲人|曲|Music|Composer",
		"编曲|编曲人|Arranger",
		"制作人|Producer",
		"制作", "制作协力", "音乐制作", "监制",
		"音乐监制|Music Supervisor",
		"混音|混音师|Mixing Engineer",
		"混音助理|Mixing Assistant",
		"录音", "录音师|Recording Engineer", "录音棚|录音室|Recording Studio", "录音助理",
		"母带|母带制作|母带工程师|Mastering Engineer",
		"混音/母带|Mixing/Mastering Engineer",
		"和声", "吉他", "电吉他", "贝斯", "鼓", "键盘", "弦乐|Strings", "小提琴",
		"钢琴|Piano", "笛子|Dizi", "古筝|Guzheng", "琵琶|Pipa", "筝|Koto",
		"哨笛|Tin Whistle", "三味线|Shamisen",
		"指挥|Conductor", "乐队|Orchestra", "乐队配器|Orchestrator",
		"工程师|ProTools Operator", "声音工程师|Sound Engineer",
		"舞台监督|Orchestra Stage Manager", "乐团经理|Orchestra Manager", "录音现场指导|Scoring Sessions Director",
		"重新编排", "乐器录制", "制谱|Music Copyist", "人声|Voice",
		"出品|出品人|Produced by|Produced", "联合出品", "联合制作|Co-produced by", "发行",
		"企划", "统筹", "推广", "营销推广", "封面|封面设计", "合作联系", "OP", "SP",
	} {
		aliases := strings.Split(group, "|")
		key := aliases[0]
		for _, name := range aliases {
			fields[compactField(name)] = key
			for _, english := range aliases {
				if english[0] < unicode.MaxASCII && name[0] >= unicode.MaxASCII {
					fields[compactField(name+english)] = key
				}
			}
		}
	}
	return fields
}()

var lyricNotices = map[string]LineKind{
	"纯音乐": Instrumental, "纯音乐请欣赏": Instrumental, "请欣赏纯音乐": Instrumental,
	"純音樂": Instrumental, "純音樂請欣賞": Instrumental, "請欣賞純音樂": Instrumental,
	"instrumental": Instrumental,
	"暂无歌词":         unavailable, "暫無歌詞": unavailable,
}

func compactField(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}

func creditField(text string) (name, value string, ok bool) {
	i := strings.IndexAny(text, ":：")
	if i <= 0 {
		return "", "", false
	}
	name, ok = creditFields[compactField(text[:i])]
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text[i:], ":"), "："))
	return name, value, ok && value != ""
}

// IsCredit matches whole field names so a singer's name can label a verse.
func IsCredit(text string) bool {
	_, _, ok := creditField(text)
	return ok
}

var contactAddress = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

func lineKind(text string) LineKind {
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
	if IsCredit(text) || contactAddress.MatchString(text) ||
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
	if name, value, ok := creditField(text); ok {
		return name + ":" + strings.ToLower(strings.Join(strings.Fields(value), " "))
	}
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
