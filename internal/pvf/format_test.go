package pvf

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"
)

func TestKnownFormatsSyntheticRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys keySet
	}{
		{"standard", standardKeys()}, {"alternate", variantKeys()},
	} {
		for _, guard := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/plain", true: "/guard"}[guard], func(t *testing.T) {
				a := New()
				a.keys, a.guard = tc.keys, guard
				_, err := a.AddFileText("etc/a.txt", "hello", TypeUnicode)
				if err != nil {
					t.Fatal(err)
				}
				var original bytes.Buffer
				if err := a.SaveTo(&original); err != nil {
					t.Fatal(err)
				}
				b, err := Parse(original.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				if b.Format() != tc.name || b.UsesGuard() != guard || b.keys.header != tc.keys.header {
					t.Fatal("wrong detected format")
				}
				var unchanged bytes.Buffer
				if err := b.SaveTo(&unchanged); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(original.Bytes(), unchanged.Bytes()) {
					t.Fatal("unmodified bytes changed")
				}
				if err := b.SetText(0, "changed"); err != nil {
					t.Fatal(err)
				}
				b.AddFile("etc/b.txt", utf16le("new"), TypeUnicode)
				if _, err := b.RemoveFiles([]int32{0}); err != nil {
					t.Fatal(err)
				}
				var edited bytes.Buffer
				if err := b.SaveTo(&edited); err != nil {
					t.Fatal(err)
				}
				c, err := Parse(edited.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				assertSavedHashMatchesFiles(t, c)
				if _, ok := c.Find("etc/a.txt"); ok {
					t.Fatal("removed entry remains")
				}
				i, ok := c.Find("etc/b.txt")
				if !ok {
					t.Fatal("added entry missing")
				}
				if text, err := c.Text(i); err != nil || text != "new" {
					t.Fatal("payload did not round trip")
				}
			})
		}
	}
}

func TestUnknownHashWritePolicy(t *testing.T) {
	for _, paged := range []bool{false, true} {
		for _, mutation := range []string{"payload", "add", "remove", "pool"} {
			t.Run(map[bool]string{false: "plain", true: "paged"}[paged]+"/"+mutation, func(t *testing.T) {
				a := New()
				a.AddFile("etc/a.txt", utf16le("old"), TypeUnicode)
				var packed bytes.Buffer
				if err := a.SaveTo(&packed); err != nil {
					t.Fatal(err)
				}
				a.keys.hash = sectionKey{}
				a.keys.hashKnown = false
				if paged {
					a.format = paged110Profile
					a.pageKeys = make([]byte, 32)
					a.strA = nil
				}
				if caps := a.WriteCapabilities(); !caps.CanSave || caps.CanChangeStructure {
					t.Fatal("incorrect write capability")
				}
				switch mutation {
				case "payload":
					_ = a.SetRawBytes(0, utf16le("replacement"))
				case "add":
					a.AddFile("etc/b.txt", utf16le("new"), TypeUnicode)
				case "remove":
					_, _ = a.RemoveFiles([]int32{0})
				case "pool":
					a.UnicodeStringOffset("new string")
				}
				var out bytes.Buffer
				err := a.SaveTo(&out)
				if mutation == "payload" {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					if !errors.Is(err, a.structureLockedError()) {
						t.Fatalf("expected structure rejection, got %v", err)
					}
					if out.Len() != 0 {
						t.Fatal("rejected save wrote bytes")
					}
					if !a.Modified() {
						t.Fatal("rejected save discarded edits")
					}
				}
			})
		}
	}
}

func TestBatchClonePreservesContainerAndRules(t *testing.T) {
	a := New()
	a.format = paged110Profile
	a.pageKeys = make([]byte, 32)
	stage := a.CloneForBatch()
	if !stage.IsPaged110() || stage.ContentRules() != a.ContentRules() {
		t.Fatal("clone lost format")
	}
	if !stage.WriteCapabilities().CanSave {
		t.Fatal("clone lost write state")
	}
	if offset := stage.StringOffset("ascii"); offset&1 == 0 {
		t.Fatal("clone used UTF-8 pool")
	}
	stage.pageKeys[0] = 1
	if a.pageKeys[0] != 0 {
		t.Fatal("clone aliases mutable page state")
	}
	if len(a.strW) != 2 {
		t.Fatal("clone modified original pool")
	}
}

func TestContentRulesIndependentOfContainer(t *testing.T) {
	a := New()
	a.format.rules = ContentRules{UTF16Only: true, RequiresIndexHash: true}
	if a.IsPaged110() {
		t.Fatal("content rules changed container")
	}
	if offset := a.StringOffset("ascii"); offset&1 == 0 {
		t.Fatal("content policy ignored")
	}
	if offset := a.scriptStringOffset("script ascii", ScriptPoolUTF8); offset&1 == 0 {
		t.Fatal("script policy ignored")
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(out.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestPagedWriteRequiresPageState(t *testing.T) {
	a := New()
	a.format = paged110Profile
	before := append([]byte(nil), a.strA...)
	var out bytes.Buffer
	if err := a.SaveTo(&out); !errors.Is(err, ErrPaged110ReadOnly) {
		t.Fatalf("got %v", err)
	}
	if !bytes.Equal(before, a.strA) || out.Len() != 0 {
		t.Fatal("rejected write changed state")
	}
}

func TestKnownZeroHashSeedAllowsStructureChanges(t *testing.T) {
	a := New()
	a.keys.hash.seed = 0
	a.AddFile("etc/a.txt", utf16le("a"), TypeUnicode)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	if !a.WriteCapabilities().CanChangeStructure {
		t.Fatal("zero seed treated as unknown")
	}
	a.AddFile("etc/b.txt", utf16le("b"), TypeUnicode)
	out.Reset()
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	plain := append([]byte(nil), a.data[a.hashOff:a.hashOff+a.hashSize]...)
	cryptSeed(a.keys.hash.seed, a.keys.hash.magic, plain)
	entries, _ := parseHashTable(plain)
	if len(entries) != 2 {
		t.Fatal("HASH not rebuilt")
	}
}

func TestParseRejectsInvalidNameSection(t *testing.T) {
	a := New()
	a.AddFile("etc/a.txt", utf16le("a"), TypeUnicode)
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	for _, lengthField := range []bool{false, true} {
		data := append([]byte(nil), out.Bytes()...)
		offset := a.nameOff + 8
		if lengthField {
			offset += 4
		}
		// Invalidate either the compressed size or the declared original length.
		data[offset+3] ^= 0x80
		if _, err := Parse(data); !errors.Is(err, ErrInvalidSection) {
			t.Fatalf("expected section error, got %v", err)
		}
	}
}

// This exercises page wrapping, parsing, edits and clone/save with synthetic
// page state, independently of proprietary sidecar fixtures.
func TestPagedContainerSyntheticRoundTrip(t *testing.T) {
	a := New()
	a.format = paged110Profile
	a.keys = paged110Keys()
	a.keys.hash = standardKeys().hash
	a.keys.hashKnown = true
	a.pageKeys = make([]byte, paged110PageKeySize)
	payload := make([]byte, 20000)
	_, _ = rand.New(rand.NewSource(42)).Read(payload)
	a.AddFile("etc/padding.txt", payload, TypeUnicode)
	i, err := a.AddFileText("etc/edit.txt", "before", TypeUnicode)
	if err != nil {
		t.Fatal(err)
	}
	openLogical := func(packed []byte) *Archive {
		t.Helper()
		logical := append([]byte(nil), packed...)
		if decryptPageGuards(logical, a.pageKeys) != 1 {
			t.Fatal("page not unlocked")
		}
		detected, ok := probeHeader(logical, a.keys, false)
		if !ok {
			t.Fatal("logical header not recognized")
		}
		detected.format, detected.pageKeys = paged110Profile, append([]byte(nil), a.pageKeys...)
		b, err := parseDetected(detected)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var original bytes.Buffer
	if err := a.SaveTo(&original); err != nil {
		t.Fatal(err)
	}
	b := openLogical(original.Bytes())
	var unchanged bytes.Buffer
	if err := b.SaveTo(&unchanged); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Bytes(), unchanged.Bytes()) {
		t.Fatal("unchanged page container differs")
	}
	stage := b.CloneForBatch()
	if err := stage.SetText(i, "after"); err != nil {
		t.Fatal(err)
	}
	stage.AddFile("etc/new.txt", utf16le("new"), TypeUnicode)
	var edited bytes.Buffer
	if err := stage.SaveTo(&edited); err != nil {
		t.Fatal(err)
	}
	c := openLogical(edited.Bytes())
	assertSavedHashMatchesFiles(t, c)
	if text, err := c.Text(i); err != nil || text != "after" {
		t.Fatal("edit did not round trip")
	}
	if _, ok := c.Find("etc/new.txt"); !ok {
		t.Fatal("new entry missing")
	}
	if text, err := b.Text(i); err != nil || text != "before" {
		t.Fatal("staging mutated source")
	}
	if len(c.strA) != 0 {
		t.Fatal("UTF-16-only rule not retained")
	}
}

func TestUnknownHeaderFallsBackToRecovery(t *testing.T) {
	a := New()
	// Synthetic format: only the header differs from a known container.
	a.keys.header.seed = 0x12345678
	a.AddFile("etc/a.txt", utf16le("sample"), TypeUnicode)
	// HASH recovery deliberately requires enough evidence (at least 64 bytes).
	for _, name := range []string{"b", "c", "d", "e"} {
		a.AddFile("etc/"+name+".txt", utf16le("sample"), TypeUnicode)
	}
	var out bytes.Buffer
	if err := a.SaveTo(&out); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b.Format() != FormatRecovered {
		t.Fatal("recovery format was not retained")
	}
	if !b.keys.hashKnown || !b.WriteCapabilities().CanChangeStructure {
		t.Fatal("recovered HASH capability missing")
	}
	if text, err := b.Text(0); err != nil || text != "sample" {
		t.Fatal("recovered content mismatch")
	}
}

func assertSavedHashMatchesFiles(t *testing.T, a *Archive) {
	t.Helper()
	plain := append([]byte(nil), a.data[a.hashOff:a.hashOff+a.hashSize]...)
	cryptSeed(a.keys.hash.seed, a.keys.hash.magic, plain)
	entries, _ := parseHashTable(plain)
	if len(entries) != len(a.items) {
		t.Fatal("persisted HASH count differs from file count")
	}
	for i, entry := range entries {
		if entry.nameOff != a.items[i].nameOff || entry.pathOff != a.items[i].pathOff {
			t.Fatal("persisted HASH references do not match file table")
		}
	}
}

func TestClientVersion(t *testing.T) {
	for _, tc := range []struct {
		profile formatProfile
		want    string
	}{
		{standardProfile, "90US"}, {alternateProfile, "90CN"}, {paged110Profile, "110US"}, {recoveredProfile, ""}, {formatProfile{id: "future"}, ""},
	} {
		a := New()
		a.format = tc.profile
		if got := a.ClientVersion(); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.profile.id, got, tc.want)
		}
		if got := a.CloneForBatch().ClientVersion(); got != tc.want {
			t.Fatalf("clone version = %q", got)
		}
	}
	var absent *Archive
	if absent.ClientVersion() != "" {
		t.Fatal("nil archive must have unknown version")
	}
}
