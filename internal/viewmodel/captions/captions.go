/*
管理字幕选择、模型安装状态和转写任务，通过服务接口加载与保存字幕。
*/

package captions

import (
	"context"
	"time"

	"netease-music-mygo/internal/captionstore"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/speech"
)

const captionLead = 30 * time.Second

// Captions owns transcript selection and jobs; native recognizers and disk ordering have one owner each.
type Captions struct {
	Model      CaptionModel
	Program    int64
	Transcript *captionstore.Transcript
	Lines      []lyrics.Line
	Language   string
	Revision   uint64
	Work       *CaptionWork
	Failed     int64
	Saved      time.Duration
	load       uint64
}
type CaptionModel struct {
	Checked, Checking, Installed, Downloading, Deleting bool
	Done, Total                                         int64
	Error                                               string
	generation                                          int
	cancel                                              context.CancelFunc
}
type CaptionWork struct {
	Program         int64
	From, Until, At time.Duration
	cancel          context.CancelFunc
}
type CaptionServices struct {
	Player             *player.Player
	Engine             *speech.Engine
	Post               func(func())
	Available, Enabled func() bool
	Enable, Changed    func()
	ModelDir           func() (string, error)
	TranscriptPath     func(int64) (string, error)
	Installed          func(string) bool
	Download           func(context.Context, string, func(int64, int64)) error
	Remove             func(string) error
	NewTranscriber     func(string) (speech.Transcriber, error)
}

// modelReady reports whether the model is installed, looking once. The look
// reads the model whole, a gigabyte and more of digests, so it runs off the
// thread that draws and its answer arrives in a frame of its own: until then
// the model counts as one that is not installed.
func (vm *Captions) ModelReady(s CaptionServices) bool {
	m := &vm.Model
	if m.Downloading || m.Deleting {
		return m.Installed
	}
	if !m.Checked && !m.Checking {
		m.Checking = true
		m.generation++
		generation := m.generation
		go func() {
			installed := false
			if dir, err := s.ModelDir(); err == nil {
				installed = s.Installed(dir)
			}
			s.Post(func() {
				if generation != m.generation {
					return
				}
				m.Checked, m.Checking, m.Installed = true, false, installed
				// An episode may be waiting for the answer.
				vm.Sync(s)
				s.Changed()
			})
		}()
	}
	return m.Installed
}

// captionsOn reports whether episodes are written down as they play.
func (vm *Captions) On(s CaptionServices) bool {
	return s.Enabled() && s.Available() && vm.ModelReady(s)
}

// startModelDownload downloads the model, its progress shown as it goes.
func (vm *Captions) StartModelDownload(s CaptionServices) {
	m := &vm.Model
	if m.Downloading || m.Installed || m.Deleting {
		return
	}
	dir, err := s.ModelDir()
	if err != nil {
		m.Error = "无法保存模型"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.Downloading, m.Error, m.cancel = true, "", cancel
	m.Done, m.Total = 0, speech.ModelSize()
	m.generation++
	generation := m.generation
	m.Checking = false
	go func() {
		// Shown in steps of a megabyte: the download reads far smaller
		// pieces.
		var shown int64
		err := s.Download(ctx, dir, func(done, total int64) {
			if done-shown < 1<<20 && done < total {
				return
			}
			shown = done
			s.Post(func() {
				if generation != m.generation {
					return
				}
				m.Done, m.Total = done, total
				s.Changed()
			})
		})
		s.Post(func() {
			s.Changed()
			if generation != m.generation {
				return
			}
			m.Downloading, m.cancel = false, nil
			switch {
			case err == nil:
				m.Installed, m.Checked = true, true
				s.Enable()
				vm.Sync(s)
			case ctx.Err() != nil:
			default:
				m.Error = "下载失败，请检查网络后重试"
			}
		})
	}()
}

// cancelModelDownload stops the download, keeping what came in for the
// next.
func (vm *Captions) CancelModelDownload() {
	if m := &vm.Model; m.cancel != nil {
		m.cancel()
	}
}

// deleteModel stops the transcription, lets the model go and removes it.
func (vm *Captions) DeleteModel(s CaptionServices) {
	m := &vm.Model
	if !m.Installed || m.Deleting {
		return
	}
	vm.Stop(s)
	m.Installed = false
	m.Deleting = true
	m.generation++
	m.Checking = false
	gen := m.generation
	dir, err := s.ModelDir()
	if err != nil {
		m.Deleting = false
		m.Installed = true
		m.Error = "无法访问模型目录：" + err.Error()
		return
	}
	go func() {
		err := s.Engine.Remove(dir, s.Remove)
		s.Post(func() {
			if gen != m.generation {
				return
			}
			m.Deleting = false
			if err != nil {
				m.Error = "删除模型失败：" + err.Error()
				m.Installed = true // Revert on failure
			} else {
				m.Checked = false // Force re-check on next use
			}
		})
	}()
}

// syncCaptions keeps the episode playing written down ahead of the
// listener: it loads its transcript, and starts a transcription from the
// first of what is not written down from where they are, unless one
// running gets there soon. It runs whenever the player changes.
func (vm *Captions) Sync(s CaptionServices) {
	cs := vm
	var program int64
	if tr := s.Player.Current(); tr != nil {
		program = tr.ProgramID
	}
	if program != cs.Program {
		vm.Pause()
		vm.Save(s)
		cs.Program, cs.Transcript, cs.Lines, cs.Language = program, nil, nil, ""
		cs.Revision++
		if program != 0 && s.Available() {
			vm.Load(program, s)
		}
	}
	if program == 0 || !vm.On(s) || cs.Failed == program {
		vm.Stop(s)
		return
	}
	if cs.Transcript == nil || !s.Player.Ready() {
		// The model stays for once the episode or its transcript loads.
		vm.Pause()
		return
	}
	from, until, ok := captionstore.Start(cs.Transcript.Spans, s.Player.Position(), s.Player.Duration())
	if w := cs.Work; w != nil {
		if ok && w.From <= from && from <= w.At+captionLead {
			return
		}
		w.cancel()
		cs.Work = nil
	}
	if !ok {
		s.Engine.Release()
		return
	}
	vm.Transcribe(program, from, until, s)
}

// stopCaptions stops the transcription, if one runs, and lets the model
// go once it has.
func (vm *Captions) Stop(s CaptionServices) {
	vm.Pause()
	s.Engine.Release()
}

// pauseCaptions stops the transcription, if one runs, keeping the model.
func (vm *Captions) Pause() {
	if w := vm.Work; w != nil {
		w.cancel()
		vm.Work = nil
	}
}

// transcribe writes down the episode playing from from up to until, on a
// goroutine. Opening the song is done there too: it reads the song's first
// bytes to find how to decode them, which waits for the network, and the
// window draws on the goroutine this is called from.
func (vm *Captions) Transcribe(program int64, from, until time.Duration, s CaptionServices) {
	open, err := s.Player.AudioOpener()
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &CaptionWork{Program: program, From: from, Until: until, At: from, cancel: cancel}
	vm.Work = w
	go func() {
		src, err := open()
		if err == nil {
			if posErr := src.SetPosition(int64(from.Seconds() * float64(src.SampleRate()))); posErr != nil {
				src.Close()
				err = posErr
			} else {
				defer src.Close()
			}
		}
		var dir string
		if err == nil {
			dir, err = s.ModelDir()
		}
		if err != nil {
			s.Post(func() { vm.GaveUp(w) })
			return
		}
		err = s.Engine.Transcribe(ctx, dir, src, from, until, s.NewTranscriber,
			func(segment speech.Segment) { s.Post(func() { vm.Said(w, segment) }) },
			func(at time.Duration) { s.Post(func() { vm.Heard(w, at, s) }) })
		s.Post(func() { vm.Done(w, err, ctx.Err() != nil, s) })
	}()
}

// captionGaveUp drops w, which wrote nothing down: the next change of the
// player starts another, as one that could not be opened is asked for again
// rather than taken as an episode that cannot be written down.
func (vm *Captions) GaveUp(w *CaptionWork) {
	if vm.Work == w {
		vm.Work = nil
	}
}

// captionSaid adds a segment written down to the transcript of its
// episode, unless the transcript has it: a transcription started again
// from where one stopped writes down again what that one did last.
func (vm *Captions) Said(w *CaptionWork, s speech.Segment) {
	cs := vm
	if cs.Work != w || w.Program != cs.Program || cs.Transcript == nil {
		return
	}
	segs := cs.Transcript.Segments
	atEnd := len(segs) == 0 || s.Start > segs[len(segs)-1].Start
	cs.Transcript.Segments = captionstore.AddSegment(segs, s)
	if len(cs.Transcript.Segments) == len(segs) {
		return
	}
	newLines := speech.Lines([]speech.Segment{s})
	if cs.Language == "" {
		cs.Language = lyrics.Lang(newLines)
	}
	if atEnd {
		cs.Lines = append(cs.Lines, newLines...)
	} else {
		cs.Lines = speech.Lines(cs.Transcript.Segments)
		cs.Revision++
	}
}

// captionHeard marks the episode written down from where w started to at.
func (vm *Captions) Heard(w *CaptionWork, at time.Duration, s CaptionServices) {
	cs := vm
	if cs.Work != w || w.Program != cs.Program || cs.Transcript == nil {
		return
	}
	w.At = max(w.At, at)
	cs.Transcript.Spans = captionstore.AddSpan(cs.Transcript.Spans, captionstore.Span{w.From, at})
	// Kept a minute at a time, should the app not quit as it should.
	if at > cs.Saved+time.Minute || at < cs.Saved {
		cs.Saved = at
		vm.Save(s)
	}
}

// captionDone ends w: all it was to write down is, unless it was stopped
// or failed. Then the next of what is not written down follows.
func (vm *Captions) Done(w *CaptionWork, err error, stopped bool, s CaptionServices) {
	cs := vm
	if cs.Work != w {
		return
	}
	cs.Work = nil
	if w.Program != cs.Program || cs.Transcript == nil || stopped {
		return
	}
	if err != nil {
		cs.Failed = w.Program
		vm.Save(s)
		return
	}
	to := w.Until
	if to == 0 {
		to = max(w.At, s.Player.Duration())
	}
	cs.Transcript.Spans = captionstore.AddSpan(cs.Transcript.Spans, captionstore.Span{w.From, to})
	vm.Save(s)
	vm.Sync(s)
}

// captionsWritten reports whether all of the episode playing is written
// down.
func (vm *Captions) Written(duration time.Duration) bool {
	cs := vm
	if cs.Transcript == nil {
		return false
	}
	_, _, ok := captionstore.Start(cs.Transcript.Spans, 0, duration)
	return !ok
}

// captionsWriting reports whether what is at pos is being written down
// and not yet there.
func (vm *Captions) Writing(pos time.Duration, on bool) bool {
	cs := vm
	if cs.Transcript == nil {
		return on && cs.Program != 0
	}
	if _, ok := captionstore.Covered(cs.Transcript.Spans, pos); ok {
		return false
	}
	return cs.Work != nil
}

func (vm *Captions) Load(program int64, s CaptionServices) {
	vm.load++
	request := vm.load
	path, err := s.TranscriptPath(program)
	if err != nil {
		vm.Transcript = &captionstore.Transcript{Model: speech.ModelName}
		return
	}
	read := captionstore.Reader(path)
	go func() {
		tr := read()
		s.Post(func() {
			if vm.Program != program || vm.Transcript != nil || request != vm.load {
				return
			}
			vm.Transcript = tr
			vm.Lines = speech.Lines(tr.Segments)
			vm.Language = lyrics.Lang(vm.Lines)
			vm.Revision++
			vm.Sync(s)
		})
	}()
}
func (vm *Captions) Save(s CaptionServices) {
	if write := vm.Write(s.TranscriptPath); write != nil {
		go write()
	}
}
func (vm *Captions) Write(pathFor func(int64) (string, error)) func() {
	if vm.Program == 0 || vm.Transcript == nil || len(vm.Transcript.Spans) == 0 {
		return nil
	}
	path, err := pathFor(vm.Program)
	if err != nil {
		return nil
	}
	return captionstore.Snapshot(path, vm.Transcript)
}
