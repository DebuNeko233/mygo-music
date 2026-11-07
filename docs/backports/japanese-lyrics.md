# Use consistent Japanese glyphs in lyrics

Adapted from EGOIST's [egoist/mygo-music commit `e67d70666de14f12adea669e2a246c5e48d81da2`](https://github.com/egoist/mygo-music/commit/e67d70666de14f12adea669e2a246c5e48d81da2) ([PR #90](https://github.com/egoist/mygo-music/pull/90)). This is an adapted backport, not a direct cherry-pick.

Target base: `zhangnjsd/mygo-music` commit `52e0184d258984e6f0184bd52c3a7bcabe8943e1`. This branch is independent of the other backports.

## Implementation

- Detect kana in the complete set of original lyrics and pass `ja` to text shaping, including kanji-only lines. Translations retain the application locale.
- Cover Now Playing, stage and wall, and extend the fix to this fork's desktop lyrics.
- Give desktop drawing, outline layers and width measurement the same language. Keep explicit user-selected font families.
- Lyrics without kana cannot be reliably identified as Japanese and retain the application locale.

## Validation

- `CGO_ENABLED=0 go test ./...`
- `go tool mygo vet .`
- `CGO_ENABLED=1 go test -race ./internal/app ./internal/lyrics -run 'Test(LyricsLanguage|LyricWindow|LyricOutline|LockingTheDeskLyrics)' -count=5`
- `NM_FRAMES=/absolute/output/path CGO_ENABLED=0 go test ./internal/app -run TestJapaneseLyricsFrames -count=5` (create the output directory first).
- Inspected all four generated surfaces on macOS with Chinese locale and Japanese originals; no font pixel assertions.
- Windows amd64 app test binary cross-compiles with CGO disabled. Native Windows rendering has not been tested.

No version, dependency, changelog or `mygo.json` changes are included.
