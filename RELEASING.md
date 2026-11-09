# Releasing

This repository, `zhangnjsd/mygo-music`, is where the app is released and
where installed apps look for updates: the repository the app came from was
deleted, and `mygo.json`'s `updates.github` points here. There is no other
remote to sync with.

1. Commit pending work, and run the tests:

   ```bash
   go test ./internal/...
   ```

2. Bump `version` in `mygo.json`, commit that alone, titled just the version,
   e.g. `0.2.6`, and push main. The version's changelog section comes in step 4;
   `mygo build` refuses to sign an update without one.

   ```bash
   git push origin main
   ```

3. Generate the notes in a draft release, on the repository's Releases page:
   "Draft a new release", the version's tag in "Choose a tag" (`v0.2.6`, which
   GitHub creates on publish, not before), the previous release as the tag to
   compare against (`v0.2.5`), then "Generate release notes", and save the
   draft. Nothing builds yet, because the tag does not exist.

   GitHub writes that body from the pull requests merged since that release, so
   changes pushed straight to main are not in it, and it reads as the pull
   requests were titled.

   Make this draft on the Releases page, not with `gh release create`: it
   creates the missing tag as it runs, which starts the workflow before
   `CHANGELOG.md` has the section.

4. Copy the draft's notes into `CHANGELOG.md`, under a new `## 0.2.6` heading at
   the top, unchanged except for the headings: `## What's Changed`, and any
   other `##` heading GitHub writes, become `###`. MyGo takes a version's
   section up to the next `## ` heading, and so does the release workflow; a
   `##` inside the section leaves it empty, and `mygo build` fails on it.
   Commit it, titled just the version, and push main.

   Do not reword it: that body is what the release page, the app's update
   window and `CHANGELOG.md` all show.

5. Push the version's tag. This builds and uploads:

   ```bash
   git tag v0.2.6
   git push origin v0.2.6
   ```

   `.github/workflows/release.yml` replaces the draft's body with the
   changelog's section, the same text, and uploads macOS, Windows and Linux to
   that draft. It needs `MYGO_UPDATER_PRIVATE_KEY` for updates, not Apple
   credentials.

   To build and upload from this machine instead, `mygo build` makes the
   installers, `SHA256SUMS` and the install scripts, and `-upload` puts them in
   the same draft, with the changelog's section as its notes. `GH_TOKEN` is a
   token of this repository with `contents: write`. `MYGO_UPDATER_PRIVATE_KEY`
   is the key of `mygo keygen`, matching the public key in `mygo.json`;
   `-upload` requires it to sign the automatic-update archives.

   ```bash
   GH_TOKEN=... go tool mygo build \
     -platform windows/amd64,windows/arm64,linux/amd64,linux/arm64 -upload
   ```

   macOS needs a Mac to build, but no Apple Developer certificate to package
   or publish. Build and Release use ad hoc signing and skip notarization:

   ```bash
   go tool mygo build -platform darwin/arm64,darwin/amd64 -sign - -skip-notarize
   ```

   These DMGs are not Developer ID signed or notarized, so macOS may ask the
   user to allow the app in Privacy & Security.

6. Publish the draft, and mark it latest, so that `releases/latest/download/` of
   the install scripts points at it:

   ```bash
   gh release edit v0.2.6 --repo zhangnjsd/mygo-music --draft=false --latest
   ```

7. Check what the install script downloads:

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
go run ./scripts/sherpa -sign -
gh release create sherpa-onnx-2.0.1 --repo zhangnjsd/mygo-music \
  --prerelease --latest=false build/sherpa/*
```

Off a Mac, pack the platforms the release ships: `-platforms` leaves the macOS
libraries out, and an app there has no captions until they are packed on a Mac.

```bash
go run ./scripts/sherpa -platforms windows-amd64,windows-arm64,linux-amd64,linux-arm64
```

On a Mac, `-sign -` uses ad hoc signing, without an Apple Developer certificate,
for an app that is also ad hoc signed. If the app uses Developer ID signing
with hardened runtime library validation, pass its Developer ID instead so
that the libraries and app belong to the same team.

Keep the default platforms when regenerating the manifest to retain Windows
and Linux too. To add macOS to an existing library release, upload its four
new library assets before shipping the manifest that refers to them:

```bash
gh release upload sherpa-onnx-2.0.1 --repo zhangnjsd/mygo-music \
  build/sherpa/darwin-*.gz
```
