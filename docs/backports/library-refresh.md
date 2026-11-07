# Refresh playlists and liked songs after external changes

Adapted from EGOIST's [egoist/mygo-music commit `175380d1d1f054ce356f0a3f91de894874ad3e5f`](https://github.com/egoist/mygo-music/commit/175380d1d1f054ce356f0a3f91de894874ad3e5f) ([PR #13](https://github.com/egoist/mygo-music/pull/13)). This is an adapted backport, not a direct cherry-pick.

Target base: `zhangnjsd/mygo-music` commit `52e0184d258984e6f0184bd52c3a7bcabe8943e1`. This branch is independent of the other backports.

## Implementation

- Adapt the source's plugin-based behavior to this fork's direct NetEase calls.
- Reopening a playlist or returning to the foreground refreshes expired data in the background. Foreground refresh covers sidebar playlists, liked IDs and the visible playlist; reopening a playlist also refreshes liked IDs.
- Keep old content during requests/failures and leave the playback queue unchanged. Use a 30-second freshness interval and coalesce concurrent requests; failures of cached refreshes observe the same interval. There is no periodic poll.
- Reject responses from prior account epochs or invalidated playlist requests. Snapshot mutable playlist data and track pending local edits/revisions so stale replies cannot undo rename, delete, subscription, track or like edits.
- Refresh metadata stays on the UI thread. Workers only perform network calls and publish through the existing `post` mechanism.

## Validation

- `CGO_ENABLED=0 go test ./...`
- `go tool mygo vet .`
- `CGO_ENABLED=1 go test -race ./internal/app -run 'Test(ReopenedPlaylistRefreshes|LibraryRefresh|PlaylistPlaysWhenItLands)' -count=5`
- Test actual window activation, cached reopening, expiry, coalescing, failures, liked IDs, account A-B-A, invalidated loads and optimistic edits.
- The reopening regression fails on the unchanged base: the cached playlist is never requested again.
- Network responses are mocked; authenticated live NetEase behavior has not been tested.

No version, dependency, changelog or `mygo.json` changes are included.
