package pvf

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// All resources use the same versioned bundle: PVFC, version, PKCS#8 length,
// sealed-table length, DER, 32-byte metadata, sealed table. Parameters never
// appear as source literals or in decoder errors.
//
//go:embed assets/clients/110us.bin
var paged110EmbeddedClient []byte

//go:embed assets/clients/100cn.bin
var paged100CNEmbeddedClient []byte

//go:embed assets/clients/115us.bin
var paged115EmbeddedClient []byte

//go:embed assets/clients/115cn.bin
var paged115CNEmbeddedClient []byte

type bundledPagedClient struct {
	sealed   []byte
	key      *rsa.PrivateKey
	metadata []byte
	profile  formatProfile
	table    []byte // RSA-unsealed, still metadata-wrapped; read-only
}

var errBundledPagedClient = errors.New("pvf: invalid bundled client resource")

var bundledClientState struct {
	once    sync.Once
	clients []bundledPagedClient
}

func bundledPagedClients() []bundledPagedClient {
	bundledClientState.once.Do(func() {
		for _, resource := range []struct {
			data    []byte
			profile formatProfile
		}{{paged110EmbeddedClient, paged110Profile}, {paged100CNEmbeddedClient, paged100CNProfile},
			{paged115EmbeddedClient, paged115Profile}, {paged115CNEmbeddedClient, paged115CNProfile}} {
			if client, err := decodeBundledPagedClient(resource.data, resource.profile); err == nil {
				bundledClientState.clients = append(bundledClientState.clients, client)
			}
		}
	})
	return bundledClientState.clients
}

func decodeBundledPagedClient(asset []byte, profile formatProfile) (bundledPagedClient, error) {
	if len(asset) < 48 || !bytes.Equal(asset[:4], []byte("PVFC")) || binary.LittleEndian.Uint32(asset[4:8]) != 1 {
		return bundledPagedClient{}, errBundledPagedClient
	}
	derSize := int64(binary.LittleEndian.Uint32(asset[8:12]))
	sealedSize := int64(binary.LittleEndian.Uint32(asset[12:16]))
	if derSize <= 0 || sealedSize <= 0 || 48+derSize+sealedSize != int64(len(asset)) {
		return bundledPagedClient{}, errBundledPagedClient
	}
	metadataOff := 16 + int(derSize)
	sealedOff := metadataOff + 32
	parsed, err := x509.ParsePKCS8PrivateKey(asset[16:metadataOff])
	if err != nil {
		return bundledPagedClient{}, errBundledPagedClient
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || pagedClientProfile(&key.PublicKey).id != profile.id {
		return bundledPagedClient{}, errBundledPagedClient
	}
	sealed := bytes.Clone(asset[sealedOff:])
	table, err := unsealPageKeyTableWithKey(sealed, key)
	if err != nil || len(table) == 0 || len(table)%paged110PageKeySize != 0 {
		return bundledPagedClient{}, errBundledPagedClient
	}
	return bundledPagedClient{sealed: sealed, key: key,
		metadata: bytes.Clone(asset[metadataOff:sealedOff]), profile: profile, table: table}, nil
}

func loadSealedPageKeyCandidates(dir string) [][]byte {
	if dir != "" {
		if sealed, err := os.ReadFile(filepath.Join(dir, sealedPageKeyName)); err == nil {
			return [][]byte{sealed}
		}
	}
	var candidates [][]byte
	for _, client := range bundledPagedClients() {
		candidates = append(candidates, bytes.Clone(client.sealed))
	}
	return candidates
}

func pagedMetadataCandidates(executable []byte) [][]byte {
	var out [][]byte
	seen := map[string]bool{}
	add := func(candidate []byte) {
		if !seen[string(candidate)] {
			seen[string(candidate)] = true
			out = append(out, candidate)
		}
	}
	for _, client := range bundledPagedClients() {
		add(client.metadata)
	}
	for _, candidate := range metadataKeyCandidates(executable) {
		add(candidate)
	}
	return out
}
