/*
按账号作用域读取歌词缓存，并通过调用方提供的获取和发布回调刷新内容。
*/

package lyrics

import (
	"encoding/json"
	"fmt"
	"time"

	"netease-music-mygo/internal/diskcache"
)

// Cache reads lyrics for a captured account scope. Fetch and Publish are
// supplied by the owner; Publish must dispatch and validate the captured epoch
// and account before invoking the update. Load itself runs off the UI thread.
type Cache struct {
	Store   *diskcache.Store
	Scope   string
	Fetch   func(int64) (string, string, string, error)
	Publish func(epoch uint64, update func())
	Now     func() time.Time
}

func (c Cache) Load(id int64, refresh func([]Line, Display)) (string, string, string, error) {
	key := fmt.Sprintf("lyrics/%d", id)
	epoch := c.Store.Epoch()
	var kept Text
	b, at := c.Store.Read(c.Scope, key)
	if b != nil && json.Unmarshal(b, &kept) == nil {
		now := time.Now
		if c.Now != nil {
			now = c.Now
		}
		if refresh != nil && now().Sub(at) >= 7*24*time.Hour {
			go func() {
				l, t, w, err := c.fetch(id, key, epoch)
				if err != nil {
					return
				}
				lines, display := BuildDisplay(l, t, w)
				c.Publish(epoch, func() { refresh(lines, display) })
			}()
		}
		return kept.Original, kept.Translation, kept.Words, nil
	}
	return c.fetch(id, key, epoch)
}

func (c Cache) fetch(id int64, key string, epoch uint64) (string, string, string, error) {
	l, t, w, err := c.Fetch(id)
	if err != nil {
		return "", "", "", err
	}
	c.Store.Put(c.Scope, key, diskcache.Encode(Text{Original: l, Translation: t, Words: w}), epoch)
	return l, t, w, nil
}
