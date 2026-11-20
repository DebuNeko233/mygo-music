package player

import (
	"errors"
	"io"
	"sync"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/audio/mp4"
)

// MyGo's MP4 decoder does not expose its codec or a Close method. Hold this
// lock while opening a local file so its AAC factory can return the decoder
// which needs to be destroyed with the stream. Other codecs have no external
// resources and therefore need no ownership transfer.
var localMP4Open sync.Mutex

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

func openMP4Stream(dl *download) (*decoderStream, error) {
	dl.mu.Lock()
	file := dl.file
	dl.mu.Unlock()
	if file == nil {
		return nil, io.ErrClosedPipe
	}
	localMP4Open.Lock()
	defer localMP4Open.Unlock()
	var owned *ownedAACCodec
	registerLocalAACCodec()
	localAACFactoryMu.Lock()
	localAACFactory = func(config []byte) (mp4.Codec, error) {
		codec, err := newLocalAACCodec(config)
		if err != nil {
			return nil, err
		}
		owned = &ownedAACCodec{Codec: codec}
		if closer, ok := codec.(io.Closer); ok {
			owned.close = closer.Close
		}
		return owned, nil
	}
	localAACFactoryMu.Unlock()
	dec, err := mp4.NewDecoder(io.NewSectionReader(file, 0, dl.size))
	localAACFactoryMu.Lock()
	localAACFactory = newLocalAACCodec
	localAACFactoryMu.Unlock()
	if err != nil {
		if owned != nil {
			return nil, errors.Join(err, owned.Close())
		}
		return nil, err
	}
	s := newDecoderStream(dl, dec)
	if owned != nil {
		s.closer = owned
	}
	return s, nil
}

func newDecoderStream(dl *download, dec audio.Decoder) *decoderStream {
	return &decoderStream{dl: dl, dec: dec, spec: newSpectrum(dec.SampleRate())}
}
