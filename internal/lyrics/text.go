package lyrics

// Text keeps original, translated and word-timed source text together.
// The exported field names are also the persisted cache keys.
type Text struct{ Original, Translation, Words string }
