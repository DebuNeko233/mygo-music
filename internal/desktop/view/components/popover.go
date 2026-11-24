package components

import (
	"github.com/egoist/mygo/ui"
)

// Popover holds the colors of a popover as AppKit draws them.
type Popover struct {
	Fill, Edge, Track, Knob ui.Color
}

func PopoverColors(dark bool) Popover {
	if dark {
		return Popover{Fill: ui.Hex("#2b2b2e"), Edge: ui.RGBA(255, 255, 255, 0.14), Track: ui.RGBA(255, 255, 255, 0.16), Knob: ui.Hex("#ececec")}
	}
	return Popover{Fill: ui.Hex("#f6f6f6"), Edge: ui.RGBA(0, 0, 0, 0.12), Track: ui.RGBA(0, 0, 0, 0.12), Knob: ui.Hex("#ffffff")}
}
