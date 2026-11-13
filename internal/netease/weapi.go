package netease

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
)

const (
	weapiKey       = "0CoJUm6Qyw8W8jud"
	weapiIV        = "0102030405060708"
	weapiAlphabet  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	weapiPublicKey = "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7"
)

var weapiRandom = rand.Reader

// weapiPost uses the web player's request protocol for operations its servers
// expect through that protocol, rather than the desktop client's eapi.
func weapiPost(path string, data map[string]any, out any) error {
	csrf := ""
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		if c.Name == "__csrf" {
			csrf = c.Value
		}
	}
	data["csrf_token"] = csrf
	form, err := weapiEncrypt(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", apiBase+path+"?csrf_token="+url.QueryEscape(csrf), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var cookies []string
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	return do(req, out)
}

// weapiEncrypt seals data as the web player does: two AES-CBC layers, with
// the random second key reversed and encrypted using NetEase's public RSA key.
func weapiEncrypt(data any) (url.Values, error) {
	text, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	random := make([]byte, 16)
	if _, err := io.ReadFull(weapiRandom, random); err != nil {
		return nil, err
	}
	key := make([]byte, 16)
	for i, n := range random {
		key[i] = weapiAlphabet[int(n)%len(weapiAlphabet)]
	}
	first, err := weapiAES(text, []byte(weapiKey))
	if err != nil {
		return nil, err
	}
	params, err := weapiAES([]byte(base64.StdEncoding.EncodeToString(first)), key)
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(key)-1; left < right; left, right = left+1, right-1 {
		key[left], key[right] = key[right], key[left]
	}
	modulus, _ := new(big.Int).SetString(weapiPublicKey, 16)
	padded := append(make([]byte, 128-len(key)), key...)
	secret := new(big.Int).Exp(new(big.Int).SetBytes(padded), big.NewInt(65537), modulus)
	return url.Values{
		"params":    {base64.StdEncoding.EncodeToString(params)},
		"encSecKey": {hex.EncodeToString(secret.FillBytes(make([]byte, 128)))},
	}, nil
}

func weapiAES(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(bytes.Clone(plain), bytes.Repeat([]byte{byte(padding)}, padding)...)
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte(weapiIV)).CryptBlocks(out, plain)
	return out, nil
}
