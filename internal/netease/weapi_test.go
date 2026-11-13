package netease

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Liking a song uses the regular song endpoint; radio/like belongs to private FM
// and NetEase rejects its web requests with -460.
func TestLikeUsesSongEndpoint(t *testing.T) {
	oldTransport := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = oldTransport })
	oldRandom := weapiRandom
	weapiRandom = bytes.NewReader(bytes.Repeat([]byte{0}, 16))
	t.Cleanup(func() { weapiRandom = oldRandom })
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Value: "session-value", Path: "/"},
		{Name: "__csrf", Value: "csrf-value", Path: "/"},
	})

	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://music.163.com/weapi/song/like?csrf_token=csrf-value" {
			t.Errorf("like request URL = %s", req.URL)
		}
		if cookie := req.Header.Get("Cookie"); !strings.Contains(cookie, "MUSIC_U=session-value") || !strings.Contains(cookie, "__csrf=csrf-value") {
			t.Errorf("like request cookie = %q", cookie)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		key := []byte("aaaaaaaaaaaaaaaa")
		params, err := base64.StdEncoding.DecodeString(req.Form.Get("params"))
		if err != nil {
			t.Fatal(err)
		}
		params, err = weapiDecrypt(t, params, key)
		if err != nil {
			t.Fatal(err)
		}
		params, err = base64.StdEncoding.DecodeString(string(params))
		if err != nil {
			t.Fatal(err)
		}
		params, err = weapiDecrypt(t, params, []byte(weapiKey))
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal(params, &data); err != nil {
			t.Fatal(err)
		}
		for field, want := range map[string]any{
			"trackId": float64(42), "like": true, "csrf_token": "csrf-value",
		} {
			if data[field] != want {
				t.Errorf("%s = %#v, want %#v", field, data[field], want)
			}
		}
		if req.Form.Get("encSecKey") != "d473b9eca232f1b4090dd606b0df86de318748dd2eec307e4ed4345030fc4ee30331e598f41d5a6f5befaab94630ea1a1eda7cfade84fbec1a907913d2e4d2c8744bc572b99a050075e075b4537f645ecfa994f95906c32818e076aeda6bdb906bfa0bb96c4cf4bc3ed6d9ab76cf08441153d9d85e1ea3d78fa8d9210d581cee" {
			t.Errorf("encSecKey = %s", req.Form.Get("encSecKey"))
		}
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":200}`)),
		}, nil
	})
	if err := Like(42, true); err != nil {
		t.Fatal(err)
	}
}

func weapiDecrypt(t *testing.T, ciphertext, key []byte) ([]byte, error) {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		t.Fatal("invalid AES ciphertext length")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, []byte(weapiIV)).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding == 0 || padding > aes.BlockSize {
		t.Fatal("invalid AES padding")
	}
	return plain[:len(plain)-padding], nil
}
