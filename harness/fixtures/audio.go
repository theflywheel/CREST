package fixtures

import _ "embed"

// ConsentOgg is a short, decodable Opus-in-Ogg recording used by the HTTP
// story. It is deliberately an actual media file rather than bytes that only
// happen to start with the Ogg capture pattern.
//
//go:embed consent.ogg
var ConsentOgg []byte
