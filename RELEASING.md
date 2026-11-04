# Releasing

This repository, `zhangnjsd/mygo-music`, is where the app is released and
where installed apps look for updates: the repository the app came from was
deleted, and `mygo.json`'s `updates.github` points here. There is no other
remote to sync with.

1. Commit pending work, and run the tests:

   ```bash
   go test ./internal/...
   ```

2. Bump `version` in `mygo.json`, and add a `## <version>` section at the top of
   `CHANGELOG.md` saying, in Chinese, what changed for people using the app
   since the last release (`git log v0.1.24..HEAD`). Commit both in a commit
   titled just the version, e.g. `0.2.0`. MyGo's updater shows that section in
   the app's update window, and the release page takes it as its notes;
   `mygo build` fails without one.

3. Push main:

   ```bash
   git push origin main
   ```

4. Build and upload. `mygo build` makes the installers, `SHA256SUMS` and the
   install scripts, and `-upload` puts them in a draft release of the version's
   tag, with the changelog's section as its notes. `GH_TOKEN` is a token of this
   repository with `contents: write`. `MYGO_UPDATER_PRIVATE_KEY` is the key of
   `mygo keygen`: without it the build warns and signs no update archives, and
   installed apps then cannot update themselves.

   ```bash
   GH_TOKEN=... go tool mygo build \
     -platform windows/amd64,windows/arm64,linux/amd64,linux/arm64 -upload
   ```

   macOS needs a Mac, a Developer ID and the notary profile of `mygo.json`, so
   this repository releases Windows and Linux. Pushing a tag instead builds it
   in `.github/workflows/release.yml`, which covers macOS too and needs the same
   secrets.

5. Publish the draft, and mark it latest, so that `releases/latest/download/` of
   the install scripts points at it:

   ```bash
   gh release edit v0.2.0 --repo zhangnjsd/mygo-music --draft=false --latest
   ```

6. Check what the install script downloads:

   ```bash
   curl -sL https://github.com/zhangnjsd/mygo-music/releases/latest/download/SHA256SUMS
   ```

## The speech recognition libraries

Podcast captions run sherpa-onnx, whose libraries the app downloads with the
model from the `sherpa-onnx-<version>` release of this repository, not from the
app's own release: `internal/speech/natives.go` says where. They only change
with sherpa-onnx's version, in `scripts/sherpa`. Then pack them again, commit
the `internal/speech/natives.go` it writes, and upload them as a prerelease,
which never becomes the latest that updates look for:

```bash
go run ./scripts/sherpa
gh release create sherpa-onnx-2.0.1 --repo zhangnjsd/mygo-music \
  --prerelease --latest=false build/sherpa/*
```

Off a Mac, pack the platforms the release ships: `-platforms` leaves the macOS
libraries out, and an app there has no captions until they are packed on a Mac.

```bash
go run ./scripts/sherpa -platforms windows-amd64,windows-arm64,linux-amd64,linux-arm64
```

The macOS libraries are signed with a Developer ID before they are packed, which
`scripts/sherpa -sign` does on a Mac: the app's hardened runtime loads no other.
Packed anywhere else they are unsigned, and macOS refuses them.
