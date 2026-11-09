//go:build !windows && !linux

package mixvol

// openLink links nothing: this system keeps no volume of its own for the app,
// as macOS does, where every program plays into the one system volume.
func openLink(changed func(State)) link {
	changed(State{Attached: false})
	return nil
}
