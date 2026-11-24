package playback

// PickedUp is the song a login picked up, while it waits to play.
type PickedUp struct {
	ID    int64
	Heard string // where and when, as "12分钟前在手机上听过"
}
