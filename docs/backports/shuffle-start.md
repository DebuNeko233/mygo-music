# Start whole-list shuffle on a random song

Adapted from EGOIST's [egoist/mygo-music commit `aabab0008cf60b5c31632151930931f5905c00e0`](https://github.com/egoist/mygo-music/commit/aabab0008cf60b5c31632151930931f5905c00e0) ([PR #96](https://github.com/egoist/mygo-music/pull/96)). This is an adapted backport, not a direct cherry-pick.

Target base: `zhangnjsd/mygo-music` commit `52e0184d258984e6f0184bd52c3a7bcabe8943e1`. This branch is independent of the other backports.

## Implementation

- Add `Player.PlayShuffled` and use it for the playlist and daily shuffle buttons.
- Shuffle the entire playback order; keep the source list unchanged. Explicit `PlayQueue` selections still start on the picked song.
- Retain this fork's decoder, retry and cache handling.

## Validation

- `CGO_ENABLED=0 go test ./...`
- `go tool mygo vet .`
- `CGO_ENABLED=1 go test -race ./internal/app ./internal/player -run 'Test(WholeListShuffle|ShufflePreserves)' -count=5`
- The actual playlist/daily buttons are exercised repeatedly. The UI regression also fails against the unchanged base because every shuffle starts with track 1.

No version, dependency, changelog or `mygo.json` changes are included.
