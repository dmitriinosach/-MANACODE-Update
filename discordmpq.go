package main

import (
	"bytes"
	"compress/bzip2"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type mpqFile struct {
	path string
	key  string
	loc  bool
}

func (f mpqFile) letter() string {
	if len(f.key) == 1 && f.key[0] >= 'A' && f.key[0] <= 'Z' {
		return f.key
	}
	return ""
}

var (
	mpqBase    = []string{"common", "common-2", "expansion", "lichking"}
	mpqBaseLoc = []string{"locale-%s", "speech-%s", "expansion-locale-%s", "lichking-locale-%s", "expansion-speech-%s", "lichking-speech-%s"}
	rePatchKey = regexp.MustCompile(`(?i)^-([2-9a-z])$`)
	reLocale   = regexp.MustCompile(`(?mi)^\s*SET\s+locale\s+"([a-z]{4})"`)
)

func mpqLocales(data string) []string {
	cfg, _ := os.ReadFile(filepath.Join(filepath.Dir(data), "WTF", "Config.wtf"))
	if m := reLocale.FindSubmatch(cfg); m != nil && isDir(filepath.Join(data, string(m[1]))) {
		return []string{string(m[1])}
	}
	var with, all []string
	ents, _ := os.ReadDir(data)
	for _, e := range ents {
		if !e.IsDir() || len(e.Name()) != 4 {
			continue
		}
		all = append(all, e.Name())
		if _, err := os.Stat(filepath.Join(data, e.Name(), "locale-"+e.Name()+".MPQ")); err == nil {
			with = append(with, e.Name())
		}
	}
	if len(with) > 0 {
		return with
	}
	return all
}

func mpqLoad(data string, locs ...string) []mpqFile {
	if len(locs) == 0 {
		locs = mpqLocales(data)
	}
	base := func(dir string, names []string, loc bool) []mpqFile {
		ents, _ := os.ReadDir(dir)
		var out []mpqFile
		for _, n := range names {
			for _, e := range ents {
				if !e.IsDir() && strings.EqualFold(e.Name(), n+".mpq") {
					out = append(out, mpqFile{path: filepath.Join(dir, e.Name()), loc: loc})
					break
				}
			}
		}
		return out
	}
	patches := func(dir, prefix string, loc bool) []mpqFile {
		ents, _ := os.ReadDir(dir)
		var out []mpqFile
		for _, e := range ents {
			n := e.Name()
			stem := strings.TrimSuffix(n, filepath.Ext(n))
			if e.IsDir() || !strings.EqualFold(filepath.Ext(n), ".mpq") || len(stem) < len(prefix) || !strings.EqualFold(stem[:len(prefix)], prefix) {
				continue
			}
			key := "1"
			if rest := stem[len(prefix):]; rest != "" {
				m := rePatchKey.FindStringSubmatch(rest)
				if m == nil {
					continue
				}
				key = strings.ToUpper(m[1])
			}
			out = append(out, mpqFile{path: filepath.Join(dir, n), key: key, loc: loc})
		}
		return out
	}
	out := base(data, mpqBase, false)
	all := patches(data, "patch", false)
	for _, l := range locs {
		names := make([]string, len(mpqBaseLoc))
		for i, n := range mpqBaseLoc {
			names[i] = strings.ReplaceAll(n, "%s", l)
		}
		out = append(out, base(filepath.Join(data, l), names, true)...)
		all = append(all, patches(filepath.Join(data, l), "patch-"+l, true)...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].loc != all[j].loc {
			return all[j].loc
		}
		return all[i].key < all[j].key
	})
	return append(out, all...)
}

func mpqOrder(data string) []string {
	load := mpqLoad(data)
	out := make([]string, len(load))
	for i, f := range load {
		out[len(load)-1-i] = f.path
	}
	return out
}

var mpqCrypt = func() [0x500]uint32 {
	var t [0x500]uint32
	seed := uint32(0x00100001)
	for i := 0; i < 0x100; i++ {
		for j := i; j < 0x500; j += 0x100 {
			seed = (seed*125 + 3) % 0x2AAAAB
			a := (seed & 0xFFFF) << 16
			seed = (seed*125 + 3) % 0x2AAAAB
			t[j] = a | (seed & 0xFFFF)
		}
	}
	return t
}()

func mpqHash(s string, kind uint32) uint32 {
	s1, s2 := uint32(0x7FED7FED), uint32(0xEEEEEEEE)
	for _, ch := range []byte(strings.ToUpper(s)) {
		s1 = mpqCrypt[kind*0x100+uint32(ch)] ^ (s1 + s2)
		s2 = uint32(ch) + s1 + s2 + (s2 << 5) + 3
	}
	return s1
}

func mpqDecrypt(b []uint32, key uint32) {
	seed := uint32(0xEEEEEEEE)
	for i := range b {
		seed += mpqCrypt[0x400+(key&0xFF)]
		ch := b[i] ^ (key + seed)
		b[i] = ch
		key = ((^key << 0x15) + 0x11111111) | (key >> 0x0B)
		seed = ch + seed + (seed << 5) + 3
	}
}

var errMpq = errors.New("архив MPQ не читается")

func mpqTable(f io.ReaderAt, at int64, n uint32, key string) ([]uint32, error) {
	if n == 0 || n > 1<<20 {
		return nil, errMpq
	}
	t := make([]uint32, n*4)
	if err := binary.Read(io.NewSectionReader(f, at, int64(n)*16), binary.LittleEndian, t); err != nil {
		return nil, err
	}
	mpqDecrypt(t, mpqHash(key, 3))
	return t, nil
}

type mpqArc struct {
	f      io.ReaderAt
	c      io.Closer
	base   int64
	sector int64
	hashes []uint32
	blocks []uint32
}

func openMPQ(path string) (*mpqArc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	a, err := readMPQ(f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	a.c = f
	return a, nil
}

func readMPQ(f io.ReaderAt, size int64) (*mpqArc, error) {
	var err error
	var base int64 = -1
	head := make([]byte, 32)
	for off := int64(0); off+32 <= size && off < 1<<24; off += 512 {
		if _, err := f.ReadAt(head, off); err != nil {
			return nil, err
		}
		if string(head[:4]) == "MPQ\x1a" {
			base = off
			break
		}
	}
	if base < 0 {
		return nil, errMpq
	}
	le := binary.LittleEndian
	a := &mpqArc{f: f, base: base, sector: int64(512) << le.Uint16(head[14:])}
	if a.hashes, err = mpqTable(f, base+int64(le.Uint32(head[16:])), le.Uint32(head[24:]), "(hash table)"); err != nil {
		return nil, err
	}
	if a.blocks, err = mpqTable(f, base+int64(le.Uint32(head[20:])), le.Uint32(head[28:]), "(block table)"); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *mpqArc) Close() {
	if a.c != nil {
		a.c.Close()
	}
}

func (a *mpqArc) find(name string) (uint32, bool) {
	n := uint32(len(a.hashes) / 4)
	h0, h1, h2 := mpqHash(name, 0)%n, mpqHash(name, 1), mpqHash(name, 2)
	for i := uint32(0); i < n; i++ {
		e := a.hashes[((h0+i)%n)*4:]
		if e[3] == 0xFFFFFFFF {
			break
		}
		if e[3] != 0xFFFFFFFE && e[0] == h1 && e[1] == h2 {
			return e[3], int(e[3])*4+3 < len(a.blocks)
		}
	}
	return 0, false
}

func (a *mpqArc) read(name string) ([]byte, error) {
	bi, ok := a.find(name)
	if !ok {
		return nil, os.ErrNotExist
	}
	b := a.blocks[bi*4:]
	pos, csize, fsize, flags := a.base+int64(b[0]), int64(b[1]), int64(b[2]), b[3]
	if flags&0x80000000 == 0 || flags&0x00010000 != 0 || fsize > 16<<20 {
		return nil, errMpq
	}
	read := func(at, n int64) ([]byte, error) {
		buf := make([]byte, n)
		_, err := a.f.ReadAt(buf, at)
		return buf, err
	}
	if flags&0x300 == 0 {
		return read(pos, fsize)
	}
	if flags&0x01000000 != 0 {
		raw, err := read(pos, csize)
		if err != nil || csize >= fsize {
			return raw, err
		}
		return mpqUnpack(raw, fsize)
	}
	sector := a.sector
	count := (fsize + sector - 1) / sector
	le := binary.LittleEndian
	tb, err := read(pos, (count+1)*4)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, fsize)
	for i := int64(0); i < count; i++ {
		lo, hi := int64(le.Uint32(tb[i*4:])), int64(le.Uint32(tb[i*4+4:]))
		if hi < lo || hi-lo > sector+64 {
			return nil, errMpq
		}
		raw, err := read(pos+lo, hi-lo)
		if err != nil {
			return nil, err
		}
		plain := min(sector, fsize-i*sector)
		if int64(len(raw)) >= plain {
			out = append(out, raw[:plain]...)
			continue
		}
		part, err := mpqUnpack(raw, plain)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func mpqRead(arc, name string) ([]byte, error) {
	a, err := openMPQ(arc)
	if err != nil {
		return nil, err
	}
	defer a.Close()
	return a.read(name)
}

func mpqUnpack(raw []byte, size int64) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errMpq
	}
	var r io.Reader
	switch raw[0] {
	case 0x02:
		zr, err := zlib.NewReader(bytes.NewReader(raw[1:]))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	case 0x10:
		r = bzip2.NewReader(bytes.NewReader(raw[1:]))
	default:
		return nil, errMpq
	}
	out, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) != size {
		return nil, errMpq
	}
	return out, nil
}

type mpqSet struct {
	data string
	arcs []*mpqArc
	done bool
}

func gameMPQ(game string) *mpqSet {
	if game == "" {
		return &mpqSet{done: true}
	}
	return &mpqSet{data: filepath.Join(game, "Data")}
}

func (s *mpqSet) read(name string) ([]byte, error) {
	if !s.done {
		s.done = true
		for _, p := range mpqOrder(s.data) {
			if a, err := openMPQ(p); err == nil {
				s.arcs = append(s.arcs, a)
			}
		}
	}
	for _, a := range s.arcs {
		if b, err := a.read(name); err == nil {
			return b, nil
		}
	}
	return nil, os.ErrNotExist
}

func (s *mpqSet) Close() {
	for _, a := range s.arcs {
		a.Close()
	}
	s.arcs = nil
}
