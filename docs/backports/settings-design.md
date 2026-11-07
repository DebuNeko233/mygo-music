# Settings sidebar and search

Adapt the settings design from EGOIST's [egoist/mygo-music](https://github.com/egoist/mygo-music), inspected at [1af0d8efd77fdf630c27cfc635f12e9cb089ce10](https://github.com/egoist/mygo-music/commit/1af0d8efd77fdf630c27cfc635f12e9cb089ce10).

Design sources:

- [ceed8343de1f1036b4c2c9d76d3f529c05580bb8](https://github.com/egoist/mygo-music/commit/ceed8343de1f1036b4c2c9d76d3f529c05580bb8): category sidebar, cross-page search, hidden title bar and scroll-edge header.
- [9ebbc6d33f0576bc6a131c27945e6944f44b1aaf](https://github.com/egoist/mygo-music/commit/9ebbc6d33f0576bc6a131c27945e6944f44b1aaf): the whole search field, including its icon, focuses the input.

This is an adapted design port. The branch starts at the local four-backport merge `88cc584a2211172830676acc6c32d8eb372722a4`, preserving those changes.

## Behavior

The 780 × 640 settings window has a 200-point sidebar, a search field and a right-hand page of setting cards. Its minimum size is 640 × 420. Navigation scrolls independently when this fork's extra categories do not fit. The page header uses the existing hard scroll edge and leaves room for native window controls. A new window state key avoids restoring the old narrow settings window's dimensions.

Search matches category titles, setting names and descriptions. Selecting a category exits search; Command/Ctrl+F focuses the search field. Each category's definition reports metadata through `settingsRow` and `settingsBlock`; discovery does not execute their drawing/control callbacks. Only matching controls are built, with the existing preference application and persistence behavior. Navigation/search state remains in memory, and no UI context or element is stored in app state or worker callbacks.

The direct NetEase account, theme colors, background images, cache-directory migration, desktop lyric fonts/colors/outline and all other existing controls remain. This change ports settings presentation and search, without adding the sibling project's plugin system, language system or other unrelated settings. No version, dependency, changelog or `mygo.json` changes are included.

## Validation

```sh
CGO_ENABLED=0 go test ./...
go tool mygo vet .
CGO_ENABLED=1 go test -race ./internal/app -run 'Test(SettingsDesign|CacheDirectory|ChoosingAccent|SettingsSliders|TheSettingsChoose|TurnOffUpdate|CheckForUpdatesFromSettings)' -count=5
```

The UI tests exercise navigation, cross-page search, applying and saving controls, clearing background pictures from search, clicking the search icon, resizing and changing pages after scrolling. Existing cache migration, accent, desktop lyrics and updater tests now navigate to their respective categories. Set `NM_FRAMES` to an existing output directory to generate `TestSettingsDesignFrames` screenshots for light/dark and normal/minimum sizes. Inspect these visually instead of asserting font pixels.
