# Locate the current song in a playlist

The playlist page has a circular crosshair button at its lower right, above the player bar. Clicking it brings the current song into view near the middle of the list. The queue, song, playback position, pause state and open page are unchanged. The button remains available while paused if the current row is offscreen. Enter activates the focused button.

The button is shown only for a loaded playlist containing the current song while its row is outside the usable viewport. Even a partly visible current row hides it; rows wholly covered by the top navigation or bottom player bar do not. Clicking locate hides the button once the row is revealed; scrolling away shows it again. It is hidden without a current song, while the playlist is loading, when the song is absent/removed, and on other kinds of pages. Switching songs does not automatically move the list: locating remains an explicit action.

## Implementation

`playlistCurrentIndex` scans the displayed tracks by ID on each build. It resolves the location afresh instead of using the queue index or a cached lookup, so another queue, shuffled playback, a refreshed list or a changed current song cannot send the user to a stale position. The scan stops at the first match and creates no persistent lookup cache.

On a click, `ListState.ScrollTo(index+1, ui.Center)` requests the corresponding virtual row: row zero is the playlist header. MyGo can measure and reveal an offscreen row without constructing all preceding rows. The button stays outside the scrolling flow and is positioned above the player bar. It uses the existing `plugins/glass` material family, `glass.Glass{Interactive: true}`, with the theme colors, button focus/input handling and an SVG icon. Its translucent background blurs the content beneath, follows light/dark mode and honors Reduce Transparency; it has only a brief hover/press transition. No new background work, polling, full-list rendering or perpetual animation is introduced.

`trackRows` reports whether a built current row intersects the usable list viewport via `InViewMargin(-top, 0, -bottom, 0)`. This excludes both overlay bars and the virtual list's overscan, without storing UI elements or constructing the entire playlist. Visibility uses the row's keyed identity, so switching songs or reordering the list cannot reuse another song's bounds.

Geometry queries read the previous committed frame. The playlist therefore tracks its existing `pageScroll` offset and stores only the last offset, window dimensions, playlist/song/index identity and the text sources used in the header. Header sources are compared by value because renaming or refreshing can mutate the same playlist object. A changed offset, size, target identity or header source requests one follow-up frame to read the final geometry, including small scrolls behind a bar and header text wrapping that leave the virtual row range unchanged. `ListState.Visible()` also requests a follow-up when layout changes that range. These checks stop requesting frames at rest; no timer or background worker is involved.

This feature starts from `zhangnjsd/mygo-music` commit `52e0184d258984e6f0184bd52c3a7bcabe8943e1` and is independent of the other backport/settings PRs. No versions, dependencies, changelog or `mygo.json` are changed.

## Validation

```sh
CGO_ENABLED=0 go test ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./internal/app -run '^TestPlaylistLocate' -count=5
CGO_ENABLED=1 go test -race ./internal/app -run '^TestPlaylistLocate' -count=1
```

UI tests click the real control against a long virtual playlist, with a different queue order and with playback both paused and playing. They cover keyboard activation, visibility on entry and after changing to another visible song, partial rows versus rows under both bars, header growth/shrinkage after a rename or description refresh without a scroll/range change, scrolling and resizing, changing the song, replacing/reordering the displayed tracks, removal and unavailable states. The frame-budget regression verifies that neither the visible glass button nor the hidden state keeps scheduling idle frames. With `NM_FRAMES` set to an existing directory, `TestPlaylistLocateFrames` saves light/dark before/after screenshots for visual inspection.

On amd64, restoring the list through `ScrollState` can quantize a decorative heart's position by about 0.00003 DIP. The covered-page regression checks the content row/card's bounds with a 0.01 DIP tolerance, instead of comparing every child's integer-rounded text snapshot. It still checks that scrolling occurred, that covered content is not built, and that the same item returns to its position. The old test failed on Darwin amd64/Rosetta as on Linux; the revised test passes on both native arm64 and amd64.
