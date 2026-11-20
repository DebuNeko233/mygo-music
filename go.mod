module netease-music-mygo

go 1.27.4

tool github.com/egoist/mygo/cmd/mygo

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/ebitengine/purego v0.11.3
	github.com/egoist/mygo v0.10.0
	github.com/hajimehoshi/go-mp3 v0.3.4
	github.com/jfreymuth/oggvorbis v1.0.5
	github.com/klauspost/compress v1.20.4
	github.com/llehouerou/go-faad2 v0.3.1
	github.com/mewkiz/flac v1.0.14
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0
	rsc.io/qr v0.2.0
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/icza/bitio v1.1.0 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/mewkiz/pkg v0.0.0-20260703220044-4fb89b18cc87 // indirect
	github.com/mewpkg/term v0.0.0-20241026122259-37a80af23985 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/tphakala/go-aac v0.7.2 // indirect
	github.com/tphakala/simd v1.15.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/net v0.62.0 // indirect
)

// Retained lyric layers and macOS frame-ready fixes, pinned to a tested commit.
replace github.com/egoist/mygo => github.com/DebuNeko233/mygo v0.10.1-0.20261120193717-667973527550
