package netease

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	dawnClientVersion = "3.1.35"
	dawnBuildVersion  = "3.1.35.205293"
)

// ReportDawnStart records the beginning of an audible desktop play session.
func ReportDawnStart(track Track, bitrate int, level string, source PlaySource) error {
	plv := dawnFields(track, bitrate, level, source)
	plv["app_mode"] = 2
	now := time.Now()
	dawnRefer(plv, now, track, source, 23, 933)
	return uploadDawnRecord("_plv", plv, now)
}

// ReportDawnEnd records how long audio was actually heard and why it ended.
// These are BI log records, not entries in the older feedback weblog.
func ReportDawnEnd(track Track, played time.Duration, end string, bitrate int, level string, source PlaySource) error {
	pld := dawnFields(track, bitrate, level, source)
	pld["app_mode"] = 1
	now := time.Now()
	dawnRefer(pld, now, track, source, 92, 616)
	pld["time"] = max(int64(math.Round(played.Seconds())), 1)
	pld["realtime"] = pld["time"]
	pld["end"] = end
	pld["musiceffect_id"] = "1001"
	pld["lyriceffect"] = "default"
	pld["displayMode"] = "classic"
	return uploadDawnRecord("_pld", pld, now)
}

func uploadDawnRecord(action string, fields map[string]any, at time.Time) error {
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return uploadDawn(fmt.Appendf(nil, "%d\x01%s\x01%s\n", at.Unix(), action, data))
}

func dawnFields(track Track, bitrate int, level string, source PlaySource) map[string]any {
	data := map[string]any{
		"mode": "circulation", "download": 0,
		"status": "front", "id": strconv.FormatInt(track.ID, 10),
		"type": "song", "is_listentogether": 0,
		"is_heart": 0, "resource_ratio": "",
		"resource_time": track.Duration / 1000, "musiceffect_id": "",
		"bitrate": bitrate, "bitrate_level": level, "vipType": "", "fee": track.Fee, "file": 4,
		"rightSource": 0, "libra_abt": "",
		"channel": "netease", "curStartChannel": "",
	}
	for key, value := range playFields(track.ID, source) {
		switch key {
		case "source", "sourceId", "alg":
			data[key] = value
		}
	}
	if source.Source != "" || source.ID != 0 {
		data["sourcetype"] = "track"
	}
	return data
}

// Refer describes only the entry point the app actually knows. The desktop
// logger accepts absent navigation metadata; a fabricated liked-songs visit
// would turn every album, artist and search play into a playlist play.
func dawnRefer(fields map[string]any, now time.Time, track Track, source PlaySource, position, event int) {
	if source.ID == 0 && source.Source == "" {
		return
	}
	page, cell := "page_pc_songlist_songflow", "cell_pc_songlist_song"
	switch source.Source {
	case "album":
		page, cell = "page_pc_album_detail", "cell_pc_album_song"
	case "artist":
		page, cell = "page_pc_artist_detail", "cell_pc_artist_song"
	case "search":
		page, cell = "page_pc_search_result", "cell_pc_search_song"
	case "dailySongRecommend":
		page, cell = "page_pc_daily_recommend", "cell_pc_daily_song"
	}
	fields["_addrefer"] = fmt.Sprintf("[F:63][%d#%d#%s#205293#c9156c3][e][2][%d][%s|%s][%d:song:x:x|:::|%d:%s::]",
		now.UnixMilli(), event, dawnClientVersion, position, cell, page, track.ID, source.ID, source.Source)
}

func uploadDawn(records []byte) error {
	cookies := map[string]string{
		"WEVNSM": "1.0.0", "_iuqxldmzr_": "33", "_ntes_nnid": ",",
		"appver": dawnBuildVersion, "channel": "netease", "deviceId": "",
		"WNMCID": fmt.Sprintf("abcdef.%d.01.0", time.Now().UnixMilli()),
		"mode":   "", "ntes_kaola_ad": "1", "os": "pc", "osver": "Microsoft-Windows-10-Professional-build-19045-64bit",
	}
	for _, c := range playCookieJar().Cookies(siteURL) {
		switch c.Name {
		case "MUSIC_U", "NMTID", "__csrf", "JSESSIONID-WYYY", "WNMCID", "clientSign", "_ntes_nuid":
			cookies[c.Name] = c.Value
		}
	}
	meta, err := json.Marshal(cookies)
	if err != nil {
		return err
	}
	payload, err := ncblEncrypt(meta, records)
	if err != nil {
		return err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := fmt.Sprintf("op_%d_0_%d", 10000+int(random[0])%90000, 1+uint32(random[4])<<24|uint32(random[5])<<16|uint32(random[6])<<8|uint32(random[7]))
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://clientlog3.music.163.com/api/clientlog/encrypt/upload?multiupload=true", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Referer", "https://music.163.com/di")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/91.0.4472.164 NeteaseMusicDesktop/"+dawnClientVersion)
	var cookie []string
	for k, v := range cookies {
		cookie = append(cookie, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}
	req.Header.Set("Cookie", strings.Join(cookie, "; "))
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.8")
	res, err := DoRequest(&http.Client{Timeout: httpClient.Timeout, Transport: httpClient.Transport}, req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return responseError{res.StatusCode, "Dawn upload: " + res.Status}
	}
	contents, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var answer struct {
		Code int `json:"code"`
		Data struct {
			SuccessFiles []string `json:"successfiles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(contents, &answer); err != nil {
		return err
	}
	if answer.Code != 200 {
		return responseError{answer.Code, fmt.Sprintf("Dawn upload rejected: code %d", answer.Code)}
	}
	for _, uploaded := range answer.Data.SuccessFiles {
		if uploaded == name {
			return nil
		}
	}
	return fmt.Errorf("Dawn upload did not acknowledge %s", name)
}
