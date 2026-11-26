//go:build !windows && !linux

/*
为不提供独立应用音量的系统提供空混音器适配。
*/

package mixvol

// openLink links nothing: this system keeps no volume of its own for the app,
// as macOS does, where every program plays into the one system volume.
func openLink(changed func(State)) link {
	changed(State{Attached: false})
	return nil
}
