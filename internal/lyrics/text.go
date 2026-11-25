/*
保存原文、翻译和逐字歌词源文本，保持持久化缓存字段兼容。
*/

package lyrics

// Text keeps original, translated and word-timed source text together.
// The exported field names are also the persisted cache keys.
type Text struct{ Original, Translation, Words string }
