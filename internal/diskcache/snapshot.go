/*
捕获缓存作用域和代次，为异步读取与持久化提供快照及编码接口。
*/

package diskcache

import (
	"encoding/json"
	"log"
	"time"
)

// Snapshot captures a cache scope and epoch before an asynchronous read/write.
// It is a value describing an existing Store, not another cache.
type Snapshot struct {
	Store      *Store
	Scope, Key string
	Epoch      uint64
}

func (r Snapshot) Read(value any) time.Time {
	b, at := r.Store.Read(r.Scope, r.Key)
	if b == nil || json.Unmarshal(b, value) != nil {
		return time.Time{}
	}
	return at
}
func (r Snapshot) Keep(b []byte) {
	if b != nil {
		r.Store.Queue(r.Scope, r.Key, b, r.Epoch)
	}
}
func Encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("cache: cannot encode %T (%T)", v, err)
		return nil
	}
	return b
}
