# Bound spectrum storage for long and unknown-length audio

Adapted from EGOIST's [egoist/mygo-music commit `0927157ca1a8bf73935d5d18d5ee17c86fbc1a0f`](https://github.com/egoist/mygo-music/commit/0927157ca1a8bf73935d5d18d5ee17c86fbc1a0f) ([PR #22](https://github.com/egoist/mygo-music/pull/22)). This is an adapted backport, not a direct cherry-pick.

Target base: `zhangnjsd/mygo-music` commit `52e0184d258984e6f0184bd52c3a7bcabe8943e1`. This branch is independent of the other backports.

## Implementation

- Replace duration-sized spectrum tables with a ring holding approximately ten seconds of decoded chunks.
- Store each slot's absolute chunk index, rejecting overwritten or not-yet-decoded chunks; seeking measures the newly decoded samples again.
- Update both MP3 and FLAC constructors. Retain the existing FFT and locking.
- Storage scales with sample rate, not the track duration; unknown-duration audio no longer stops after the first two chunks.

## Validation

- `CGO_ENABLED=0 go test ./...`
- `go tool mygo vet .`
- `CGO_ENABLED=1 go test -race ./internal/player -run TestSpectrum -count=5`
- Cover ten-hour offsets, eviction, future lookups, seeking back, and bounded allocation at 44.1/192 kHz.
- A constructor-adapted behavior probe fails on the unchanged base (about 150 MB allocated for ten hours; unknown-duration measurements stop) and passes on this branch. This measures spectrum allocation, not whole-app RSS.

No version, dependency, changelog or `mygo.json` changes are included.
