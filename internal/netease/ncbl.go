package netease

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/chacha20"
)

// NCBL is the desktop BI logger's encrypted container. KEY_A encrypts the
// compressed records; raw RSA wraps it into KEY_B, which also encrypts the
// device metadata. Both use the UUID as their ChaCha20 nonce and counter.
const ncblModulusHex = "fd90bd466ff9bc8a3fec2fbcf263b90d5c564879fa5d7aab89b31c1d5cb4139d"

var ncblRandom io.Reader = rand.Reader

func ncblEncrypt(meta, body []byte) ([]byte, error) {
	keyA := make([]byte, 32)
	uuid := make([]byte, 16)
	if _, err := io.ReadFull(ncblRandom, keyA); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(ncblRandom, uuid); err != nil {
		return nil, err
	}
	if keyA[0] >= 0xa3 {
		keyA[0] = 0xa2
	}
	uuid[6] = uuid[6]&0x0f | 0x40
	uuid[8] = uuid[8]&0x3f | 0x80
	var seqBytes [2]byte
	if _, err := io.ReadFull(ncblRandom, seqBytes[:]); err != nil {
		return nil, err
	}
	return ncblEncode(meta, body, keyA, uuid, binary.LittleEndian.Uint16(seqBytes[:]))
}

func ncblEncode(meta, body, keyA, uuid []byte, baseSeq uint16) ([]byte, error) {
	if len(keyA) != 32 || len(uuid) != 16 {
		return nil, fmt.Errorf("invalid NCBL key or UUID")
	}
	modulus, ok := new(big.Int).SetString(ncblModulusHex, 16)
	if !ok {
		return nil, fmt.Errorf("invalid NCBL modulus")
	}
	keyB := new(big.Int).Exp(new(big.Int).SetBytes(keyA), big.NewInt(65537), modulus).Bytes()
	keyB = append(make([]byte, 32-len(keyB)), keyB...)
	nonce, counter := uuid[:12], binary.LittleEndian.Uint32(uuid[12:])>>2

	metaCipher, err := ncblChaCha(keyB, counter, nonce, meta)
	if err != nil {
		return nil, err
	}
	compressed, err := ncblZstd(body)
	if err != nil {
		return nil, err
	}
	var frames bytes.Buffer
	seq := uint32(baseSeq)
	last := seq
	for offset := 0; offset < len(compressed) || offset == 0; offset += 0x8000 {
		end := min(offset+0x8000, len(compressed))
		cipher, err := ncblChaCha(keyA, counter, nonce, compressed[offset:end])
		if err != nil {
			return nil, err
		}
		var head [6]byte
		binary.LittleEndian.PutUint16(head[:2], uint16(len(cipher)))
		binary.LittleEndian.PutUint32(head[2:], seq)
		frames.Write(head[:])
		frames.Write(cipher)
		last = seq
		seq++
		if len(compressed) == 0 {
			break
		}
	}
	var out bytes.Buffer
	out.WriteString("NCBL")
	binary.Write(&out, binary.LittleEndian, uint32(3))
	binary.Write(&out, binary.LittleEndian, uint16(70+4+len(metaCipher)))
	out.Write(uuid)
	out.Write(keyB)
	binary.Write(&out, binary.LittleEndian, uint32(baseSeq))
	binary.Write(&out, binary.LittleEndian, last)
	binary.Write(&out, binary.LittleEndian, uint32(frames.Len()))
	binary.Write(&out, binary.LittleEndian, uint16(0x4343))
	binary.Write(&out, binary.LittleEndian, uint16(len(metaCipher)))
	out.Write(metaCipher)
	out.Write(frames.Bytes())
	return out.Bytes(), nil
}

func ncblZstd(body []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(body, nil), nil
}

// ncblChaCha is RFC 8439 ChaCha20 with the desktop logger's initial counter.
func ncblChaCha(key []byte, counter uint32, nonce, src []byte) ([]byte, error) {
	cipher, err := chacha20.NewUnauthenticatedCipher(key, nonce)
	if err != nil {
		return nil, err
	}
	cipher.SetCounter(counter)
	out := make([]byte, len(src))
	cipher.XORKeyStream(out, src)
	return out, nil
}
