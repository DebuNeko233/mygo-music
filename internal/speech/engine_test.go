package speech

import (
	"context"
	"slices"
	"testing"
	"time"
)

type engineTranscriber struct {
	started, release chan struct{}
	events           chan string
}

func (t *engineTranscriber) Transcribe(context.Context, Source, time.Duration, time.Duration, func(Segment), func(time.Duration)) error {
	close(t.started)
	<-t.release
	t.events <- "finished"
	return nil
}
func (t *engineTranscriber) Close() { t.events <- "closed" }

// Removing a model waits for its active recognizer, then closes it before deleting its files.
func TestCaptionEngineRemovalWaitsForTranscription(t *testing.T) {
	e := new(Engine)
	recognizer := &engineTranscriber{make(chan struct{}), make(chan struct{}), make(chan string, 4)}
	done := make(chan error, 1)
	go func() {
		done <- e.Transcribe(context.Background(), "model", nil, 0, 0, func(string) (Transcriber, error) { return recognizer, nil }, nil, nil)
	}()
	<-recognizer.started
	removed := make(chan error, 1)
	go func() {
		removed <- e.Remove("model", func(string) error { recognizer.events <- "removed"; return nil })
	}()
	close(recognizer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-removed; err != nil {
		t.Fatal(err)
	}
	var events []string
	for len(recognizer.events) > 0 {
		events = append(events, <-recognizer.events)
	}
	if !slices.Equal(events, []string{"finished", "closed", "removed"}) {
		t.Fatalf("unsafe resource order: %v", events)
	}
}
