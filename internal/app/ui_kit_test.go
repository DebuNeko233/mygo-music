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
