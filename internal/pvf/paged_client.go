package pvf

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
)

// SHA-256 of the RSA public key's SubjectPublicKeyInfo, verified against the
// 115US 2.38.2.34 client. This is an identity, not decryption key material.
const paged115PublicKeyFingerprint = "42631b7746e1b68ac21176a697522b8a569d29a8be1a640328dbf5dbd63d656e"

// Public identity verified against the 115CN archive and matching DNF.exe.
const paged115CNPublicKeyFingerprint = "5ef4f68c9b85f782538a4b55fbd2273533bfbda5ec23234116998ed952e902b9"

// Public identity of the DNF.exe candidate verified by unsealing the 100CN
// sidecar and validating the archive header. This constant is only a public identity.
const paged100CNPublicKeyFingerprint = "414b83aac1af7822c5038b0b98946cc3feda05930007afee1812df240a6bb85d"

// Public identity of the bundled 110US client.
const paged110PublicKeyFingerprint = "48eaaeef1b6184f62587dbbae8bfee07c01add86d2a4c324fdea80c7b17b573e"

type pageKeyTable struct {
	data    []byte
	profile formatProfile
}

func pagedClientProfile(key *rsa.PublicKey) formatProfile {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return pagedProfile
	}
	fingerprint := sha256.Sum256(der)
	if hex.EncodeToString(fingerprint[:]) == paged110PublicKeyFingerprint {
		return paged110Profile
	}
	if hex.EncodeToString(fingerprint[:]) == paged115PublicKeyFingerprint {
		return paged115Profile
	}
	if hex.EncodeToString(fingerprint[:]) == paged115CNPublicKeyFingerprint {
		return paged115CNProfile
	}
	if hex.EncodeToString(fingerprint[:]) == paged100CNPublicKeyFingerprint {
		return paged100CNProfile
	}
	return pagedProfile
}
