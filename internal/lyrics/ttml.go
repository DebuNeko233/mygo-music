package lyrics

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

const MaxTTMLBytes = 512 << 10

const (
	ttmlNS        = "http://www.w3.org/ns/ttml"
	ttmNS         = ttmlNS + "#metadata"
	ttsNS         = ttmlNS + "#styling"
	itunesNS      = "http://itunes.apple.com/lyric-ttml-extensions"
	appleLyricsNS = "http://music.apple.com/lyric-ttml-internal"
	xmlNS         = "http://www.w3.org/XML/1998/namespace"
)

type ttmlNode struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*ttmlNode
}

func (n *ttmlNode) attr(space, name string) string {
	for _, a := range n.attrs {
		if a.Name.Space == space && a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (n *ttmlNode) visit(fn func(*ttmlNode)) {
	fn(n)
	for _, child := range n.children {
		child.visit(fn)
	}
}

func isTTML(n *ttmlNode, name string) bool {
	return n.name.Local == name && (n.name.Space == ttmlNS || n.name.Space == "")
}

func readTTML(source []byte) (*ttmlNode, error) {
	if len(source) > MaxTTMLBytes {
		return nil, errors.New("TTML is too large")
	}
	d := xml.NewDecoder(strings.NewReader(strings.TrimPrefix(string(source), "\ufeff")))
	var root *ttmlNode
	var stack []*ttmlNode
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, errors.New("unexpected XML directive")
		case xml.StartElement:
			if len(stack) >= 32 {
				return nil, errors.New("TTML is nested too deeply")
			}
			n := &ttmlNode{name: t.Name, attrs: t.Attr}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else {
				if root != nil || !isTTML(n, "tt") {
					return nil, errors.New("expected a TTML document")
				}
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, &ttmlNode{text: string(t)})
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, errors.New("text outside TTML document")
			}
		}
	}
	if root == nil {
		return nil, errors.New("missing TTML document")
	}
	return root, nil
}

func ttmlTime(s string) (time.Duration, bool) {
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, false
	}
	var seconds float64
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789.") != "" ||
			i < len(parts)-1 && strings.Contains(part, ".") {
			return 0, false
		}
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || i > 0 && n >= 60 {
			return 0, false
		}
		seconds = seconds*60 + n
	}
	if seconds > maxLyricTime.Seconds() {
		return 0, false
	}
	return time.Duration(math.Round(seconds * float64(time.Second))), true
}

func (n *ttmlNode) timing() (start, end time.Duration, valid bool) {
	start, okStart := ttmlTime(n.attr("", "begin"))
	end, okEnd := ttmlTime(n.attr("", "end"))
	return start, end, okStart && okEnd && start < end
}

func ttmlText(text string) string {
	if strings.ContainsAny(text, "\r\n") {
		// Ignore indentation between elements while retaining meaningful inline spaces.
		return strings.Join(strings.Fields(text), " ")
	}
	return text
}

func baseText(n *ttmlNode) string {
	var b strings.Builder
	var appendText func(*ttmlNode)
	appendText = func(n *ttmlNode) {
		switch n.attr(ttmNS, "role") {
		case "x-bg", "x-translation", "x-roman":
			return
		}
		switch n.attr(ttsNS, "ruby") {
		case "textContainer", "text":
			return
		}
		if isTTML(n, "br") {
			b.WriteByte(' ')
		}
		b.WriteString(ttmlText(n.text))
		for _, child := range n.children {
			appendText(child)
		}
	}
	appendText(n)
	return b.String()
}

func ttmlWords(n *ttmlNode) []Word {
	var words []Word
	badTiming := false
	var prefix strings.Builder
	add := func(word Word) {
		word.Text = prefix.String() + word.Text
		prefix.Reset()
		words = append(words, word)
	}
	var walk func(*ttmlNode)
	walk = func(n *ttmlNode) {
		switch n.attr(ttmNS, "role") {
		case "x-bg", "x-translation", "x-roman":
			return
		}
		if isTTML(n, "span") {
			if n.attr(ttsNS, "ruby") == "container" {
				var ruby strings.Builder
				var start, end time.Duration
				n.visit(func(child *ttmlNode) {
					if child.attr(ttsNS, "ruby") == "text" {
						for _, part := range child.children {
							ruby.WriteString(baseText(part))
						}
						if at, to, ok := child.timing(); ok {
							if end == 0 {
								start = at
							}
							end = max(end, to)
						}
					}
				})
				if at, to, ok := n.timing(); ok {
					start, end = at, to
				}
				text := baseText(n)
				if text != "" && end > start {
					add(Word{Text: text, Ruby: ruby.String(), Start: start, End: end})
				} else {
					prefix.WriteString(text)
				}
				return
			}
			if start, end, valid := n.timing(); valid {
				if text := baseText(n); text != "" {
					add(Word{Text: text, Start: start, End: end})
					return
				}
			} else if n.attr("", "begin") != "" || n.attr("", "end") != "" {
				badTiming = true
			}
		}
		if isTTML(n, "br") {
			prefix.WriteByte(' ')
		}
		prefix.WriteString(ttmlText(n.text))
		for _, child := range n.children {
			walk(child)
		}
	}
	for _, child := range n.children {
		walk(child)
	}
	if len(words) == 0 || badTiming {
		return nil
	}
	words[len(words)-1].Text += prefix.String()
	words[0].Text = strings.TrimLeft(words[0].Text, " \r\n\t")
	words[len(words)-1].Text = strings.TrimRight(words[len(words)-1].Text, " \r\n\t")
	return words
}

type lyricText struct{ lang, text string }

func preferredText(options []lyricText) string {
	for _, language := range []string{"zh-hans", "zh-cn", "zh"} {
		for _, x := range options {
			if strings.EqualFold(x.lang, language) || strings.HasPrefix(strings.ToLower(x.lang), language+"-") {
				return x.text
			}
		}
	}
	if len(options) > 0 {
		return options[0].text
	}
	return ""
}

func readTTMLLine(n *ttmlNode) (Line, bool) {
	var text strings.Builder
	for _, child := range n.children {
		text.WriteString(baseText(child))
	}
	line := Line{Text: strings.TrimSpace(text.String()), Agent: n.attr(ttmNS, "agent"), classified: true}
	if line.Text == "" {
		return line, false
	}
	// Whitespace between timed spans can be meaningful in English lyrics.
	words := ttmlWords(n)
	var joined strings.Builder
	var previous time.Duration
	ordered := len(words) > 0
	for _, word := range words {
		joined.WriteString(word.Text)
		ordered = ordered && word.Start >= previous
		previous = word.End
	}
	start, end, valid := n.timing()
	// Background groups may omit timing and use their children's ranges.
	if !valid && n.attr(ttmNS, "role") == "x-bg" && ordered {
		start, end, valid = words[0].Start, words[len(words)-1].End, true
	}
	if !valid {
		return line, false
	}
	line.At, line.End = start, end
	if ordered && joined.String() == line.Text && words[0].Start >= start && previous <= end {
		line.Words, line.Timed = words, true
	}
	var translations, romans []lyricText
	for _, child := range n.children {
		switch child.attr(ttmNS, "role") {
		case "x-translation", "x-roman":
			var text strings.Builder
			for _, part := range child.children {
				text.WriteString(baseText(part))
			}
			value := lyricText{child.attr(xmlNS, "lang"), strings.TrimSpace(text.String())}
			if value.text == "" {
				continue
			}
			if child.attr(ttmNS, "role") == "x-translation" {
				translations = append(translations, value)
			} else {
				romans = append(romans, value)
			}
		case "x-bg":
			if bg, ok := readTTMLLine(child); ok {
				line.Background = append(line.Background, bg)
			}
		}
	}
	line.Translation, line.Roman = preferredText(translations), preferredText(romans)
	return line, true
}

// ParseTTML builds the shared timeline directly from AMLL's TTML, without
// converting it to a format that would lose background vocals or translations.
func ParseTTML(source []byte, trackID int64) (Display, error) {
	root, err := readTTML(source)
	if err != nil {
		return Display{}, err
	}
	var lines []Line
	var authors, ids []string
	var head, body *ttmlNode
	for _, node := range root.children {
		switch {
		case isTTML(node, "head"):
			head = node
		case isTTML(node, "body"):
			body = node
		}
	}
	if body == nil {
		return Display{}, errors.New("TTML has no body")
	}
	translations, romans := map[string][]lyricText{}, map[string][]lyricText{}
	bgTranslations, bgRomans := map[string][][]lyricText{}, map[string][][]lyricText{}
	if head != nil {
		head.visit(func(n *ttmlNode) {
			if n.name.Local == "meta" {
				switch n.attr("", "key") {
				case "ncmMusicId":
					ids = append(ids, n.attr("", "value"))
				case "ttmlAuthorGithubLogin":
					if author := n.attr("", "value"); author != "" && !slices.Contains(authors, author) {
						authors = append(authors, author)
					}
				}
			}
			var dst map[string][]lyricText
			var bg map[string][][]lyricText
			switch n.name.Local {
			case "translation":
				if n.attr("", "type") != "replacement" {
					dst, bg = translations, bgTranslations
				}
			case "transliteration":
				dst, bg = romans, bgRomans
			}
			if dst != nil {
				for _, child := range n.children {
					key := child.attr("", "for")
					if child.name.Local != "text" || key == "" {
						continue
					}
					lang := n.attr(xmlNS, "lang")
					if text := strings.TrimSpace(baseText(child)); text != "" {
						dst[key] = append(dst[key], lyricText{lang, text})
					}
					i := 0
					for _, part := range child.children {
						if part.attr(ttmNS, "role") != "x-bg" {
							continue
						}
						if len(bg[key]) == i {
							bg[key] = append(bg[key], nil)
						}
						var text strings.Builder
						for _, node := range part.children {
							text.WriteString(baseText(node))
						}
						if value := strings.TrimSpace(text.String()); value != "" {
							bg[key][i] = append(bg[key][i], lyricText{lang, value})
						}
						i++
					}
				}
			}
		})
	}
	if trackID > 0 && len(ids) > 0 && !slices.Contains(ids, strconv.FormatInt(trackID, 10)) {
		return Display{}, fmt.Errorf("TTML does not match song %d", trackID)
	}
	body.visit(func(n *ttmlNode) {
		if !isTTML(n, "p") {
			return
		}
		line, ok := readTTMLLine(n)
		if !ok {
			return
		}
		key := n.attr(itunesNS, "key")
		if key == "" {
			key = n.attr(appleLyricsNS, "key")
		}
		if line.Translation == "" {
			line.Translation = preferredText(translations[key])
		}
		if line.Roman == "" {
			line.Roman = preferredText(romans[key])
		}
		for i := range line.Background {
			if line.Background[i].Translation == "" && i < len(bgTranslations[key]) {
				line.Background[i].Translation = preferredText(bgTranslations[key][i])
			}
			if line.Background[i].Roman == "" && i < len(bgRomans[key]) {
				line.Background[i].Roman = preferredText(bgRomans[key][i])
			}
		}
		lines = append(lines, line)
	})
	if len(lines) == 0 {
		return Display{}, errors.New("TTML has no usable timed lyrics")
	}
	display := newDisplay(lines)
	display.Source, display.Language, display.Authors = AMLL, root.attr(xmlNS, "lang"), authors
	return display, nil
}
