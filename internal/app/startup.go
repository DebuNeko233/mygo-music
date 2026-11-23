package app

import "netease-music-mygo/internal/netease"

// Startup keeps one opening intent across account and queue restoration.
// A newer file or link supersedes either kind of older request.
type startupState struct {
	waiting bool
	show    bool
	pending *openingIntent
}

type openingIntent struct {
	file string
	link netease.MusicLink
}

func (s *startupState) queue(intent openingIntent) { s.pending = &intent }

func (s *startupState) take() *openingIntent {
	s.waiting = false
	intent := s.pending
	s.pending = nil
	return intent
}

func (a *app) finishStartup() {
	if a.quitting || !a.restored || a.pl == nil {
		return
	}
	intent := a.startup.take()
	if intent == nil {
		return
	}
	if intent.file != "" {
		a.openTemporaryAudio(intent.file)
	} else {
		a.openMusicLink(intent.link)
	}
}
