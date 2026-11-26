/*
验证播放地址接受支持的音频格式，并将容器别名规范化为安全扩展名。
*/

package netease

import (
	"encoding/json"
	"testing"
)

// Playback URLs for every supported container survive parsing; aliases normalize without allowing arbitrary cache extensions.
func TestSongURLAcceptsSupportedAudioFormats(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"mp3", "mp3"}, {"FLAC", "flac"}, {" WAV ", "wav"}, {"wave", "wav"}, {"ogg", "ogg"}, {"aac", "aac"}, {"adts", "aac"},
		{"m4a", "m4a"}, {"mp4", "m4a"}, {"alac", "m4a"}, {"3gp", "m4a"}, {"3g2", "m4a"}, {"", ""},
		{"opus", "!"}, {"../../wav", "!"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"code": 200, "data": []any{map[string]any{"code": 200, "url": "https://example.test/audio", "type": tc.input, "size": 100, "level": "exhigh"}}})
			var answer songURLAnswer
			if err := json.Unmarshal(body, &answer); err != nil {
				t.Fatal(err)
			}
			got, err := answer.stream("test", "exhigh")
			if tc.want == "!" {
				if err == nil {
					t.Fatal("unsupported type accepted")
				}
				return
			}
			if err != nil || got.Type != tc.want {
				t.Fatalf("parsed stream: %+v %v", got, err)
			}
		})
	}
}
