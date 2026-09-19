package pvf

import (
	"compress/zlib"
	"encoding/binary"
	"io"
	"runtime"
	"sync"
)

// LCG constants shared by every PVF variant.
const (
	lcgMul      uint32 = 0x343FD
	seedFormula uint32 = 0x76826701 // multiplier of the first key byte
	seedBase    uint32 = 0x1C1      // base of the Horner tail
)

// invLCGMul is the modular inverse of lcgMul modulo 2^32.
var invLCGMul = func() uint32 {
	inv := lcgMul
	for i := 0; i < 6; i++ {
		inv *= 2 - lcgMul*inv
	}
	return inv
}()

// sectionKey is one section's LCG seed together with its magic constant.
type sectionKey struct {
	seed  uint32
	magic uint32
}

// keySet holds the per-section keys of one archive. The standard variant
// derives every seed from a fixed key string; the alternate variant ships a
// different, equally fixed set of seeds (see variantKeys).
type keySet struct {
	header, hash, grpi, body, strA, strW sectionKey

	// maskA/maskW are the name-pool section size obfuscation constants, which
	// differ between the 90US scheme and the newer Paged110 scheme.
	maskA, maskW uint32

	// bodyRecovered records that body was derived from the archive data rather
	// than from a known key set, so the recovery is attempted only once.
	bodyRecovered bool
}

// keySeed derives the standard seed from a 4-byte section key string.
func keySeed(key string) uint32 {
	k := []byte(key)
	return seedFormula*uint32(k[0]) +
		seedBase*(uint32(k[3])+seedBase*(uint32(k[2])+seedBase*uint32(k[1])))
}

// standardKeys is the key set used by the reference "S4A21" archives.
func standardKeys() keySet {
	return keySet{
		header: sectionKey{keySeed(keyHead), magicMain},
		hash:   sectionKey{keySeed(keyHash), magicMain},
		grpi:   sectionKey{keySeed(keyGrpi), magicMain},
		body:   sectionKey{keySeed(keyBody), magicMain},
		strA:   sectionKey{keySeed(keyStrA), magicAlt},
		strW:   sectionKey{keySeed(keyStrW), magicAlt},
		maskA:  xorStrA,
		maskW:  xorStrW,
	}
}

// variantKeys is the key set of the alternate variant family.
//
// These seeds are constants of the variant rather than of a single archive:
// two archives of that family built eight months apart, one of them edited by
// third-party tooling in between, share all six values exactly. Pinning them
// here makes opening instant, and makes writes use the same keystream the
// original tooling used.
//
// The HASH seed is the wide-formula seed of the all-lowercase key name "hash":
// decrypted with it, the section of both known archives parses as a valid table
// (entry count, exact size, resolvable offsets, ascending lookup list), and its
// entry/lookup sets cover every current file. Rebuild therefore regenerates the
// section instead of copying the original bytes.
func variantKeys() keySet {
	return keySet{
		header: sectionKey{0x4A454634, magicMain},
		hash:   sectionKey{wideSeed(keyHashVariant), magicMain},
		grpi:   sectionKey{0x1FBB7078, magicMain},
		body:   sectionKey{0xDD4FF706, magicMain},
		strA:   sectionKey{0x712A98D4, magicAlt},
		strW:   sectionKey{0x712AE776, magicAlt},
		maskA:  xorStrA,
		maskW:  xorStrW,
	}
}

// cryptSeed XORs b in place with the LCG keystream for an explicit seed.
func cryptSeed(seed, magic uint32, b []byte) {
	if len(b) == 0 {
		return
	}
	s := seed
	n := len(b)
	nq := n >> 2
	for i := 0; i < nq; i++ {
		t1 := lcgMul*s + magic
		s = lcgMul*t1 + magic
		xk := (t1 & 0xFFFF0000) | (s >> 16)
		off := i << 2
		binary.LittleEndian.PutUint32(b[off:], binary.LittleEndian.Uint32(b[off:])^xk)
	}
	if tail := n - nq<<2; tail > 0 {
		t1 := lcgMul*s + magic
		t2 := lcgMul*t1 + magic
		var kb [4]byte
		binary.LittleEndian.PutUint32(kb[:], (t1&0xFFFF0000)|(t2>>16))
		start := nq << 2
		for i := 0; i < tail; i++ {
			b[start+i] ^= kb[i]
		}
	}
}

// lcgStatesForXk returns every LCG state whose next keystream dword is x.
func lcgStatesForXk(x, magic uint32) []uint32 {
	hiT1 := x >> 16
	loS2 := x & 0xFFFF
	out := make([]uint32, 0, 4)
	for lo := uint32(0); lo < 1<<16; lo++ {
		t1 := (hiT1 << 16) | lo
		if (lcgMul*t1+magic)>>16 != loS2 {
			continue
		}
		out = append(out, (t1-magic)*invLCGMul)
	}
	return out
}

// lcgStatesForLow16 returns the states whose next keystream dword ends in xLow.
func lcgStatesForLow16(xLow uint16, magic uint32) []uint32 {
	out := make([]uint32, 1<<16)
	base := uint32(xLow) << 16
	for i := uint32(0); i < 1<<16; i++ {
		r := base + i
		t1 := (r - magic) * invLCGMul
		out[i] = (t1 - magic) * invLCGMul
	}
	return out
}

// affine is a map x -> p*x + c over uint32.
type affine struct{ p, c uint32 }

func (f affine) apply(x uint32) uint32 { return f.p*x + f.c }

func compose(f, g affine) affine { return affine{f.p * g.p, f.p*g.c + f.c} }

// affinePow composes f with itself n times.
func affinePow(f affine, n int) affine {
	res := affine{1, 0}
	base := f
	for n > 0 {
		if n&1 == 1 {
			res = compose(base, res)
		}
		base = compose(base, base)
		n >>= 1
	}
	return res
}

// rewindSteps returns the map taking the LCG state at dword step k back to the
// state at step 0 (the seed). One dword step advances the state by
//
//	s_{k+1} = lcgMul*(lcgMul*s_k + magic) + magic
//
// so the inverse is s_k = (s_{k+1} - magic*(lcgMul+1)) * inv(lcgMul^2).
func rewindSteps(k int, magic uint32) affine {
	invA2 := invLCGMul * invLCGMul
	step := affine{p: invA2, c: -(magic * (lcgMul + 1)) * invA2}
	return affinePow(step, k)
}

// seedReader decrypts src on the fly so an invalid candidate aborts as soon as
// zlib rejects the stream, instead of decrypting the whole section.
type seedReader struct {
	src   []byte
	pos   int
	state uint32
	magic uint32
	ks    [4]byte
	avail int
}

func newSeedReader(src []byte, seed, magic uint32) *seedReader {
	return &seedReader{src: src, state: seed, magic: magic}
}

func (r *seedReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := len(p)
	if n > len(r.src)-r.pos {
		n = len(r.src) - r.pos
	}
	for i := 0; i < n; i++ {
		if r.avail == 0 {
			t1 := lcgMul*r.state + r.magic
			r.state = lcgMul*t1 + r.magic
			binary.LittleEndian.PutUint32(r.ks[:], (t1&0xFFFF0000)|(r.state>>16))
			r.avail = 4
		}
		p[i] = r.src[r.pos+i] ^ r.ks[4-r.avail]
		r.avail--
	}
	r.pos += n
	return n, nil
}

// inflateExact reports whether decrypting src with (seed, magic) yields a zlib
// stream of exactly wantLen bytes.
func inflateExact(src []byte, seed, magic uint32, wantLen int) bool {
	if wantLen < 0 {
		return false
	}
	n, exceeded, err := inflateDecryptedLen(src, seed, magic, wantLen)
	return err == nil && !exceeded && n == wantLen
}

// inflateDecryptedLen decrypts src with (seed, magic) and inflates at most
// limit+1 bytes. exceeded reports that the stream is longer than limit; when it
// is not, n is the exact decompressed length and the zlib Adler32 checksum has
// been validated.
func inflateDecryptedLen(src []byte, seed, magic uint32, limit int) (n int, exceeded bool, err error) {
	if len(src) < 2 {
		return 0, false, io.ErrUnexpectedEOF
	}
	if limit < 0 {
		limit = 0
	}
	zr, err := zlib.NewReader(newSeedReader(src, seed, magic))
	if err != nil {
		return 0, false, err
	}
	defer zr.Close()
	got, readErr := io.CopyN(io.Discard, zr, int64(limit)+1)
	if got > int64(limit) {
		return int(got), true, nil
	}
	if readErr == io.EOF {
		return int(got), false, nil
	}
	return int(got), false, readErr
}

// inflateAtLeast reports whether decrypting src with (seed, magic) yields a
// zlib stream of at least minLen bytes.
func inflateAtLeast(src []byte, seed, magic uint32, minLen int) bool {
	zr, err := zlib.NewReader(newSeedReader(src, seed, magic))
	if err != nil {
		return false
	}
	defer zr.Close()
	n, err := io.CopyN(io.Discard, zr, int64(minLen))
	return err == nil && n == int64(minLen)
}

// validZlibFLG lists the FCHECK-carrying low bytes a zlib header may have for
// compression method 8 (FLG % 31 == 0 and no preset dictionary). They are
// ordered by how often real encoders emit them — 0x9C is zlib's default level —
// because the search walks this list and stops at the first match.
var validZlibFLG = [...]byte{0x9C, 0xDA, 0x01, 0x5E, 0x20, 0x3F, 0x7D, 0xBB, 0xF9}

// grpiLooksSane reports whether an already-decrypted GRPI section satisfies the
// cumulative-size invariants: strictly increasing compressed sizes ending at
// BodySize, with every chunk's original size positive.
func (a *Archive) grpiLooksSane(grpi []byte) bool {
	count := int(a.hdr.GroupCount)
	if count <= 0 || len(grpi) < count*8 {
		return false
	}
	prev := int32(-1)
	for i := 0; i < count; i++ {
		comp := int32(binary.LittleEndian.Uint32(grpi[i*8:]))
		orig := int32(binary.LittleEndian.Uint32(grpi[i*8+4:]))
		if comp <= prev || orig <= 0 || orig > 1<<30 {
			return false
		}
		prev = comp
	}
	return prev == a.hdr.BodySize
}

// firstChunkOrigSize returns the decompressed size of chunk 0.
func (a *Archive) firstChunkOrigSize() int {
	if len(a.groups) == 0 {
		return 0
	}
	return int(a.groups[0].origSize)
}

// bodyKeyWorks reports whether the current body key inflates chunk 0 to its
// declared original size.
func (a *Archive) bodyKeyWorks() bool {
	span := a.firstChunkSpan()
	if span == nil {
		return true // nothing to validate (empty body)
	}
	return inflateExact(span, a.keys.body.seed, a.keys.body.magic, a.firstChunkOrigSize())
}

// seedProbeLen is how many ciphertext bytes a candidate seed is tested against
// before the full section is inflated. A wrong seed desynchronises almost
// immediately, so a short prefix keeps the per-candidate cost low, while still
// being long enough that a valid seed inflates far past seedProbeOut.
const seedProbeLen = 2048

// seedProbeOut is the minimum output a candidate seed must produce from the
// probe prefix. Measured on real sections a wrong seed that survives the
// structural gate yields a handful of bytes at most, while a valid seed yields
// thousands, so this threshold separates them without touching the CPU
// intensive full-section check.
const seedProbeOut = 1024

// recoverZlibSeed brute-forces the seed of a fully-encrypted zlib section. The
// first two plaintext bytes are the zlib header, which pins the low 16 bits of
// the first keystream dword; the remaining 16 bits give 65536 candidate seeds.
// A candidate is accepted only when it inflates the whole section to exactly
// wantLen bytes, which desynchronisation makes essentially unique.
//
// Candidates are split into chunks of consecutive states so the search spreads
// over all cores; recovery only runs for archives whose seeds are not the
// standard ones, so it stays off the common path.
func recoverZlibSeed(cipher []byte, wantLen int) (sectionKey, bool) {
	if wantLen <= 0 || len(cipher) < 8 {
		return sectionKey{}, false
	}
	probe := cipher
	if len(cipher) > seedProbeLen {
		probe = cipher[:seedProbeLen]
	}
	c0, c1 := cipher[0], cipher[1]

	type job struct {
		magic    uint32
		b1       byte
		from, to int // inclusive, exclusive state indexes
	}
	var jobs []job
	const stateChunk = 2048
	// magicAlt is tried first: the two string pools and the body all decrypt
	// under it in the known variants, and a match aborts the remaining work.
	for _, magic := range [...]uint32{magicAlt, magicMain} {
		for _, b1 := range validZlibFLG {
			for from := 0; from < 1<<16; from += stateChunk {
				jobs = append(jobs, job{magic, b1, from, min(from+stateChunk, 1<<16)})
			}
		}
	}

	workers := runtime.GOMAXPROCS(0)
	var (
		mu    sync.Mutex
		best  sectionKey
		found bool
		wg    sync.WaitGroup
		next  = make(chan job)
		// stop is closed once a worker wins, releasing the producer if it is
		// blocked trying to hand out more work.
		stop     = make(chan struct{})
		stopOnce sync.Once
	)
	halt := func() { stopOnce.Do(func() { close(stop) }) }
	// accept re-checks a validated seed under the lock so the first winner wins.
	accept := func(seed, magic uint32) {
		mu.Lock()
		if !found {
			found, best = true, sectionKey{seed, magic}
		}
		mu.Unlock()
		halt()
	}

	worker := func() {
		defer wg.Done()
		for j := range next {
			xLow := uint16(c0^0x78) | uint16(c1^j.b1)<<8
			base := uint32(xLow) << 16
			for i := j.from; i < j.to; i++ {
				// A solved seed makes the rest of the search pointless.
				select {
				case <-stop:
					return
				default:
				}
				// Derive the state for index i directly rather than
				// materialising all 65536 states for every job.
				r := base + uint32(i)
				t1 := (r - j.magic) * invLCGMul
				seed := (t1 - j.magic) * invLCGMul
				if !deflateHeaderLooksValid(cipher, seed, j.magic) {
					continue
				}
				if !inflateAtLeast(probe, seed, j.magic, seedProbeOut) {
					continue
				}
				if !inflateExact(cipher, seed, j.magic, wantLen) {
					continue
				}
				accept(seed, j.magic)
				return
			}
		}
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}
	// The producer exits on either finishing the queue or a winner; both paths
	// close next so the workers always terminate.
	go func() {
		defer close(next)
		for _, j := range jobs {
			select {
			case next <- j:
			case <-stop:
				return
			}
		}
	}()
	wg.Wait()
	return best, found
}

// deflateHeaderLooksValid decrypts the first bytes of a section and checks that
// the first deflate block's BTYPE is not the reserved value 3. A wrong seed
// produces essentially random bits, so this cheap test discards roughly half of
// all candidates before any inflation is attempted. It never rejects a valid
// stream: BTYPE 3 is forbidden by the format.
func deflateHeaderLooksValid(cipher []byte, seed, magic uint32) bool {
	if len(cipher) < 3 {
		return false
	}
	var ks [4]byte
	t1 := lcgMul*seed + magic
	s := lcgMul*t1 + magic
	binary.LittleEndian.PutUint32(ks[:], (t1&0xFFFF0000)|(s>>16))
	// The zlib header occupies the first two bytes; BFINAL occupies bit 0 of
	// byte 2 and BTYPE bits 1-2.
	third := cipher[2] ^ ks[2]
	return (third>>1)&0x03 != 0x03
}

// validateGRPI decrypts a GRPI section with seed and checks the structural
// invariants: every cumulative compressed size strictly increasing and the
// last one equal to bodySize, with every chunk's original size positive.
func validateGRPI(cipher []byte, seed, magic uint32, count int, bodySize int32) bool {
	return decodeGRPI(cipher, seed, magic, count, bodySize, nil)
}

// decodeGRPI validates a GRPI section, optionally filling comp/orig.
func decodeGRPI(cipher []byte, seed, magic uint32, count int, bodySize int32, dst []groupItem) bool {
	if count <= 0 || len(cipher) < count*8 {
		return false
	}
	var ks [4]byte
	s := seed
	prev := int32(-1)
	for i := 0; i < count; i++ {
		var comp, orig int32
		for j := 0; j < 2; j++ {
			t1 := lcgMul*s + magic
			s = lcgMul*t1 + magic
			binary.LittleEndian.PutUint32(ks[:], (t1&0xFFFF0000)|(s>>16))
			off := i*8 + j*4
			v := int32(binary.LittleEndian.Uint32(cipher[off:]) ^ binary.LittleEndian.Uint32(ks[:]))
			if j == 0 {
				comp = v
			} else {
				orig = v
			}
		}
		if comp <= prev || orig <= 0 || orig > 1<<30 {
			return false
		}
		prev = comp
		if dst != nil {
			dst[i] = groupItem{compSize: comp, origSize: orig}
		}
	}
	return prev == bodySize
}

// recoverGRPISeed recovers the GRPI seed. The last cumulative compressed size
// must equal the header's BodySize, which pins the keystream dword there and
// leaves 65536 candidate seeds to filter by the monotonicity invariants.
func recoverGRPISeed(cipher []byte, count int, bodySize int32) (sectionKey, bool) {
	if count <= 0 || len(cipher) < count*8 {
		return sectionKey{}, false
	}
	last := (count - 1) * 2
	for _, magic := range [...]uint32{magicMain, magicAlt} {
		cdw := binary.LittleEndian.Uint32(cipher[last*4:])
		for _, at := range lcgStatesForXk(uint32(bodySize)^cdw, magic) {
			seed := rewindSteps(last, magic).apply(at)
			if validateGRPI(cipher, seed, magic, count, bodySize) {
				return sectionKey{seed, magic}, true
			}
		}
	}
	return sectionKey{}, false
}

// headerSizeFits reports whether the decoded header describes exactly size bytes.
func headerSizeFits(hdr Header, size int) bool {
	if hdr.FileCount <= 0 || hdr.HashTableSize <= 0 || hdr.NameTableSize <= 0 ||
		hdr.GroupCount <= 0 || hdr.BodySize <= 0 || hdr.Padding < 0 {
		return false
	}
	declared := int64(headerSize) + int64(hdr.FileCount)*0x18 +
		int64(hdr.HashTableSize) + int64(hdr.NameTableSize) +
		int64(hdr.GroupCount)*8 + int64(hdr.BodySize)
	return declared == int64(size)
}

// recoverHeader decodes the header of an archive that is not using the standard
// key set. The known variant family is tried first, which is a single decrypt;
// an unknown variant falls back to solving for the seed, where the signature
// dword pins the first keystream dword and the section-size equation identifies
// the right candidate among the remaining 65536 possibilities.
func recoverHeader(data []byte) (Header, bool, keySet, bool) {
	var raw [headerSize]byte
	copy(raw[:], data[:headerSize])

	// Fast path: the known variant's fixed keys.
	keys := variantKeys()
	for _, guard := range [...]bool{true, false} {
		b := raw
		if guard {
			applyGuard(b[:])
		}
		dec := b
		cryptSeed(keys.header.seed, keys.header.magic, dec[:])
		if binary.LittleEndian.Uint32(dec[:]) != MagicSignature {
			continue
		}
		hdr := decodeHeader(dec)
		if !headerSizeFits(hdr, len(data)) {
			continue
		}
		return hdr, guard, keys, true
	}

	// Slow path: unknown variant, solve for the header seed.
	for _, guard := range [...]bool{true, false} {
		for _, magic := range [...]uint32{magicMain, magicAlt} {
			b := raw
			if guard {
				applyGuard(b[:])
			}
			cdw := binary.LittleEndian.Uint32(b[:])
			for _, seed := range lcgStatesForXk(MagicSignature^cdw, magic) {
				dec := b
				cryptSeed(seed, magic, dec[:])
				hdr := decodeHeader(dec)
				if binary.LittleEndian.Uint32(dec[:]) != MagicSignature {
					continue
				}
				if !headerSizeFits(hdr, len(data)) {
					continue
				}
				// An unknown variant: only the header key is known, so the other
				// sections are solved from their own data. The HASH seed is
				// cleared rather than taken from the standard key set: parse
				// then tries to solve it from the section, and if that fails
				// rebuild carries the original bytes over instead of
				// re-encrypting them under a key the client cannot read.
				keys := standardKeys()
				keys.hash = sectionKey{}
				keys.header = sectionKey{seed, magic}
				return hdr, guard, keys, true
			}
		}
	}
	return Header{}, false, keySet{}, false
}
