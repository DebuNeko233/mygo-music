package app

import (
	"testing"
	"time"
)

// Each day of a week reads as its own name, Sunday first.
func TestWeekday(t *testing.T) {
	names := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	sunday := time.Date(2026, 3, 1, 12, 0, 0, 0, time.Local)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("%v is not a Sunday", sunday)
	}
	for i := range 7 {
		day := sunday.AddDate(0, 0, i)
		if got, want := weekday(day), names[day.Weekday()]; got != want {
			t.Errorf("%v: %q, want %q", day.Format("2006-01-02"), got, want)
		}
	}
}
