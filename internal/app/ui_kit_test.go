package app

import (
	"testing"
	"time"
)

// A length reads as minutes and seconds, and a play count the way people
// here read it.
func TestClockAndPlays(t *testing.T) {
	if s := clock(185 * time.Second); s != "3:05" {
		t.Error(s)
	}
	if s := plays(123456789); s != "1.2亿" {
		t.Error(s)
	}
	if s := plays(56789); s != "6万" {
		t.Error(s)
	}
}

// The hour of the day picks the greeting, at either side of each part of it.
func TestGreeting(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 3, 1, hour, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		hour int
		want string
	}{
		{0, "夜深了"}, {4, "夜深了"}, {5, "早上好"}, {10, "早上好"},
		{11, "中午好"}, {12, "中午好"}, {13, "下午好"}, {17, "下午好"},
		{18, "晚上好"}, {23, "晚上好"},
	} {
		if got := greeting(at(c.hour)); got != c.want {
			t.Errorf("at %d: %q, want %q", c.hour, got, c.want)
		}
	}
}
