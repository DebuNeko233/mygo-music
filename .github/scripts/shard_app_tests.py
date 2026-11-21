import argparse
import sys


# Estimated durations in hundredths of a second. Seeded from ordinary
# go test -json runs; newly discovered and shorter tests use the default.
UNIT_TEST_COSTS = {
    "TestRapidCoverChangesAlwaysMove": 930,
    "TestLyricClickGlidesToReadingPosition": 660,
    "TestNowPlayingLyricsUseResizedHeightOnFirstFrame": 550,
    "TestWallGlidesAndStops": 490,
    "TestWallPlaysTheCoverSwaying": 370,
    "TestImmersiveIntroCreditsClearTheQuietCard": 300,
    "TestPaneCoverFollowsLatestSelection": 280,
    "TestNowPlayingRestoreAfterMinimize": 260,
    "TestCoverDirectionAfterShuffleAndWrap": 260,
    "TestCoveredPageKeepsItsPlace": 250,
    "TestNowPlayingOpensAtLastLyric": 240,
    "TestNowPlayingLyricScrollbarAlignmentAndDrag": 240,
    "TestWallShowsTheSongPlaying": 210,
    "TestLyricIntroFollowsReflow": 190,
    "TestRapidCoverChangesRestartTheEntrance": 180,
    "TestImmersiveLyricsFollowAfterRefresh": 160,
    "TestImmersiveFirstLyricAnchor": 140,
    "TestLyricCanvasAcrossTrackChanges": 130,
    "TestPlaylistCoversSurviveReturningToPage": 130,
    "TestArtistNameOpensTheirPage": 130,
    "TestNowPlayingClosesAsOneLayer": 120,
    "TestPlayerBarOpensComments": 110,
    "TestImmersiveLyricViewportTracksControls": 110,
    "TestExitHeartMode": 100,
}
UNIT_DEFAULT_COST = 7

# Estimates from Linux Actions race logs. The two cover-wall costs use
# measurements from the smaller-raster run.
RACE_TEST_COSTS = {
    "TestRapidCoverChangesAlwaysMove": 28884,
    "TestNowPlayingLyricsUseResizedHeightOnFirstFrame": 24666,
    "TestLyricClickGlidesToReadingPosition": 23116,
    "TestNowPlayingRestoreAfterMinimize": 11964,
    "TestCoveredPageKeepsItsPlace": 11224,
    "TestWallShowsTheSongPlaying": 10710,
    "TestPaneCoverFollowsLatestSelection": 8678,
    "TestCoverDirectionAfterShuffleAndWrap": 7236,
    "TestPlayerBarOpensComments": 6950,
    "TestArtistNameOpensTheirPage": 6198,
    "TestLyricIntroFollowsReflow": 5991,
    "TestNowPlayingLyricScrollbarAlignmentAndDrag": 5896,
    "TestImmersiveIntroCreditsClearTheQuietCard": 5849,
    "TestNowPlayingOpensAtLastLyric": 5808,
    "TestPlaylistCoversSurviveReturningToPage": 5468,
    "TestImmersiveLyricViewportTracksControls": 5457,
    "TestNowPlayingClosesAsOneLayer": 5411,
    "TestImmersiveLyricsFollowAfterRefresh": 5369,
    "TestRapidCoverChangesRestartTheEntrance": 5223,
    "TestLyricsReflowWhenWindowResizes": 4985,
    "TestSpaceAfterOpeningNowPlaying": 4754,
    "TestNowPlayingClockTicksEverySecond": 4687,
    "TestExitHeartMode": 4437,
    "TestImmersiveFirstLyricAnchor": 4431,
    "TestLyricCanvasAcrossTrackChanges": 4310,
    "TestPlayEpisode": 4029,
    "TestLocalQueueMenuRemovesEntry": 3829,
    "TestWallPlaysTheCoverSwaying": 3747,
    "TestNetworkChangesUpdateVisibleComponentStates": 3701,
    "TestWallGlidesAndStops": 3417,
    "TestNowPlayingIdlePointerFollowsWindowLeave": 3122,
    "TestWallPlaysTheCoverClicked": 3073,
    "TestLyricEdgeFadeScrolling": 2910,
    "TestLyricsWidthResizeSchedulesFollowUp": 2760,
    "TestLyricsFillATallWindow": 2475,
    "TestCmdNumberChoosesTab": 2384,
    "TestLyricsManualScrollStaysAfterResize": 2344,
    "TestOfflineLyricsStopAnimating": 2336,
    "TestSearchClearKeepsFocus": 2247,
    "TestNetworkLyricsRetryAction": 2207,
}
RACE_DEFAULT_COST = 161


def main():
    parser = argparse.ArgumentParser(description="Balance the discovered app tests")
    parser.add_argument("shard", type=int, help="1-based shard index")
    parser.add_argument("--shards", type=int, default=2, help="total number of shards")
    parser.add_argument("--race", action="store_true", help="use race detector timings")
    args = parser.parse_args()
    if not 1 <= args.shard <= args.shards:
        parser.error("shard index must be between 1 and the total number of shards")
    selected = args.shard - 1
    names = [
        line.strip()
        for line in sys.stdin
        if line.startswith(("Test", "Example", "Fuzz"))
    ]
    if len(names) < args.shards or len(names) != len(set(names)) or any(
        len(name.split()) != 1 for name in names
    ):
        sys.exit("Expected a list of unique top-level app tests")

    profile, default_cost = (
        (RACE_TEST_COSTS, RACE_DEFAULT_COST) if args.race
        else (UNIT_TEST_COSTS, UNIT_DEFAULT_COST)
    )
    costs = {name: profile.get(name, default_cost) for name in names}
    shards = [[] for _ in range(args.shards)]
    estimates = [0] * args.shards
    # Schedule the longest tests first, with stable tie-breaking so runners
    # independently compute the same split.
    for name in sorted(names, key=lambda name: (-costs[name], name)):
        index = min(range(len(shards)), key=estimates.__getitem__)
        shards[index].append(name)
        estimates[index] += costs[name]

    test_names = shards[selected]
    print(
        f"App shard {selected + 1}: {len(test_names)} of {len(names)} top-level tests",
        file=sys.stderr,
    )
    print("^(" + "|".join(test_names) + ")$")


if __name__ == "__main__":
    main()
