import sys


# Estimated durations in hundredths of a second. Seeded from ordinary
# go test -json runs; newly discovered and shorter tests use the default.
TEST_COSTS = {
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
DEFAULT_COST = 7


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ("1", "2"):
        sys.exit("Usage: shard_app_tests.py {1|2} < go-test-list")
    selected = int(sys.argv[1]) - 1
    names = [
        line.strip()
        for line in sys.stdin
        if line.startswith(("Test", "Example", "Fuzz"))
    ]
    if len(names) < 2 or len(names) != len(set(names)) or any(
        len(name.split()) != 1 for name in names
    ):
        sys.exit("Expected a list of unique top-level app tests")

    costs = {name: TEST_COSTS.get(name, DEFAULT_COST) for name in names}
    shards = [[], []]
    estimates = [0, 0]
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
