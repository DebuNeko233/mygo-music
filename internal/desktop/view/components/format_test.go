package components

import (
	"testing"
	"time"
)

// A length reads as minutes and seconds, and a play count the way people
// here read it.
func TestClockAndPlays(t *testing.T) {
	if s := Clock(185 * time.Second); s != "3:05" {
		t.Error(s)
	}
	if s := Plays(123456789); s != "1.2亿" {
		t.Error(s)
	}
	if s := Plays(56789); s != "6万" {
		t.Error(s)
	}
}

// The recent plays say when and on what the listener played something.
func TestRecentPlaysSayWhen(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	cases := []struct {
		at     time.Time
		device string
		want   string
	}{
		{now.Add(-30 * time.Second), "iphone", "刚刚 · 手机"},
		{now.Add(-12 * time.Minute), "osx", "12分钟前 · 电脑"},
		{now.Add(-3 * time.Hour), "web", "3小时前 · 网页版"},
		{now.Add(-50 * time.Hour), "", "2天前"},
	}
	for _, c := range cases {
		if got := PlayedAt(c.at, c.device, now); got != c.want {
			t.Errorf("PlayedAt(%v, %q) = %q, want %q", c.at, c.device, got, c.want)
		}
	}
}
