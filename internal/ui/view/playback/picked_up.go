/*
呈现等待续播的跨设备播放记录及其来源信息。
*/

package playback

// PickedUp is the song a login picked up, while it waits to play.
type PickedUp struct {
	ID    int64
	Heard string // where and when, as "12分钟前在手机上听过"
}
