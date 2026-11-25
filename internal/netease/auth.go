package netease

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// KeyringHelp explains, in Markdown, how to install and enable a Secret
// Service keyring on each desktop so that the session persists across
// restarts. The login dialog offers to copy it when there is no store.
//
//go:embed keyring_help.md
var KeyringHelp string

// The listener's session is NetEase's login cookie, MUSIC_U, which every
// request carries once signed in. The app keeps it in the system's
// keychain. Development builds keep it in a private file instead: each
// rebuild is signed anew, and the keychain would ask before every one of
// them read it.

const sessionSecret = "netease-session"

// ErrNoKeyring wraps a session save failure when the system has no
// credential store at all, rather than a store that refused one write.
// On Linux that means no Secret Service: no keyring daemon such as
// GNOME Keyring, KWallet or KeePassXC answers org.freedesktop.secrets.
// The login keeps working for this run; the listener just has to sign
// in again after a restart until a keyring is installed and running.
var ErrNoKeyring = errors.New("no credential store")

func IsNoKeyring(err error) bool {
	return errors.Is(err, ErrNoKeyring) ||
		(errors.Is(err, errors.ErrUnsupported) && !errors.Is(err, mygo.ErrSecretNotFound))
}

// A pending save must finish before sign-out removes its credentials.
var sessionMu sync.Mutex

func sessionFile() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session"), nil
}

func readSession() ([]byte, error) {
	if !mygo.IsDev() {
		return mygo.Secrets.Get(sessionSecret)
	}
	path, err := sessionFile()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeSession(b []byte) error {
	if !mygo.IsDev() {
		return mygo.Secrets.Set(sessionSecret, b)
	}
	path, err := sessionFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func deleteSession() error {
	if !mygo.IsDev() {
		return mygo.Secrets.Delete(sessionSecret)
	}
	path, err := sessionFile()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

var siteURL, _ = url.Parse(apiBase)

func init() {
	httpClient.Jar, _ = cookiejar.New(nil)
}

// RestoreSession puts the session from the keychain in the cookie jar, and
// reports whether there was one.
func RestoreSession() bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	b, err := readSession()
	if err != nil || len(b) == 0 {
		return false
	}
	var cookies []*http.Cookie
	for _, part := range strings.Split(string(b), ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
		}
	}
	httpClient.Jar.SetCookies(siteURL, cookies)
	return true
}

// SaveSession keeps the login cookies of the jar in the keychain.
func SaveSession() error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	var parts []string
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		if c.Name == "MUSIC_U" || c.Name == "__csrf" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	if len(parts) == 0 {
		return errors.New("no session cookie")
	}
	if err := writeSession([]byte(strings.Join(parts, "; "))); err != nil {
		if IsNoKeyring(err) {
			return fmt.Errorf("netease: save session: %w: %v", ErrNoKeyring, err)
		}
		return err
	}
	return nil
}

// ClearSession forgets the session, here and in the keychain.
func ClearSession() error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Path: "/", MaxAge: -1},
		{Name: "__csrf", Path: "/", MaxAge: -1},
	})
	if err := deleteSession(); err != nil {
		return fmt.Errorf("netease: clear session: %w", err)
	}
	return nil
}

type Profile struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

// Account returns the profile of who is signed in, or nil.
func Account() (*Profile, error) {
	return accountWithClient(httpClient)
}

func accountWithClient(client *http.Client) (*Profile, error) {
	var res struct {
		Profile *Profile `json:"profile"`
	}
	req, err := http.NewRequest("GET", apiBase+"/api/nuser/account/get", nil)
	if err != nil {
		return nil, err
	}
	err = doWithClient(client, req, &res)
	return res.Profile, err
}

// QRLogin holds one attempt's cookies until the app accepts the confirmation.
type QRLogin struct {
	jar *qrCookieJar
}

type qrCookieJar struct {
	http.CookieJar
	changes []qrCookieChange
}

type qrCookieChange struct {
	url     *url.URL
	cookies []*http.Cookie
}

func (j *qrCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.CookieJar.SetCookies(u, cookies)
	j.changes = append(j.changes, qrCookieChange{u, cookies})
}

func NewQRLogin() *QRLogin {
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(siteURL, httpClient.Jar.Cookies(siteURL))
	return &QRLogin{jar: &qrCookieJar{CookieJar: jar}}
}

// Commit publishes cookies received during this attempt to the shared session.
func (l *QRLogin) Commit() {
	for _, ch := range l.jar.changes {
		httpClient.Jar.SetCookies(ch.url, ch.cookies)
	}
}

func (l *QRLogin) post(path string, data map[string]any, out any) error {
	endpoint := eapiDomain + "/eapi/" + strings.TrimPrefix(path, "/api/")
	return eapiPostEndpoint(context.Background(), endpoint, path, data, out, l.jar)
}

// Key starts a QR code login: the code shows LoginURL(key). The login
// goes through the eapi, as the desktop client's does.
func (l *QRLogin) Key() (string, error) {
	var res struct {
		Code   int    `json:"code"`
		Unikey string `json:"unikey"`
	}
	if err := l.post("/api/login/qrcode/unikey", map[string]any{"type": 3}, &res); err != nil {
		return "", err
	}
	if res.Unikey == "" {
		return "", fmt.Errorf("netease: no QR key (code %d)", res.Code)
	}
	return res.Unikey, nil
}

func LoginURL(key string) string { return "https://music.163.com/login?codekey=" + key }

// QR login states, as NetEase reports them.
const (
	QRExpired   = 800
	QRWaiting   = 801
	QRScanned   = 802
	QRConfirmed = 803
)

type QRStatus struct {
	Code      int      `json:"code"`
	Message   string   `json:"message"`
	Nickname  string   `json:"nickname"`
	AvatarURL string   `json:"avatarUrl"`
	Profile   *Profile `json:"-"`
}

// Check reports the QR state and looks up a confirmed profile in the
// attempt's own session, leaving the current account alone until Commit.
func (l *QRLogin) Check(key string) (QRStatus, error) {
	var r QRStatus
	err := l.post("/api/login/qrcode/client/login", map[string]any{"key": key, "type": 3}, &r)
	if err == nil && r.Code == QRConfirmed {
		client := *httpClient
		client.Jar = l.jar
		r.Profile, _ = accountWithClient(&client)
	}
	return r, err
}

// SessionCookies returns the cookies the session is kept by, for Logout to
// end it with after they are cleared here.
func SessionCookies() []*http.Cookie {
	if httpClient.Jar == nil {
		return nil
	}
	var session []*http.Cookie
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		if c.Name == "MUSIC_U" || c.Name == "__csrf" {
			session = append(session, c)
		}
	}
	return session
}

// Logout ends the session with NetEase too, with the cookies it was kept by:
// the app clears them here first, and a request without them ends nothing
// there.
func Logout(cookies []*http.Cookie) error {
	// This request belongs to the account that signed out. Its response
	// must not expire a new account's cookies or delete its saved session.
	client := *httpClient
	client.Jar = nil
	return postAsClient(&client, "/api/logout", url.Values{}, cookies, &struct{}{})
}

// UserPlaylists returns the playlists a listener made, the liked songs
// first, and those they saved.
func UserPlaylists(uid int64) (made, saved []Playlist, err error) {
	for offset := 0; ; offset += userPlaylistPage {
		var res struct {
			Playlist []Playlist `json:"playlist"`
		}
		q := url.Values{
			"uid":    {strconv.FormatInt(uid, 10)},
			"limit":  {strconv.Itoa(userPlaylistPage)},
			"offset": {strconv.Itoa(offset)},
		}
		if err := get("/api/user/playlist", q, &res); err != nil {
			return nil, nil, err
		}
		for _, p := range res.Playlist {
			if p.Subscribed {
				saved = append(saved, p)
			} else {
				made = append(made, p)
			}
		}
		// A page short of what was asked for is the last one: asking for
		// the first page alone left the rest of a listener's playlists out
		// of the library.
		if len(res.Playlist) < userPlaylistPage {
			return made, saved, nil
		}
	}
}

// userPlaylistPage is how many playlists NetEase sends at a time.
const userPlaylistPage = 200

// RecommendedPlaylists returns the day's playlists picked for the listener.
func RecommendedPlaylists() ([]Playlist, error) {
	var res struct {
		Recommend []struct {
			Playlist
			Playcount float64 `json:"playcount"`
		} `json:"recommend"`
	}
	if err := get("/api/v1/discovery/recommend/resource", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Playlist, len(res.Recommend))
	for i, r := range res.Recommend {
		out[i] = r.Playlist
		out[i].PlayCount = r.Playcount
	}
	return out, nil
}

// LikedIDs returns the songs a listener likes, in no order.
func LikedIDs(uid int64) ([]int64, error) {
	var res struct {
		Code int     `json:"code"`
		IDs  []int64 `json:"ids"`
	}
	if err := eapiPost("/api/song/like/get", map[string]any{"uid": strconv.FormatInt(uid, 10)}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, (apiError{Code: res.Code}).codeError("liked songs")
	}
	return res.IDs, nil
}

// Like likes a song, adding it to the listener's liked songs, or unlikes it.
func Like(id int64, like bool) error {
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	data := map[string]any{"trackId": id, "like": like}
	if err := weapiPost("/weapi/song/like", data, &res); err != nil {
		return err
	}
	if res.Code != 200 {
		return fmt.Errorf("netease: like: code %d %s", res.Code, res.Msg)
	}
	return nil
}

// SessionIdentity binds offline account metadata to the restored login cookie.
// Only its digest is used in cache keys; credentials stay in the keychain.
func SessionIdentity() string {
	if httpClient.Jar == nil {
		return ""
	}
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		if c.Name == "MUSIC_U" && c.Value != "" {
			sum := sha256.Sum256([]byte(c.Value))
			return hex.EncodeToString(sum[:])
		}
	}
	return ""
}
