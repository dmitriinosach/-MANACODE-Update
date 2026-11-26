package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

var gameFontNames = []string{"FRIZQT___CYR.TTF", "FRIZQT__.TTF"}

var (
	fontMu    sync.Mutex
	fontCache = map[string][]byte{}
)

func gameFont(game string) []byte {
	fontMu.Lock()
	defer fontMu.Unlock()
	if b, ok := fontCache[game]; ok {
		return b
	}
	b := findGameFont(game)
	if w := webFont(b); w != nil {
		b = w
	}
	fontCache[game] = b
	return b
}

func findGameFont(game string) []byte {
	if game == "" {
		return nil
	}
	for _, n := range gameFontNames {
		if b, err := os.ReadFile(filepath.Join(game, "Fonts", n)); err == nil && isTTF(b) {
			return b
		}
	}
	for _, arc := range mpqOrder(filepath.Join(game, "Data")) {
		for _, n := range gameFontNames {
			if b, err := mpqRead(arc, `Fonts\`+n); err == nil && isTTF(b) {
				return b
			}
		}
	}
	return nil
}

func isTTF(b []byte) bool {
	return len(b) > 12 && (bytes.HasPrefix(b, []byte{0, 1, 0, 0}) || bytes.HasPrefix(b, []byte("OTTO")) || bytes.HasPrefix(b, []byte("true")))
}

func webFont(b []byte) []byte {
	if !isTTF(b) || len(b) < 12 {
		return nil
	}
	be := binary.BigEndian
	n := int(be.Uint16(b[4:]))
	if 12+n*16 > len(b) {
		return nil
	}
	tags := make([]string, 0, n)
	tabs := map[string][]byte{}
	for i := 0; i < n; i++ {
		r := b[12+i*16:]
		off, size := int(be.Uint32(r[8:])), int(be.Uint32(r[12:]))
		if off < 0 || size < 0 || off+size > len(b) {
			return nil
		}
		tag := string(r[:4])
		tags = append(tags, tag)
		tabs[tag] = b[off : off+size]
	}
	maxp, cmap := tabs["maxp"], tabs["cmap"]
	if len(maxp) < 6 || cmap == nil {
		return nil
	}
	m := cmapUnicode(cmap, int(be.Uint16(maxp[4:])))
	if len(m) == 0 {
		return nil
	}
	tabs["cmap"] = cmapFormat4(m)
	sort.Strings(tags)
	return sfnt(tags, tabs)
}

func cmapUnicode(c []byte, glyphs int) map[uint16]uint16 {
	be := binary.BigEndian
	if len(c) < 4 {
		return nil
	}
	for i := 0; i < int(be.Uint16(c[2:])); i++ {
		r := 4 + i*8
		if r+8 > len(c) {
			return nil
		}
		pid, eid, off := be.Uint16(c[r:]), be.Uint16(c[r+2:]), int(be.Uint32(c[r+4:]))
		if pid != 3 || (eid != 1 && eid != 0) || off+14 > len(c) || be.Uint16(c[off:]) != 4 {
			continue
		}
		t := c[off:]
		seg := int(be.Uint16(t[6:])) / 2
		if 16+seg*8 > len(t) {
			return nil
		}
		ends, starts, deltas, ros := 14, 16+seg*2, 16+seg*4, 16+seg*6
		out := map[uint16]uint16{}
		for s := 0; s < seg; s++ {
			end, start := int(be.Uint16(t[ends+s*2:])), int(be.Uint16(t[starts+s*2:]))
			delta, ro := int(be.Uint16(t[deltas+s*2:])), int(be.Uint16(t[ros+s*2:]))
			for ch := start; ch <= end && ch < 0xFFFF; ch++ {
				g := 0
				if ro == 0 {
					g = (ch + delta) & 0xFFFF
				} else {
					at := ros + s*2 + ro + 2*(ch-start)
					if at+2 > len(t) {
						continue
					}
					if g = int(be.Uint16(t[at:])); g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				if g > 0 && g < glyphs {
					out[uint16(ch)] = uint16(g)
				}
			}
		}
		return out
	}
	return nil
}

func cmapFormat4(m map[uint16]uint16) []byte {
	codes := make([]int, 0, len(m))
	for c := range m {
		codes = append(codes, int(c))
	}
	sort.Ints(codes)
	type seg struct{ start, end, delta int }
	var segs []seg
	for _, c := range codes {
		d := (int(m[uint16(c)]) - c) & 0xFFFF
		if k := len(segs) - 1; k >= 0 && segs[k].end == c-1 && segs[k].delta == d {
			segs[k].end = c
			continue
		}
		segs = append(segs, seg{c, c, d})
	}
	segs = append(segs, seg{0xFFFF, 0xFFFF, 1})
	n := len(segs)
	p, e := 1, 0
	for p*2 <= n {
		p, e = p*2, e+1
	}
	be := binary.BigEndian
	sub := make([]byte, 16+n*8)
	be.PutUint16(sub[0:], 4)
	be.PutUint16(sub[2:], uint16(len(sub)))
	be.PutUint16(sub[6:], uint16(n*2))
	be.PutUint16(sub[8:], uint16(p*2))
	be.PutUint16(sub[10:], uint16(e))
	be.PutUint16(sub[12:], uint16(n*2-p*2))
	for i, s := range segs {
		be.PutUint16(sub[14+i*2:], uint16(s.end))
		be.PutUint16(sub[16+n*2+i*2:], uint16(s.start))
		be.PutUint16(sub[16+n*4+i*2:], uint16(s.delta))
	}
	head := make([]byte, 12)
	be.PutUint16(head[2:], 1)
	be.PutUint16(head[4:], 3)
	be.PutUint16(head[6:], 1)
	be.PutUint32(head[8:], 12)
	return append(head, sub...)
}

func sfnt(tags []string, tabs map[string][]byte) []byte {
	be := binary.BigEndian
	n := len(tags)
	p, e := 1, 0
	for p*2 <= n {
		p, e = p*2, e+1
	}
	head := make([]byte, 12+16*n)
	be.PutUint32(head[0:], 0x00010000)
	be.PutUint16(head[4:], uint16(n))
	be.PutUint16(head[6:], uint16(p*16))
	be.PutUint16(head[8:], uint16(e))
	be.PutUint16(head[10:], uint16(n*16-p*16))
	var body []byte
	for i, tag := range tags {
		t := tabs[tag]
		padded := append(append([]byte{}, t...), make([]byte, (4-len(t)%4)%4)...)
		var sum uint32
		for j := 0; j < len(padded); j += 4 {
			sum += be.Uint32(padded[j:])
		}
		r := head[12+i*16:]
		copy(r, tag)
		be.PutUint32(r[4:], sum)
		be.PutUint32(r[8:], uint32(len(head)+len(body)))
		be.PutUint32(r[12:], uint32(len(t)))
		body = append(body, padded...)
	}
	return append(head, body...)
}
