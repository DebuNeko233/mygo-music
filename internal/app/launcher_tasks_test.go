/*
验证启动器控制复用播放器命令、恢复后执行一次，以及文件和链接的启动意图顺序。
*/

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
	if a.startup.show {
		t.Fatal("a playback task requested the main window")
	}
	a.quitting = true
	run("shuffle")
	if a.pl.Shuffle() {
		t.Fatal("a task changed modes after quit")
	}
}

// Static launcher tasks remain clickable with no queue, including during
// cache relocation; they must not leave a phantom playing intent behind.
func TestLauncherEmptyPlaybackQueue(t *testing.T) {
	a := newTestApp()
	defer a.release()
	a.restored = true
	a.pl.SuspendCache()
	for _, action := range []string{"play-pause", "previous", "next"} {
		if !a.handlePlaybackAction([]string{playbackActionFlag + action}) {
			t.Fatalf("valid empty-queue task was rejected: %s", action)
		}
		if a.pl.Current() != nil || len(a.pl.Queue()) != 0 || a.pl.Playing() || a.startup.show {
			t.Fatalf("empty-queue task changed playback or opened a window: %s", action)
		}
	}
}

// Invalid invocations preserve both the active playback and a pending cold
// start task; mixed file arguments must not execute a valid-looking prefix.
func TestLauncherRejectsInvalidInvocationWithoutSideEffects(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		a := newTestApp()
		a.pl.SuspendCache()
		a.restored = !waiting
		a.startup.waiting = waiting
		tracks := []netease.Track{{ID: 1}, {ID: 2}}
		if waiting {
			a.pl.Restore(player.Session{Queue: tracks, Order: []int{0, 1}, At: 1})
			a.startup.queue(openingIntent{playbackActions: []string{"repeat"}})
		} else {
			a.pl.PlayQueue(tracks, 1)
		}
		pending := a.startup.pending
		playing := a.pl.Playing()
		for _, args := range [][]string{
			nil, {""}, {"--other"}, {playbackActionFlag}, {playbackActionFlag + "unknown"},
			{playbackActionFlag + "next "}, {playbackActionFlag + "Next"},
			{playbackActionFlag + "shuffle", "song.mp3"},
			{playbackActionFlag + "next", playbackActionFlag + "previous"},
		} {
			if a.handlePlaybackAction(args) || a.pl.Current().ID != 2 || a.pl.Playing() != playing ||
				a.pl.Shuffle() || a.pl.Repeat() != player.RepeatOff || a.startup.pending != pending ||
				pending != nil && !slices.Equal(pending.playbackActions, []string{"repeat"}) {
				t.Fatalf("waiting=%v: invalid invocation changed an active or pending task: %v", waiting, args)
			}
		}
		a.release()
	}
}

// Real file/link entry points replace old control batches, and new control
// batches replace older openings, without replay after restoration completes.
func TestLauncherOpeningIntentIntegration(t *testing.T) {
	for _, kind := range []string{"file", "link"} {
		for _, actionLast := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/opening-last", true: "/control-last"}[actionLast], func(t *testing.T) {
				a := musicLinkApp(t)
				t.Cleanup(a.release)
				posts := testPosts(a)
				a.startup.waiting = true
				control := func() {
					a.handlePlaybackAction([]string{playbackActionFlag + "shuffle"})
					a.handlePlaybackAction([]string{playbackActionFlag + "repeat"})
				}
				if !actionLast {
					control()
				}
				if kind == "file" {
					a.openTemporaryAudio(filepath.Join("..", "features", "player", "testdata", "seek.flac"))
				} else if !a.openMusicInput("orpheus://album/34") {
					t.Fatal("fixture link was rejected")
				}
				if actionLast {
					control()
				}
				a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 11}}, Order: []int{0}, At: 0})
				a.finishStartup() // Restoration has not been accepted by App yet.
				if a.startup.pending == nil || !a.startup.waiting || a.pl.Shuffle() || a.pl.Playing() {
					t.Fatal("pending opening ran before App finished restoration")
				}
				a.restored = true
				a.finishStartup()
				if actionLast {
					if !a.pl.Shuffle() || a.pl.Repeat() != player.RepeatAll || a.pl.Current().ID != 11 || a.pl.Playing() || a.nav.Current.Page != pageHome {
						t.Fatal("an older file/link displaced the latest controls")
					}
				} else {
					if kind == "file" {
						refreshPost(t, posts)
						if a.pl.Current() == nil || !a.pl.Current().Temporary {
							t.Fatal("latest file was not opened")
						}
					} else if a.nav.Current != (route{Page: pageAlbum, ID: 34}) {
						t.Fatal("latest link was not opened")
					}
					if a.pl.Shuffle() || a.pl.Repeat() != player.RepeatOff {
						t.Fatal("superseded controls changed playback modes")
					}
				}
				selected, revision := a.pl.Current().Key(), a.pl.QueueRevision()
				a.nav.Current = route{Page: pageLocal}
				a.finishStartup()
				if a.startup.pending != nil || a.pl.Current().Key() != selected || a.pl.QueueRevision() != revision || a.nav.Current.Page != pageLocal {
					t.Fatal("startup opening was replayed")
				}
			})
		}
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
		command := a.startup.take().playbackActions[0]
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

// Relative mode commands received during restoration retain every click,
// rather than collapsing two shuffle toggles or three repeat cycles to one.
func TestLauncherStartupRetainsEachToggle(t *testing.T) {
	a := newTestApp()
	defer a.release()
	a.pl.SuspendCache()
	a.startup.waiting = true
	for _, action := range []string{"shuffle", "shuffle", "repeat", "repeat", "repeat"} {
		a.handlePlaybackAction([]string{playbackActionFlag + action})
	}
	a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 1}}, Order: []int{0}, At: 0})
	a.restored = true
	changes := 0
	a.pl.Changed = func() { changes++ }
	a.finishStartup()
	a.finishStartup()
	if changes != 5 || a.pl.Shuffle() || a.pl.Repeat() != player.RepeatOff || a.pl.Playing() {
		t.Fatalf("startup lost or replayed clicks: changes=%d shuffle=%v repeat=%v", changes, a.pl.Shuffle(), a.pl.Repeat())
	}
}

// Shuffle-off and next do not commute for a restored shuffled queue. Startup
// must execute them in click order, not sort or collapse the pending commands.
func TestLauncherStartupPreservesCommandOrder(t *testing.T) {
	for _, tc := range []struct {
		actions []string
		want    int64
	}{
		{[]string{"shuffle", "next"}, 2},
		{[]string{"next", "shuffle"}, 3},
	} {
		a := newTestApp()
		a.pl.SuspendCache()
		a.startup.waiting = true
		for _, action := range tc.actions {
			a.handlePlaybackAction([]string{playbackActionFlag + action})
		}
		a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, Order: []int{0, 2, 1}, At: 0, Shuffle: true})
		a.restored = true
		a.finishStartup()
		a.finishStartup()
		if a.pl.Current().ID != tc.want || a.pl.Shuffle() || !a.pl.Playing() || a.startup.pending != nil {
			t.Fatalf("startup reordered %v: current=%d shuffle=%v", tc.actions, a.pl.Current().ID, a.pl.Shuffle())
		}
		a.release()
	}
}
