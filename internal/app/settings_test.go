package app

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// A failed final settings write leaves an error in the application log.
func TestFinalSettingsSaveReportsFailure(t *testing.T) {
	var output bytes.Buffer
	writer := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(writer) })
	a := newTestApp()
	a.settings.Path = t.TempDir()
	a.saveFinalSettings()
	if !strings.Contains(output.String(), "settings: final save:") {
		t.Fatalf("final save failure was not reported: %q", output.String())
	}
}
