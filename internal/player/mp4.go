/*
将 MP4 解码器接入统一流接口，并显式管理 AAC 编解码器的释放。
*/

package player

import (
	"errors"
	"io"
	"sync"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/audio/mp4"
)

// MyGo's MP4 decoder does not expose its codec or a Close method. Hold this
// lock only until the decoder takes its AAC codec. Fragment discovery may wait
// for the network after that, independently of another song's initialization.
var mp4CodecOpen sync.Mutex

type ownedAACCodec struct {
	mp4.Codec
	close func() error
}

func (c *ownedAACCodec) Close() error {
	if c.close == nil {
		return nil
	}
	err := c.close()
	c.close = nil
	return err
}

func openMP4StreamSpectrum(dl *download, measure bool) (*decoderStream, error) {
	source, err := dl.decoderReader()
	if err != nil {
		return nil, err
	}
	// Metadata and codec creation precede ownership transfer under the lock.
	track, err := mp4.ReadTrack(source, source.Size())
	if err != nil {
		return nil, err
	}
	if track.Codec != "mp4a" {
		dec, err := mp4.NewDecoder(source)
		if err != nil {
			return nil, err
		}
		return newDecoderStream(dl, dec, measure), nil
	}
	codec, err := newAACCodec(track.Config)
	if err != nil {
		return nil, err
	}
	owned := &ownedAACCodec{Codec: codec}
	if closer, ok := codec.(io.Closer); ok {
		owned.close = closer.Close
	}
	registerAACCodec()
	mp4CodecOpen.Lock()
	release := sync.OnceFunc(func() {
		aacFactoryMu.Lock()
		aacFactory = newAACCodec
		aacFactoryMu.Unlock()
		mp4CodecOpen.Unlock()
	})
	defer release()
	aacFactoryMu.Lock()
	aacFactory = func(_ []byte) (mp4.Codec, error) {
		release()
		return owned, nil
	}
	aacFactoryMu.Unlock()
	dec, err := mp4.NewDecoder(source)
	if err != nil {
		return nil, errors.Join(err, owned.Close())
	}
	s := newDecoderStream(dl, dec, measure)
	s.closer = owned
	return s, nil
}

func newDecoderStream(dl *download, dec audio.Decoder, measure bool) *decoderStream {
	s := &decoderStream{dl: dl, dec: dec}
	if measure {
		s.spec = newSpectrum(dec.SampleRate())
	}
	return s
}
