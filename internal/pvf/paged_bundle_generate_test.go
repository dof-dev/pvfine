package pvf

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// Explicitly opt in to regenerate a client bundle from matching local fixtures.
// Only the public identity is logged; client parameters stay in the binary asset.
func TestGeneratePagedClientBundle(t *testing.T) {
	output := os.Getenv("PVF_BUNDLE_OUTPUT")
	filename := os.Getenv(testFileEnv)
	if output == "" || filename == "" {
		t.Skip("PVF_BUNDLE_OUTPUT and PVF_TESTFILE required")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(filename)
	sealed, err := os.ReadFile(filepath.Join(dir, sealedPageKeyName))
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.ReadFile(filepath.Join(dir, cnExecutableName))
	if err != nil {
		t.Fatal(err)
	}
	candidates := pagedMetadataCandidates(exe)
	for remaining := exe; len(remaining) > 0; {
		start := bytes.Index(remaining, []byte("-----BEGIN "))
		if start < 0 {
			break
		}
		remaining = remaining[start:]
		block, rest := pem.Decode(remaining)
		if block == nil {
			remaining = remaining[len("-----BEGIN "):]
			continue
		}
		remaining = rest
		var key *rsa.PrivateKey
		switch block.Type {
		case "PRIVATE KEY":
			parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err == nil {
				key, _ = parsed.(*rsa.PrivateKey)
			}
		case "RSA PRIVATE KEY":
			key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
		}
		if key == nil {
			continue
		}
		table, err := unsealPageKeyTableWithKey(sealed, key)
		if err != nil {
			continue
		}
		for _, metadata := range candidates {
			if _, _, _, ok := decryptPagedWithOwnership(data, table, paged110Keys(), metadata, true); !ok {
				continue
			}
			der, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			asset := make([]byte, 16, 48+len(der)+len(sealed))
			copy(asset, "PVFC")
			binary.LittleEndian.PutUint32(asset[4:8], 1)
			binary.LittleEndian.PutUint32(asset[8:12], uint32(len(der)))
			binary.LittleEndian.PutUint32(asset[12:16], uint32(len(sealed)))
			asset = append(asset, der...)
			asset = append(asset, metadata...)
			asset = append(asset, sealed...)
			if err := os.WriteFile(output, asset, 0o600); err != nil {
				t.Fatal(err)
			}
			public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := sha256.Sum256(public)
			t.Logf("public identity: %s; bundle bytes: %d", hex.EncodeToString(fingerprint[:]), len(asset))
			return
		}
	}
	t.Fatal("no client parameters validated against the archive")
}
