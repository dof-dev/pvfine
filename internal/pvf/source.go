package pvf

import (
	"crypto/sha256"
	"encoding/hex"
)

// SetSourcePath changes the artifact path used by Save. It is primarily used
// when a logical version is materialized from the immutable repository base:
// the in-memory archive must still save back to the user's PVF, not to the
// sidecar's base.pvf.
func (a *Archive) SetSourcePath(path string) {
	if a == nil {
		return
	}
	a.sourcePath = path
}

// SourceHash returns the SHA-256 of the currently materialized packed PVF.
// Open and Save already retain the complete packed bytes, so this avoids a
// second disk read when the version session records artifact identity.
func (a *Archive) SourceHash() string {
	if a == nil {
		return ""
	}
	sum := sha256.Sum256(a.data)
	return hex.EncodeToString(sum[:])
}
