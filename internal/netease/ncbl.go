package netease

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/klauspost/compress/zstd"
)

// NCBL is the desktop BI logger's encrypted container. KEY_A encrypts the
// compressed records; raw RSA wraps it into KEY_B, which also encrypts the
// device metadata. Both use the UUID as their ChaCha20 nonce and counter.
var ncblModulus, _ = new(big.Int).SetString("fd90bd466ff9bc8a3fec2fbcf263b90d5c564879fa5d7aab89b31c1d5cb4139d", 16)

func ncblEncrypt(meta, body []byte) ([]byte, error) {
	keyA := make([]byte, 32)
	uuid := make([]byte, 16)
	if _, err := rand.Read(keyA); err != nil {
		return nil, err
	}
	if _, err := rand.Read(uuid); err != nil {
		return nil, err
	}
	if keyA[0] >= 0xa3 {
		keyA[0] = 0xa2
	}
	uuid[6] = uuid[6]&0x0f | 0x40
	uuid[8] = uuid[8]&0x3f | 0x80
	var seqBytes [2]byte
	if _, err := rand.Read(seqBytes[:]); err != nil {
		return nil, err
	}
	return ncblEncode(meta, body, keyA, uuid, binary.LittleEndian.Uint16(seqBytes[:]))
}

func ncblEncode(meta, body, keyA, uuid []byte, baseSeq uint16) ([]byte, error) {
	if len(keyA) != 32 || len(uuid) != 16 {
		return nil, fmt.Errorf("invalid NCBL key or UUID")
	}
	keyB := new(big.Int).Exp(new(big.Int).SetBytes(keyA), big.NewInt(65537), ncblModulus).Bytes()
	keyB = append(make([]byte, 32-len(keyB)), keyB...)
	nonce, counter := uuid[:12], binary.LittleEndian.Uint32(uuid[12:])>>2

	metaCipher, err := ncblChaCha(keyB, counter, nonce, meta)
	if err != nil {
		return nil, err
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	compressed := encoder.EncodeAll(body, nil)
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

// ncblChaCha is RFC 8439 ChaCha20 with the desktop logger's initial counter.
func ncblChaCha(key []byte, counter uint32, nonce, src []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 12 {
		return nil, fmt.Errorf("invalid ChaCha20 key or nonce")
	}
	state := [16]uint32{0x61707865, 0x3320646e, 0x79622d32, 0x6b206574}
	for i := 0; i < 8; i++ {
		state[4+i] = binary.LittleEndian.Uint32(key[4*i:])
	}
	state[12] = counter
	for i := 0; i < 3; i++ {
		state[13+i] = binary.LittleEndian.Uint32(nonce[4*i:])
	}
	out := make([]byte, len(src))
	for offset := 0; offset < len(src); offset += 64 {
		work := state
		quarter := func(a, b, c, d int) {
			work[a] += work[b]
			work[d] ^= work[a]
			work[d] = work[d]<<16 | work[d]>>16
			work[c] += work[d]
			work[b] ^= work[c]
			work[b] = work[b]<<12 | work[b]>>20
			work[a] += work[b]
			work[d] ^= work[a]
			work[d] = work[d]<<8 | work[d]>>24
			work[c] += work[d]
			work[b] ^= work[c]
			work[b] = work[b]<<7 | work[b]>>25
		}
		for i := 0; i < 10; i++ {
			quarter(0, 4, 8, 12)
			quarter(1, 5, 9, 13)
			quarter(2, 6, 10, 14)
			quarter(3, 7, 11, 15)
			quarter(0, 5, 10, 15)
			quarter(1, 6, 11, 12)
			quarter(2, 7, 8, 13)
			quarter(3, 4, 9, 14)
		}
		for i := 0; i < 16; i++ {
			var word [4]byte
			binary.LittleEndian.PutUint32(word[:], work[i]+state[i])
			for j, b := range word {
				if at := offset + 4*i + j; at < len(src) {
					out[at] = src[at] ^ b
				}
			}
		}
		state[12]++
	}
	return out, nil
}
