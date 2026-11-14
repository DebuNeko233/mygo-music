package lyrics

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const unavailable LineKind = Instrumental + 1

// Credit aliases share a key for deduplication across LRC and YRC.
var creditFields = func() map[string]string {
	fields := make(map[string]string)
	for _, group := range []string{
		"作词|作词人|词|作詞|作詩|Lyrics|Lyricist|Lyrics by",
		"作曲|作曲人|曲|Music|Composer|Composed by",
		"编曲|编曲人|編曲|アレンジ|Arrangement|Arranger",
		"作曲/编曲|作曲/編曲|Music & Arrangement",
		"作词/作曲|作詞/作曲|Lyrics & Music",
		"制作人|Producer|Producers|プロデューサー",
		"制作|製作", "制作协力|制作協力", "音乐制作|音楽制作", "监制|監制",
		"音乐监制|Music Supervisor",
		"混音|混音师|混音工程师|混音工程|Mixing|Mixing Engineer|ミックス",
		"混音助理|Mixing Assistant",
		"混音室", "编曲/混音",
		"录音|録音|Recording", "录音师|录音工程师|Recording Engineer", "录音棚|录音室|Recording Studio", "录音助理",
		"母带|母带制作|母带处理|母带工程师|Mastering|Mastering Engineer|マスタリング",
		"混音/母带|Mixing/Mastering Engineer",
		"和声|和音|Background Vocals|コーラス", "和声编写", "吉他|Guitar|Guitars|ギター",
		"主音吉他|Lead Guitar", "节奏吉他|Rhythm Guitar", "电吉他",
		"贝斯|Bass|ベース", "鼓|Drums|ドラム", "鼓编程", "打击乐|Percussion",
		"键盘|Keyboards|キーボード", "风琴|Organ", "弦乐|Strings", "弦乐编写", "小提琴", "大提琴", "萨克斯",
		"钢琴|Piano", "笛子|Dizi", "古筝|Guzheng", "琵琶|Pipa", "筝|Koto",
		"哨笛|Tin Whistle", "三味线|Shamisen",
		"指挥|Conductor", "乐队|Orchestra", "乐队配器|Orchestrator",
		"工程师|ProTools Operator|Engineering", "声音工程师|音频工程师|Sound Engineer",
		"舞台监督|Orchestra Stage Manager", "乐团经理|Orchestra Manager", "录音现场指导|Scoring Sessions Director",
		"重新编排", "乐器录制", "制谱|Music Copyist", "人声|Voice", "主人声",
		"演唱|歌唱|Vocals|Vocal|ボーカル", "原唱", "翻唱", "Vocal & Lyrics", "Lead & Background Vocals",
		"人声录音", "人声录音室", "人声编辑", "配唱制作", "附加制作", "音乐总监",
		"出品|出品人|Produced by|Produced", "联合出品", "联合制作|Co-produced by", "发行",
		"企划|企画", "统筹|项目统筹", "推广", "营销推广", "封面|封面设计", "曲绘", "合作联系",
		"OP", "SP", "OP/SP", "曲版权管理方", "词版权管理方", "录音作品及MV版权", "P-Line", "C-Line",
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
	"instrumental": Instrumental, "instrumentalonly": Instrumental, "インストゥルメンタル": Instrumental, "歌詞なし": unavailable,
	"暂无歌词": unavailable, "暫無歌詞": unavailable,
}

func compactField(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		if r == '／' {
			return '/'
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
	_, separatorWidth := utf8.DecodeRuneInString(text[i:])
	value = strings.TrimSpace(text[i+separatorWidth:])
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
