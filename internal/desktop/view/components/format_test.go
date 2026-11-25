package components

import (
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
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

// A recent play identifies when and on which device it was heard.
func TestRecentPlayDevice(t *testing.T) {
	now := time.Date(2026, 10, 26, 22, 0, 0, 0, time.Local)
	last := netease.Play{At: now.Add(-3 * time.Minute), Device: "android"}
	if got := HeardAt(last, now); got != "3分钟前在手机上听过" {
		t.Errorf("heard %q", got)
	}
}

// A comment's time reads as its age: minutes ago, then the hour, then the
// day or the date it goes back to.
func TestCommentTime(t *testing.T) {
	now := time.Date(2026, 10, 25, 22, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-20 * time.Second), "刚刚"},
		{now.Add(-42 * time.Minute), "42分钟前"},
		{now.Add(-5 * time.Hour), "17:00"},
		{now.Add(-23 * time.Hour), "昨天 23:00"},
		{time.Date(2026, 3, 8, 9, 0, 0, 0, time.Local), "3月8日"},
		{time.Date(2021, 4, 10, 9, 0, 0, 0, time.Local), "2021年4月10日"},
	} {
		if got := CommentTime(c.t, now); got != c.want {
			t.Errorf("%v: %q, want %q", c.t, got, c.want)
		}
	}
}
