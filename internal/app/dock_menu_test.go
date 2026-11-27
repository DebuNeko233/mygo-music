/*
验证 Dock 菜单命令复用播放器状态，并在加载、模式切换与退出时保持一致。
*/

package app

import (
	"slices"
	"testing"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

func dockTestApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.pl.SuspendCache() // Hold loading deterministically, without account or network requests.
	a.dockMenu = a.newDockMenu()
	a.pl.Changed = a.syncDockMenu
	a.syncDockMenu()
	t.Cleanup(func() { a.pl.Close(); a.art.Close() })
	return a
}

// Choosing a specific mode repeatedly must not act as a toggle, reshuffle
// traversal or restart the selected song, including duplicate queue entries.
func TestDockModeSelectionIsExplicitAndIdempotent(t *testing.T) {
	a := dockTestApp(t)
	a.pl.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 1}}, 2)
	for _, option := range []struct {
		id   string
		mode player.RepeatMode
	}{{"repeat-one", player.RepeatOne}, {"repeat-all", player.RepeatAll}, {"repeat-off", player.RepeatOff}} {
		revision := a.pl.QueueRevision()
		chooseDock(t, a, option.id)
		chooseDock(t, a, option.id)
		if a.pl.Repeat() != option.mode || !a.dockMenu.ItemByID(option.id).IsChecked() || a.pl.QueueRevision() != revision {
			t.Fatalf("repeat selection toggled or replaced the queue: %s", option.id)
		}
	}
	chooseDock(t, a, "shuffle-on")
	order, at := a.pl.PlayOrder()
	order = slices.Clone(order)
	revision := a.pl.QueueRevision()
	chooseDock(t, a, "shuffle-on")
	again, againAt := a.pl.PlayOrder()
	if !a.pl.Shuffle() || !slices.Equal(again, order) || againAt != at || a.pl.QueueRevision() != revision {
		t.Fatal("reselecting shuffle-on changed traversal")
	}
	chooseDock(t, a, "shuffle-off")
	chooseDock(t, a, "shuffle-off")
	_, at = a.pl.PlayOrder()
	if a.pl.Shuffle() || at != 2 || a.pl.Current().ID != 1 || !a.pl.Playing() || !a.dockMenu.ItemByID("shuffle-off").IsChecked() {
		t.Fatal("shuffle-off did not preserve the selected duplicate")
	}
}

// Finishing a sole temporary file clears the queue and disables all Dock
// transport actions immediately, rather than retaining the old play state.
func TestDockTemporaryQueueBecomesEmpty(t *testing.T) {
	a := dockTestApp(t)
	a.pl.PlayTemporary(netease.Track{LocalID: 7}, nil)
	chooseDock(t, a, "next")
	if a.pl.Current() != nil || len(a.pl.Queue()) != 0 || a.pl.Playing() || a.dockMenu.ItemByID("play").Label != "播放" {
		t.Fatal("temporary playback did not clear its Dock state")
	}
	for _, id := range []string{"play", "next", "previous"} {
		if a.dockMenu.ItemByID(id).IsEnabled() {
			t.Fatalf("empty temporary queue left %s enabled", id)
		}
	}
}

// A Dock next command may wait for the FM feed. Returning to a prior song
// while it waits must not let the delayed feed replace the manual selection.
func TestDockFMFeedRespectsManualSelection(t *testing.T) {
	a := dockTestApp(t)
	var refill func([]netease.Track)
	a.pl.PlayRadio("test", []netease.Track{{ID: 1}, {ID: 2}}, func(got func([]netease.Track)) { refill = got })
	chooseDock(t, a, "next")
	chooseDock(t, a, "next")
	if !a.pl.Loading() || refill == nil || !a.dockMenu.ItemByID("previous").IsEnabled() {
		t.Fatal("FM did not retain controls while awaiting its feed")
	}
	chooseDock(t, a, "previous")
	selected := a.pl.Current().ID
	refill([]netease.Track{{ID: 3}})
	if a.pl.Current().ID != selected || a.pl.StepDirection() != -1 || len(a.pl.Queue()) != 3 {
		t.Fatal("a delayed FM feed displaced the Dock previous selection")
	}
}

func chooseDock(t *testing.T, a *app, id string) {
	t.Helper()
	item := a.dockMenu.ItemByID(id)
	if item == nil || !item.IsEnabled() {
		t.Fatalf("Dock command %s unavailable", id)
	}
	item.Click(item, nil)
}

// Empty queues disable playback commands. A loading song may be paused and
// resumed, and next/previous use the same traversal and direction as the UI.
func TestDockPlaybackCommands(t *testing.T) {
	a := dockTestApp(t)
	for _, id := range []string{"play", "next", "previous"} {
		if a.dockMenu.ItemByID(id).IsEnabled() {
			t.Fatalf("empty queue enabled %s", id)
		}
	}
	a.pl.PlayQueue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}}, 1)
	if a.dockMenu.ItemByID("play").Label != "暂停" {
		t.Fatal("loading playback did not offer pause")
	}
	chooseDock(t, a, "play")
	if a.pl.Playing() || a.dockMenu.ItemByID("play").Label != "播放" {
		t.Fatal("Dock pause did not update intent and action")
	}
	chooseDock(t, a, "play")
	if !a.pl.Playing() {
		t.Fatal("Dock play did not resume intent")
	}
	chooseDock(t, a, "previous")
	if a.pl.Current().ID != 1 || a.pl.StepDirection() != -1 {
		t.Fatal("Dock previous did not move backward")
	}
	chooseDock(t, a, "next")
	if a.pl.Current().ID != 2 || a.pl.StepDirection() != 1 {
		t.Fatal("Dock next did not move forward")
	}
}

// Dock selection changes Player directly; UI toggles and restored sessions
// update the same menu instance without a copied playback mode.
func TestDockModesAndRestoration(t *testing.T) {
	a := dockTestApp(t)
	menu := a.dockMenu
	chooseDock(t, a, "repeat-one")
	chooseDock(t, a, "shuffle-on")
	if a.pl.Repeat() != player.RepeatOne || !a.pl.Shuffle() {
		t.Fatal("Dock mode choices did not reach Player")
	}
	a.pl.CycleRepeat()
	a.pl.ToggleShuffle()
	if !menu.ItemByID("repeat-off").IsChecked() || !menu.ItemByID("shuffle-off").IsChecked() ||
		menu.ItemByID("repeat-one").IsChecked() || menu.ItemByID("shuffle-on").IsChecked() {
		t.Fatal("UI mode changes did not update exclusive Dock choices")
	}
	a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 1}}, Order: []int{0}, At: 0,
		Repeat: player.RepeatAll, Shuffle: true})
	if a.dockMenu != menu || !menu.ItemByID("repeat-all").IsChecked() || !menu.ItemByID("shuffle-on").IsChecked() ||
		!menu.ItemByID("next").IsEnabled() || !menu.ItemByID("previous").IsEnabled() {
		t.Fatal("restored single-song queue did not synchronize controls")
	}
	a.quitting = true
	item := menu.ItemByID("repeat-off")
	item.Click(item, nil)
	if a.pl.Repeat() != player.RepeatAll {
		t.Fatal("retained menu callback changed playback during quit")
	}
}
