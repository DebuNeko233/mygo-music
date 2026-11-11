package netease

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/chacha20"
)

// An unacknowledged NCBL upload is a failed report, even when the server
// responds with HTTP 200.

// The desktop BI logger sends separate PLV and PLD encrypted uploads and
// accepts the logger's encrypted multipart payload.
func TestDawnUploadAcknowledgesEachRecord(t *testing.T) {
	oldRandom := ncblRandom
	random := append(bytes.Repeat([]byte{1}, 32), []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2}...)
	ncblRandom = bytes.NewReader(append(random, random...))
	t.Cleanup(func() { ncblRandom = oldRandom })
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "clientlog3.music.163.com" || req.URL.Path != "/api/clientlog/encrypt/upload" {
			t.Fatalf("unexpected upload URL %s", req.URL)
		}
		_, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		form := multipart.NewReader(req.Body, params["boundary"])
		part, err := form.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() != "file" || !strings.HasPrefix(part.FileName(), "op_") {
			t.Fatalf("invalid log upload file: %s %s", part.FormName(), part.FileName())
		}
		payload, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		fields := decodeDawnRecord(t, payload)
		if fields["sourceId"] != "3778678" || fields["source"] != "list" || fields["bitrate"] != float64(320) || fields["bitrate_level"] != "exhigh" {
			t.Fatalf("play fields changed: %#v", fields)
		}
		if !strings.Contains(fields["_addrefer"].(string), "page_pc_songlist_songflow") {
			t.Fatalf("playlist navigation was lost: %#v", fields["_addrefer"])
		}
		if timeValue, ok := fields["time"]; ok && timeValue != float64(35) {
			t.Fatalf("wrong played time: %#v", timeValue)
		}
		body := `{"code":200,"data":{"successfiles":["` + part.FileName() + `"]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	source := PlaySource{Source: "list", ID: 3778678}
	if err := ReportDawnStart(Track{ID: 7, Duration: 100000}, 320, "exhigh", source); err != nil {
		t.Fatal(err)
	}
	if err := ReportDawnEnd(Track{ID: 7, Duration: 100000}, 35*time.Second, "playend", 320, "exhigh", source); err != nil {
		t.Fatal(err)
	}
}

// Fields for an unknown queue or stream remain absent, rather than claiming a
// playlist zero or exhigh audio that was never loaded.
func TestDawnOmitsUnknownSourceAndStream(t *testing.T) {
	fields := dawnFields(Track{ID: 7}, 0, "", PlaySource{})
	for _, key := range []string{"source", "sourceId", "alg", "sourcetype", "_addrefer"} {
		if _, ok := fields[key]; ok {
			t.Errorf("invented %s: %#v", key, fields[key])
		}
	}
	if fields["bitrate"] != 0 || fields["bitrate_level"] != "" {
		t.Fatalf("invented stream quality: %#v", fields)
	}
}

func decodeDawnRecord(t *testing.T, packet []byte) map[string]any {
	t.Helper()
	key := bytes.Repeat([]byte{1}, 32)
	uuid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	uuid[6] = uuid[6]&0x0f | 0x40
	uuid[8] = uuid[8]&0x3f | 0x80
	if !bytes.Equal(packet[10:26], uuid) {
		t.Fatal("record nonce changed")
	}
	nonce := packet[10:22]
	counter := binary.LittleEndian.Uint32(packet[22:26]) >> 2
	modulus, _ := new(big.Int).SetString(ncblModulusHex, 16)
	keyB := new(big.Int).Exp(new(big.Int).SetBytes(key), big.NewInt(65537), modulus).Bytes()
	keyB = append(make([]byte, 32-len(keyB)), keyB...)
	metaCipher, err := chachaDecode(keyB, counter, nonce, packet[74:74+int(binary.LittleEndian.Uint16(packet[72:]))])
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaCipher, &meta); err != nil {
		t.Fatal(err)
	}
	metaLen := int(binary.LittleEndian.Uint16(packet[72:]))
	frames := packet[74+metaLen:]
	if len(frames) < 6 {
		t.Fatal("missing record frame")
	}
	n := int(binary.LittleEndian.Uint16(frames))
	compressed, err := chachaDecode(key, counter, nonce, frames[6:6+n])
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	record, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(record), "\x01")
	if len(parts) != 3 {
		t.Fatalf("bad record: %q", record)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(parts[2])), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func chachaDecode(key []byte, counter uint32, nonce, data []byte) ([]byte, error) {
	cipher, err := chacha20.NewUnauthenticatedCipher(key, nonce)
	if err != nil {
		return nil, err
	}
	cipher.SetCounter(counter)
	out := make([]byte, len(data))
	cipher.XORKeyStream(out, data)
	return out, nil
}

func TestDawnUploadRequiresFilenameAcknowledgement(t *testing.T) {
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"successfiles":[]}}`)), Header: make(http.Header)}, nil
	})
	if err := ReportDawnStart(Track{ID: 7}, 320, "exhigh", PlaySource{}); err == nil {
		t.Fatal("an unacknowledged upload was treated as sent")
	}
}
