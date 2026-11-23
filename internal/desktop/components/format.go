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
