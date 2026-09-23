package pvf

import _ "embed"

// paged110EmbeddedSealedPageKeys is the default sealed 110US page-key table.
// An external sk.dat next to the archive still takes precedence at runtime.
//
//go:embed assets/sk.dat
var paged110EmbeddedSealedPageKeys []byte
