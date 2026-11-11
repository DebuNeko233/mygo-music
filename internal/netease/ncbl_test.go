package netease

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// The encrypted container can be opened by its fixed RSA-wrapped key and
// preserves the metadata, frames and compressed desktop BI log records.
func TestNCBLContainer(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	uuid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	plain := bytes.Repeat([]byte("desktop play records\n"), 4000)
	packet, err := ncblEncode([]byte(`{"deviceId":"test"}`), plain, key, uuid, 17)
	if err != nil {
		t.Fatal(err)
	}
	if string(packet[:4]) != "NCBL" || binary.LittleEndian.Uint32(packet[4:]) != 3 {
		t.Fatal("wrong NCBL container header")
	}
	keyB := new(big.Int).Exp(new(big.Int).SetBytes(key), big.NewInt(65537), ncblModulus).Bytes()
	if !bytes.Equal(packet[26:58], append(make([]byte, 32-len(keyB)), keyB...)) {
		t.Fatal("key is not RSA wrapped")
	}
	length := int(binary.LittleEndian.Uint16(packet[72:]))
	nonce := uuid[:12]
	counter := binary.LittleEndian.Uint32(uuid[12:]) >> 2
	meta, err := ncblChaCha(packet[26:58], counter, nonce, packet[74:74+length])
	if err != nil || string(meta) != `{"deviceId":"test"}` {
		t.Fatalf("metadata %q: %v", meta, err)
	}
	frames := packet[74+length:]
	if int(binary.LittleEndian.Uint32(packet[66:])) != len(frames) {
		t.Fatal("wrong trailing frame length")
	}
	var compressed []byte
	for len(frames) > 0 {
		n := int(binary.LittleEndian.Uint16(frames))
		cipher := frames[6 : 6+n]
		plain, err := ncblChaCha(key, counter, nonce, cipher)
		if err != nil {
			t.Fatal(err)
		}
		compressed = append(compressed, plain...)
		frames = frames[6+n:]
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(compressed, nil)
	if err != nil || !bytes.Equal(decoded, plain) {
		t.Fatalf("round trip differed: %v", err)
	}
}

// NCBL's stream cipher is RFC 8439 ChaCha20, including its block counter.
func TestNCBLChaChaVector(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	nonce, _ := hex.DecodeString("000000090000004a00000000")
	cipher, err := ncblChaCha(key, 1, nonce, make([]byte, 64))
	if err != nil {
		t.Fatal(err)
	}
	want := "10f1e7e4d13b5915500fdd1fa32071c4c7d1f4c733c068030422aa9ac3d46c4e" +
		"d2826446079faa0914c2d705d98b02a2b5129cd1de164eb9cbd083e8a2503c4e"
	if hex.EncodeToString(cipher) != want {
		t.Fatalf("ChaCha20 produced %x", cipher)
	}
}
