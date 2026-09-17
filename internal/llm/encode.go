package llm

import "encoding/base64"

// encode renders raw bytes for a base64 wire field. Kept separate so every
// adapter encodes the same way.
func encode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
