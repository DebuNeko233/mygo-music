module netease-music-mygo

go 1.27.3

tool github.com/egoist/mygo/cmd/mygo

require (
	github.com/ebitengine/purego v0.11.3
	github.com/egoist/mygo v0.8.3
	github.com/hajimehoshi/go-mp3 v0.3.4
	github.com/klauspost/compress v1.20.4
	github.com/mewkiz/flac v1.0.14
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	rsc.io/qr v0.2.0
)

require (
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/icza/bitio v1.1.0 // indirect
	github.com/mewkiz/pkg v0.0.0-20260703220044-4fb89b18cc87 // indirect
	github.com/mewpkg/term v0.0.0-20241026122259-37a80af23985 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// Performance backports on v0.8.3, pinned until they ship upstream.
replace github.com/egoist/mygo => github.com/DebuNeko233/mygo v0.8.4-0.20261110131907-e516b112f37c
