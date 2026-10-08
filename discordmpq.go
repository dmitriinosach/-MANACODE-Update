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
	"sort"
	"strings"
)

func mpqRank(name string) (int, string) {
	low := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	switch {
	case strings.HasPrefix(low, "patch"):
		return 0, low
	case strings.HasPrefix(low, "lichking"):
		return 1, low
	case strings.HasPrefix(low, "expansion"):
		return 2, low
	case strings.HasPrefix(low, "locale"):
		return 3, low
	case low == "common-2":
		return 5, low
	case low == "common":
		return 6, low
	}
	return 4, low
}

func mpqOrder(data string) []string {
	rank := func(dir string) []string {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		var names []string
		for _, e := range ents {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".mpq") {
				names = append(names, e.Name())
			}
		}
		sort.Slice(names, func(i, j int) bool {
			gi, si := mpqRank(names[i])
			gj, sj := mpqRank(names[j])
			if gi != gj {
				return gi < gj
			}
			if gi == 0 {
				return si > sj
			}
			return si < sj
		})
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = filepath.Join(dir, n)
		}
		return out
	}
	var out []string
	if ents, err := os.ReadDir(data); err == nil {
		for _, e := range ents {
			if e.IsDir() && len(e.Name()) == 4 {
				out = append(out, rank(filepath.Join(data, e.Name()))...)
			}
		}
	}
	return append(out, rank(data)...)
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

func mpqTable(f *os.File, at int64, n uint32, key string) ([]uint32, error) {
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
	f      *os.File
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
	a, err := readMPQ(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return a, nil
}

func readMPQ(f *os.File) (*mpqArc, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var base int64 = -1
	head := make([]byte, 32)
	for off := int64(0); off+32 <= st.Size() && off < 1<<24; off += 512 {
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
	a.f.Close()
}

func (a *mpqArc) read(name string) ([]byte, error) {
	n := uint32(len(a.hashes) / 4)
	h0, h1, h2 := mpqHash(name, 0)%n, mpqHash(name, 1), mpqHash(name, 2)
	bi := uint32(0xFFFFFFFF)
	for i := uint32(0); i < n; i++ {
		e := a.hashes[((h0+i)%n)*4:]
		if e[3] == 0xFFFFFFFF {
			break
		}
		if e[3] != 0xFFFFFFFE && e[0] == h1 && e[1] == h2 {
			bi = e[3]
			break
		}
	}
	if bi == 0xFFFFFFFF || int(bi)*4+3 >= len(a.blocks) {
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
