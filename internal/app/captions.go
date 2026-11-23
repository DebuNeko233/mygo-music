package app

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/speech"
	"netease-music-mygo/internal/viewmodel"
)

var (
	captionsAvailable = speech.Available
	modelInstalled    = speech.Installed
	downloadModel     = speech.Download
	removeModel       = speech.Remove
	newTranscriber    = func(dir string) (transcriber, error) { return speech.NewRecognizer(dir) }
)

type transcriber = speech.Transcriber

var captionEngine speech.Engine

func (a *app) captionServices() viewmodel.CaptionServices {
	return viewmodel.CaptionServices{
		Player: a.pl, Engine: &captionEngine, Post: a.post,
		Available: captionsAvailable, Enabled: func() bool { return a.settings.Captions },
		Enable: func() { a.settings.Captions = true; a.settings.saveSoon() },
		Changed: func() {
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
			a.redraw()
		},
		ModelDir: modelDir, TranscriptPath: transcriptPath,
		Installed: modelInstalled, Download: downloadModel, Remove: removeModel, NewTranscriber: newTranscriber,
	}
}
func (a *app) captionsShown() bool { return captionsAvailable() }
func (a *app) modelReady() bool {
	m := &a.captions.Model
	if m.Checked || m.Checking || m.Downloading || m.Deleting {
		return m.Installed
	}
	return a.captions.ModelReady(a.captionServices())
}
func (a *app) captionsOn() bool      { return a.settings.Captions && a.captionsShown() && a.modelReady() }
func (a *app) startModelDownload()   { a.captions.StartModelDownload(a.captionServices()) }
func (a *app) cancelModelDownload()  { a.captions.CancelModelDownload() }
func (a *app) deleteModel()          { a.captions.DeleteModel(a.captionServices()) }
func (a *app) syncCaptions()         { a.captions.Sync(a.captionServices()) }
func (a *app) captionsWritten() bool { return a.captions.Written(a.pl.Duration()) }
func (a *app) captionsWriting(pos time.Duration) bool {
	on := false
	if a.captions.Transcript == nil {
		on = a.captionsOn()
	}
	return a.captions.Writing(pos, on)
}
func (a *app) transcriptWrite() func() { return a.captions.Write(transcriptPath) }

// modelDir is where the model and the libraries that run it download to.
func modelDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models"), nil
}

func transcriptPath(program int64) (string, error) {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "transcripts", strconv.FormatInt(program, 10)+".json"), nil
}
