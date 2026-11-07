# Now Playing lyrics in tall windows

Adapted from [egoist/mygo-music PR #64](https://github.com/egoist/mygo-music/pull/64), merged as [32499f0acf6058c2fd3fe7b69874a5d6bf608571](https://github.com/egoist/mygo-music/commit/32499f0acf6058c2fd3fe7b69874a5d6bf608571). The original fix is credited to [deepfry666](https://github.com/deepfry666); upstream also credits EGOIST and Claude Opus 5.5.

## Fix and adaptation

The cover stops growing at its maximum size but its column stays vertically centered. As the window grows taller, that column moves down by half of its unused cover space. Subtract this displacement from the adjacent pane's top, keeping lyrics, comments and the queue below the tabs instead of pushing them down with the cover. Landscape placement is retained.

Additional resize regression coverage found that the same lyric line was not followed again after its viewport resized and reflowed. Remember the last viewport size and follow again only when that size or the current line changes. Manual scrolling with unchanged geometry stays untouched. The shared timed-line renderer also serves podcast captions.

Use layout bounds consistently and preserve the scroll offset from before applying a pending glide target: the bounds describe the previous frame, so adding the newly applied offset would count the movement twice. This avoids an initial layout adjustment or successive resizes sending the current line offscreen. All retained state is plain numeric data, not UI contexts or elements.

## Validation

```sh
CGO_ENABLED=0 go test ./...
go tool mygo vet .
CGO_ENABLED=1 go test -race ./internal/app -run '^TestLyrics(FillATallWindow|ReflowWhenWindowResizes|ManualScrollStaysAfterResize)$' -count=1
```

The UI regressions cover 1200 × 800, 750 × 1330 and 600 × 1400; resizing landscape → portrait → landscape; and manual scrolling after resizing. The original tall-window tests fail before the fix and pass afterwards. With `NM_FRAMES` pointing to an existing directory, `TestLyricsFillATallWindow` also saves screenshots for visual inspection. Existing frame-budget tests guard against unnecessary redraws.

This fix accompanies the settings design on a PR branch based directly on upstream `52e0184`. The four earlier backport PRs are not included. No version, dependency, changelog or `mygo.json` changes are included. Only macOS packages are built locally.

## Verification boundary

The full ordinary test suite, MyGo vet, the new UI regressions repeated five times, and the focused layout race check passed on macOS. No Windows package was built for this fix.

An expanded race run also reported races in the unchanged `TestEpisodeLikeClickBeatsAnswer` test. Re-running that test alone with `-race -count=5` on the unmodified upstream baseline `52e0184` reproduced the failure: callbacks append to `app.kept` concurrently without the test's `testPost` hook. This pre-existing issue is recorded separately from the layout changes. The slow broad rendering run was stopped after about 5m37s, and focused layout race coverage completed successfully.

A width-only resize now schedules a follow-up build after text is laid out, even when the cover and pane top stay in place. The regression advances only scheduled frames after `SetSize` (no test-forced `Frame` calls), so a missing follow-up cannot be hidden by the test helper.
