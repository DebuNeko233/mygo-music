package speech

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type engineTranscriber struct {
	started, release chan struct{}
	events           chan string
}

// Final release closes the active recognizer and cannot reopen it for a late queued job.
func TestCaptionEngineCloseIsTerminal(t *testing.T) {
	e := new(Engine)
	recognizer := &engineTranscriber{make(chan struct{}), make(chan struct{}), make(chan string, 4)}
	done := make(chan error, 1)
	go func() {
		done <- e.Transcribe(context.Background(), "model", nil, 0, 0, func(string) (Transcriber, error) { return recognizer, nil }, nil, nil)
	}()
	<-recognizer.started
	closed := make(chan struct{})
	go func() { e.Close(); close(closed) }()
	close(recognizer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-closed
	e.Close()
	err := e.Transcribe(context.Background(), "model", nil, 0, 0, func(string) (Transcriber, error) { t.Error("reopened after close"); return recognizer, nil }, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late job accepted: %v", err)
	}
	if first, second := <-recognizer.events, <-recognizer.events; first != "finished" || second != "closed" || len(recognizer.events) != 0 {
		t.Fatalf("unsafe recognizer release: %s, %s", first, second)
	}
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
