package preferences

import (
	"strconv"
	"testing"
	"testing/synctest"
	"time"
)

// Slider updates must share a disk write even when storage finishes before the next input event.
func TestSettingsSaverBatchesSeparateInputEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var saved []string
		w := settingsSaver{write: func(_ string, data []byte) error {
			saved = append(saved, string(data))
			return nil
		}}
		for i := range 20 {
			w.submit(settingsWrite{path: "settings.json", data: []byte(strconv.Itoa(i))})
			time.Sleep(10 * time.Millisecond)
		}
		synctest.Wait()
		if len(saved) != 0 {
			t.Fatalf("a short slider gesture already wrote %d intermediate settings", len(saved))
		}
		start := time.Now()
		w.wait()
		if time.Since(start) != 0 {
			t.Fatal("flushing pending settings waited for the batching interval")
		}
		if len(saved) != 1 || saved[0] != "19" {
			t.Fatalf("saved %v, want only the final value", saved)
		}
	})
}

// Continuous adjustments must reach disk periodically, then flush the latest value without leaving a worker behind.
func TestSettingsSaverKeepsSavingDuringContinuousChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var saved []string
		w := settingsSaver{write: func(_ string, data []byte) error {
			saved = append(saved, string(data))
			return nil
		}}
		for i := range 100 {
			w.submit(settingsWrite{path: "settings.json", data: []byte(strconv.Itoa(i))})
			time.Sleep(10 * time.Millisecond)
		}
		synctest.Wait()
		if len(saved) == 0 || len(saved) > 5 {
			t.Fatalf("continuous changes made %d writes in one second", len(saved))
		}
		w.wait()
		if saved[len(saved)-1] != "99" {
			t.Fatalf("last saved %q, want the final value", saved[len(saved)-1])
		}
		w.submit(settingsWrite{path: "settings.json", data: []byte("next")})
		w.wait()
		if saved[len(saved)-1] != "next" {
			t.Fatal("a new save after flushing was lost")
		}
	})
}

// Flushing during an in-flight write must also persist newer settings before returning.
func TestSettingsSaverFlushesChangesDuringWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var saved []string
		w := settingsSaver{write: func(_ string, data []byte) error {
			if len(saved) == 0 {
				<-release
			}
			saved = append(saved, string(data))
			return nil
		}}
		w.submit(settingsWrite{path: "settings.json", data: []byte("old")})
		time.Sleep(300 * time.Millisecond)
		w.submit(settingsWrite{path: "settings.json", data: []byte("new")})
		go w.wait()
		synctest.Wait()
		close(release)
		w.wait()
		synctest.Wait()
		if len(saved) != 2 || saved[0] != "old" || saved[1] != "new" {
			t.Fatalf("saved %v, want the newer settings after the in-flight write", saved)
		}
	})
}
