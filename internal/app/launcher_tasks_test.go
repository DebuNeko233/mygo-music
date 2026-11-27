/*
验证启动器控制复用播放器命令、恢复后执行一次，以及文件和链接的启动意图顺序。
*/

package app

import (
	"encoding/json"
	"os"
	"testing"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

// Running-instance tasks toggle modes once and navigate without reopening
// the main window; malformed or mixed invocations do not alter playback.
func TestLauncherPlaybackCommands(t *testing.T) {
	a := newTestApp()
	defer a.release()
	a.restored = true
	a.pl.SuspendCache()
	a.pl.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 1)
	run := func(action string) {
		t.Helper()
		if !a.handlePlaybackAction([]string{playbackActionFlag + action}) {
			t.Fatalf("launcher action %s was rejected", action)
		}
	}
	run("previous")
	if a.pl.Current().ID != 1 || a.pl.StepDirection() != -1 {
		t.Fatal("previous task did not move backward")
	}
	run("next")
	if a.pl.Current().ID != 2 || a.pl.StepDirection() != 1 {
		t.Fatal("next task did not move forward")
	}
	run("play-pause")
	if a.pl.Playing() {
		t.Fatal("play/pause task did not pause the pending load")
	}
	run("play-pause")
	if !a.pl.Playing() {
		t.Fatal("play/pause task did not resume")
	}
	for _, want := range []bool{true, false} {
		run("shuffle")
		if a.pl.Shuffle() != want {
			t.Fatal("shuffle task did not toggle once")
		}
	}
	for _, want := range []player.RepeatMode{player.RepeatAll, player.RepeatOne, player.RepeatOff} {
		run("repeat")
		if a.pl.Repeat() != want {
			t.Fatal("repeat task did not cycle once")
		}
	}
	for _, args := range [][]string{nil, {playbackActionFlag + "unknown"}, {playbackActionFlag + "shuffle", "song.mp3"}, {"--other"}} {
		if a.handlePlaybackAction(args) || a.pl.Shuffle() {
			t.Fatalf("unrelated invocation consumed: %v", args)
		}
	}
	if a.startup.show {
		t.Fatal("a playback task requested the main window")
	}
	a.quitting = true
	run("shuffle")
	if a.pl.Shuffle() {
		t.Fatal("a task changed modes after quit")
	}
}

// Every packaged Linux launcher action must reach a known single-instance
// command, without duplicated actions or extra unhandled arguments.
func TestLinuxLauncherActionsReachPlayback(t *testing.T) {
	b, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Linux struct {
			Actions []struct {
				ID   string   `json:"id"`
				Args []string `json:"args"`
			} `json:"actions"`
		} `json:"linux"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	defer a.release()
	a.startup.waiting = true
	seen := map[string]bool{}
	for _, action := range config.Linux.Actions {
		if !a.handlePlaybackAction(action.Args) {
			t.Fatalf("packaged action %s does not reach a playback command", action.ID)
		}
		command := a.startup.take().playbackAction
		if seen[command] {
			t.Fatalf("packaged action repeats command %s", command)
		}
		seen[command] = true
		a.startup.waiting = true
	}
	if len(seen) != len(launcherPlaybackTasks) {
		t.Fatal("Linux package is missing playback commands")
	}
}

// A cold-start task waits for the restored queue; later file/link intents
// supersede it, and a later task supersedes an older file without replay.
func TestLauncherStartupOrdering(t *testing.T) {
	for _, actionLast := range []bool{false, true} {
		a := newTestApp()
		a.pl.SuspendCache()
		a.startup.waiting = true
		if !actionLast {
			a.handlePlaybackAction([]string{playbackActionFlag + "shuffle"})
		}
		a.startup.queue(openingIntent{file: "older.mp3"})
		if actionLast {
			a.handlePlaybackAction([]string{playbackActionFlag + "shuffle"})
		}
		if a.pl.Shuffle() {
			t.Fatal("task ran before restoration")
		}
		if actionLast {
			a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 1}}, Order: []int{0}, At: 0})
			a.restored = true
			a.finishStartup()
			a.finishStartup()
			if !a.pl.Shuffle() || a.pl.Playing() || a.startup.pending != nil {
				t.Fatal("cold-start task was lost, replayed or started audio unexpectedly")
			}
		} else if a.startup.take().file != "older.mp3" {
			t.Fatal("older control task displaced a newer file")
		}
		a.release()
	}
}
