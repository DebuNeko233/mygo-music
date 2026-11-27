/*
验证 Windows 模式文字跟随真实播放器命令和会话恢复，保持启动参数及退出行为。
原生提交由内存任务列表替代，不接触测试机器的任务栏。
*/

package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/features/netease"
	"netease-music-mygo/internal/features/player"
)

type recordedLauncherTasks struct {
	attempts int
	err      error
	tasks    []mygo.JumpListTask
}

func (r *recordedLauncherTasks) SetTasks(tasks []mygo.JumpListTask) error {
	r.attempts++
	if r.err != nil {
		return r.err
	}
	r.tasks = slices.Clone(tasks)
	for i := range r.tasks {
		r.tasks[i].Args = slices.Clone(tasks[i].Args)
	}
	return nil
}

func (r *recordedLauncherTasks) title(t *testing.T, action string) string {
	t.Helper()
	for _, task := range r.tasks {
		if slices.Equal(task.Args, []string{playbackActionFlag + action}) {
			return task.Title
		}
	}
	t.Fatalf("missing launcher command %s", action)
	return ""
}

func launcherMenuTestApp(t *testing.T) (*app, *recordedLauncherTasks) {
	t.Helper()
	a := newTestApp()
	t.Cleanup(a.release)
	a.restored = true
	a.pl.SuspendCache()
	r := &recordedLauncherTasks{}
	a.launcherMenu = &launcherTaskMenu{list: r}
	a.pl.Changed = a.syncLauncherTasks
	a.syncLauncherTasks()
	return a, r
}

// Windows submits commands in the same relative order as the existing Dock
// menu; separators and native mode submenus do not change that ordering.
func TestLauncherOrderMatchesDock(t *testing.T) {
	a, r := launcherMenuTestApp(t)
	var dockActions []string
	for _, item := range a.newDockMenu().Items() {
		if item.Type == mygo.MenuItemSeparator {
			continue
		}
		action := item.ID
		if len(item.Submenu) > 0 {
			action, _, _ = strings.Cut(item.Submenu[0].ID, "-")
		}
		if action == "play" {
			action = "play-pause"
		}
		dockActions = append(dockActions, playbackActionFlag+action)
	}
	if len(r.tasks) != len(dockActions) {
		t.Fatal("taskbar and Dock expose different playback commands")
	}
	for i, task := range r.tasks {
		if !slices.Equal(task.Args, []string{dockActions[i]}) {
			t.Fatalf("taskbar command %d differs from Dock: %v, want %s", i, task.Args, dockActions[i])
		}
	}
}

// Labels describe the current mode after either launcher clicks or direct
// player commands, while every submitted task still executes its old action.
func TestLauncherModeLabelsFollowPlayback(t *testing.T) {
	a, r := launcherMenuTestApp(t)
	if r.title(t, "shuffle") != "随机播放：关闭" || r.title(t, "repeat") != "循环：关闭" {
		t.Fatal("initial labels do not describe the idle player")
	}
	for _, step := range []struct{ action, title string }{
		{"shuffle", "随机播放：开启"}, {"shuffle", "随机播放：关闭"},
		{"repeat", "循环：列表循环"}, {"repeat", "循环：单曲循环"}, {"repeat", "循环：关闭"},
	} {
		before := r.attempts
		if !a.handlePlaybackAction([]string{playbackActionFlag + step.action}) || r.title(t, step.action) != step.title || r.attempts != before+1 {
			t.Fatalf("click did not publish the resulting mode: %s", step.action)
		}
	}
	a.pl.SetShuffle(true)
	a.pl.SetRepeat(player.RepeatOne)
	if r.title(t, "shuffle") != "随机播放：开启" || r.title(t, "repeat") != "循环：单曲循环" {
		t.Fatal("commands outside the launcher left stale labels")
	}
	if len(r.tasks) != len(launcherPlaybackTasks) {
		t.Fatal("mode updates changed the set of playback commands")
	}
	for _, task := range r.tasks {
		if !a.handlePlaybackAction(task.Args) {
			t.Fatalf("updated task no longer reaches a playback command: %v", task.Args)
		}
	}
}

// Restored modes replace startup labels; progress, track changes, repeated
// selections and play/pause must not rewrite Explorer's persistent list.
func TestLauncherModeLabelsRestoreWithoutProgressWrites(t *testing.T) {
	a, r := launcherMenuTestApp(t)
	a.pl.Restore(player.Session{Queue: []netease.Track{{ID: 1}, {ID: 2}}, Order: []int{0, 1}, At: 0, Shuffle: true, Repeat: player.RepeatAll})
	if r.title(t, "shuffle") != "随机播放：开启" || r.title(t, "repeat") != "循环：列表循环" {
		t.Fatal("session restoration left startup mode labels")
	}
	before := r.attempts
	a.pl.SetShuffle(true)
	a.pl.SetRepeat(player.RepeatAll)
	a.pl.Next()
	a.pl.TogglePlay()
	for range 100 {
		a.pl.Changed()
	}
	if r.attempts != before {
		t.Fatal("unchanged mode labels caused native writes")
	}
}

// Failed shell submissions are not retried on progress events; a later mode
// change makes a fresh attempt with the complete current presentation.
func TestLauncherModeLabelsHandleSubmissionFailure(t *testing.T) {
	a, r := launcherMenuTestApp(t)
	r.err = errors.New("shell unavailable")
	a.pl.SetShuffle(true)
	before := r.attempts
	for range 100 {
		a.syncLauncherTasks()
	}
	if r.attempts != before || r.title(t, "shuffle") != "随机播放：关闭" {
		t.Fatal("failed submission was treated as accepted or repeatedly retried")
	}
	r.err = nil
	a.pl.SetRepeat(player.RepeatOne)
	if r.attempts != before+1 || r.title(t, "shuffle") != "随机播放：开启" || r.title(t, "repeat") != "循环：单曲循环" {
		t.Fatal("later mode change failed to repair the complete task list")
	}
}

// Shutdown restores generic commands once, and late notifications cannot
// reintroduce mode snapshots into the tasks Explorer keeps after exit.
func TestLauncherModeLabelsResetOnShutdown(t *testing.T) {
	a, r := launcherMenuTestApp(t)
	a.pl.SetShuffle(true)
	a.pl.SetRepeat(player.RepeatOne)
	before := r.attempts
	a.stopNative()
	if a.launcherMenu != nil || r.attempts != before+1 {
		t.Fatal("shutdown did not retire live task labels")
	}
	for _, task := range launcherPlaybackTasks {
		if r.title(t, task.action) != task.title {
			t.Fatalf("shutdown left a stale mode snapshot for %s", task.action)
		}
	}
	a.syncLauncherTasks()
	a.stopNative()
	if r.attempts != before+1 {
		t.Fatal("late notification or repeated shutdown rewrote tasks")
	}
}
