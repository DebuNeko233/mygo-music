package components

import (
	"fmt"
	"time"
)

// Clock formats a duration as minutes and seconds.
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// PlaybackClock includes hours for long tracks such as podcast episodes.
func PlaybackClock(d time.Duration) string {
	if d < time.Hour {
		return Clock(d)
	}
	s := int(d / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, (s/60)%60, s%60)
}

// Plays formats a play count as people in China read it.
func Plays(n float64) string {
	switch {
	case n >= 1e8:
		return fmt.Sprintf("%.1f亿", n/1e8)
	case n >= 1e4:
		return fmt.Sprintf("%.0f万", n/1e4)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// Greeting returns what to say at this hour.
func Greeting(now time.Time) string {
	switch h := now.Hour(); {
	case h < 5:
		return "夜深了"
	case h < 11:
		return "早上好"
	case h < 13:
		return "中午好"
	case h < 18:
		return "下午好"
	default:
		return "晚上好"
	}
}

// Lyric size bounds are shared by the desktop renderer and its settings control.
const LyricSizeMin = 14
const LyricSizeMax = 48

// Count formats a count as people in China read it, to a tenth of a 万.
func Count(n int) string {
	if n >= 1e4 && n < 1e6 {
		return fmt.Sprintf("%.1f万", float64(n)/1e4)
	}
	return Plays(float64(n))
}

// CommentTime says when a comment was written, as NetEase does: in
// minutes or hours while recent, then by the day.
func CommentTime(t, now time.Time) string {
	d := now.Sub(t)
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d分钟前", int(d/time.Minute))
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format("15:04")
	case now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return "昨天 " + t.Format("15:04")
	case y1 == y2:
		return fmt.Sprintf("%d月%d日", m1, d1)
	default:
		return fmt.Sprintf("%d年%d月%d日", y1, m1, d1)
	}
}
