package pvf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedPageKeyCandidates(t *testing.T) {
	if len(mustBundledPagedClient(t, FormatPaged100CN).sealed) != 1024 {
		t.Fatal("100CN sealed sidecar length changed")
	}
	if len(mustBundledPagedClient(t, FormatPaged115CN).sealed) != 2944 {
		t.Fatal("115CN sealed sidecar length changed")
	}
	candidates := loadSealedPageKeyCandidates(t.TempDir())
	if len(candidates) != 4 || !bytes.Equal(candidates[0], mustBundledPagedClient(t, FormatPaged110).sealed) ||
		!bytes.Equal(candidates[1], mustBundledPagedClient(t, FormatPaged100CN).sealed) ||
		!bytes.Equal(candidates[2], mustBundledPagedClient(t, FormatPaged115).sealed) ||
		!bytes.Equal(candidates[3], mustBundledPagedClient(t, FormatPaged115CN).sealed) {
		t.Fatal("missing sidecar did not select all bundled candidates")
	}
	for i, client := range bundledPagedClients() {
		candidates[i][0] ^= 1
		if bytes.Equal(candidates[i], client.sealed) {
			t.Fatal("candidate aliases embedded asset")
		}
	}
	for _, external := range [][]byte{nil, []byte("archive-specific sealed table")} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, sealedPageKeyName), external, 0o600); err != nil {
			t.Fatal(err)
		}
		candidates = loadSealedPageKeyCandidates(dir)
		if len(candidates) != 1 || !bytes.Equal(candidates[0], external) {
			t.Fatal("external sidecar did not take exclusive precedence")
		}
	}
}

func TestRealBundledPagedClient(t *testing.T) {
	path := os.Getenv(testFileEnv)
	version := os.Getenv("PVF_TEST_CLIENT_VERSION")
	if path == "" || (version != "100CN" && version != "110US" && version != "115US" && version != "115CN") {
		t.Skip("bundled client fixture required")
	}
	dir := t.TempDir()
	// No external files: the sealed table and client come from the binary.
	a, err := OpenWithSidecars(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	if a.ClientVersion() != version || !a.WriteCapabilities().CanChangeStructure {
		t.Fatal("embedded resource did not restore client capabilities")
	}
	if _, err := a.Chunk(a.Header().GroupCount - 1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sealedPageKeyName), []byte("invalid external table"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := OpenWithSidecars(path, dir)
	if err != nil {
		t.Fatal("invalid external sidecar overrode valid bundled parameters")
	}
	b.Release()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err = Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	b.Release()
	t.Log("bundled client opened without sidecars, from memory, and with an invalid external override")
}

func TestBundledPagedClientParameters(t *testing.T) {
	clients := bundledPagedClients()
	if len(clients) != 4 || clients[0].profile.id != FormatPaged110 || clients[1].profile.id != FormatPaged100CN ||
		clients[2].profile.id != FormatPaged115 || clients[3].profile.id != FormatPaged115CN {
		t.Fatal("bundled client identity or order is invalid")
	}
	for _, client := range clients {
		table, err := unsealPageKeyTableWithKey(client.sealed, client.key)
		if err != nil || !bytes.Equal(table, client.table) || len(table)%paged110PageKeySize != 0 || len(client.metadata) != 32 {
			t.Fatal("bundled client could not unseal its table")
		}
	}
}

func TestBundledPagedClientRejectsInvalidResources(t *testing.T) {
	for _, resource := range []struct {
		data           []byte
		profile, other formatProfile
	}{{paged110EmbeddedClient, paged110Profile, paged100CNProfile},
		{paged100CNEmbeddedClient, paged100CNProfile, paged110Profile},
		{paged115EmbeddedClient, paged115Profile, paged110Profile},
		{paged115CNEmbeddedClient, paged115CNProfile, paged115Profile}} {
		t.Run(resource.profile.id, func(t *testing.T) {
			if _, err := decodeBundledPagedClient(resource.data, resource.other); !errors.Is(err, errBundledPagedClient) {
				t.Fatal("resource accepted with another client's content rules")
			}
			for _, corrupt := range []func([]byte) []byte{
				func(b []byte) []byte { b[0] ^= 1; return b },
				func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:8], 2); return b },
				func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:12], 0xffffffff); return b },
				func(b []byte) []byte { b[16] = 0; return b },
				func(b []byte) []byte { return b[:len(b)-1] },
				func(b []byte) []byte { clear(b[len(b)-128:]); return b },
			} {
				if _, err := decodeBundledPagedClient(corrupt(bytes.Clone(resource.data)), resource.profile); !errors.Is(err, errBundledPagedClient) {
					t.Fatal("damaged client resource was accepted")
				}
			}
		})
	}
}

func mustBundledPagedClient(t *testing.T, format string) bundledPagedClient {
	t.Helper()
	for _, client := range bundledPagedClients() {
		if client.profile.id == format {
			return client
		}
	}
	t.Fatal("bundled client not found")
	return bundledPagedClient{}
}
