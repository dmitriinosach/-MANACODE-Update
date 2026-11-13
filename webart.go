package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	dbcSpellIcon   = 133
	dbcIconPath    = 1
	dbcItemDisplay = 5
	dbcDisplayIcon = 5
	dbcAchName     = 4
	dbcAchDesc     = 21
	dbcAchPoints   = 39
	dbcAchIcon     = 42
	dbcRU          = 8
)

type dbcTable struct {
	b                           []byte
	recs, size, strBase, strLen int
}

func readDBC(set *mpqSet, name string) *dbcTable {
	b, err := set.read(`DBFilesClient\` + name)
	if err != nil {
		return nil
	}
	return parseDBC(b)
}

func parseDBC(b []byte) *dbcTable {
	if len(b) < 20 || string(b[:4]) != "WDBC" {
		return nil
	}
	le := binary.LittleEndian
	d := &dbcTable{b: b, recs: int(le.Uint32(b[4:])), size: int(le.Uint32(b[12:])), strLen: int(le.Uint32(b[16:]))}
	body := len(b) - 20
	if d.size < 4 || d.size > body || d.recs > body/d.size {
		return nil
	}
	d.strBase = 20 + d.recs*d.size
	if d.strLen > len(b)-d.strBase {
		return nil
	}
	return d
}

func (d *dbcTable) num(r, f int) uint32 {
	at := 20 + r*d.size + f*4
	if at+4 > d.strBase {
		return 0
	}
	return binary.LittleEndian.Uint32(d.b[at:])
}

func (d *dbcTable) text(r, f int) string {
	off := int(d.num(r, f))
	if off == 0 || off >= d.strLen {
		return ""
	}
	s := d.b[d.strBase+off:]
	if i := bytes.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return string(s)
}

type achInfo struct {
	name, desc, icon string
	points           int
}

type clientBook struct {
	spells map[int]string
	items  map[int]string
	achs   map[int]achInfo
}

func loadClientBook(game string, set *mpqSet) clientBook {
	book := clientBook{spells: map[int]string{}, items: map[int]string{}, achs: map[int]achInfo{}}
	icons := map[uint32]string{}
	if si := readDBC(set, "SpellIcon.dbc"); si != nil {
		for r := 0; r < si.recs; r++ {
			icons[si.num(r, 0)] = si.text(r, dbcIconPath)
		}
	}
	if sp := readDBC(set, "Spell.dbc"); sp != nil {
		for r := 0; r < sp.recs; r++ {
			if p := icons[sp.num(r, dbcSpellIcon)]; p != "" {
				book.spells[int(sp.num(r, 0))] = p
			}
		}
	}
	disp := map[uint32]string{}
	if di := readDBC(set, "ItemDisplayInfo.dbc"); di != nil {
		for r := 0; r < di.recs; r++ {
			if n := di.text(r, dbcDisplayIcon); n != "" {
				disp[di.num(r, 0)] = `Interface\Icons\` + n
			}
		}
	}
	if it := readDBC(set, "Item.dbc"); it != nil {
		for r := 0; r < it.recs; r++ {
			if p := disp[it.num(r, dbcItemDisplay)]; p != "" {
				book.items[int(it.num(r, 0))] = p
			}
		}
	}
	for id, d := range itemCacheDisplays(game) {
		if _, ok := book.items[id]; !ok && disp[d] != "" {
			book.items[id] = disp[d]
		}
	}
	if ac := readDBC(set, "Achievement.dbc"); ac != nil {
		for r := 0; r < ac.recs; r++ {
			book.achs[int(ac.num(r, 0))] = achInfo{name: ac.text(r, dbcAchName+dbcRU), desc: ac.text(r, dbcAchDesc+dbcRU),
				icon: icons[ac.num(r, dbcAchIcon)], points: int(ac.num(r, dbcAchPoints))}
		}
	}
	return book
}

func itemCacheDisplays(game string) map[int]uint32 {
	out := map[int]uint32{}
	files, _ := filepath.Glob(filepath.Join(globQuote(filepath.Join(game, "Cache", "WDB")), "*", "itemcache.wdb"))
	if len(files) == 0 {
		return out
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		return out
	}
	return parseItemCache(b)
}

func globQuote(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '*' || r == '?' || r == '[' {
			sb.WriteByte('[')
			sb.WriteRune(r)
			sb.WriteByte(']')
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func parseItemCache(b []byte) map[int]uint32 {
	out := map[int]uint32{}
	if len(b) < 24 || string(b[:4]) != "BDIW" || binary.LittleEndian.Uint32(b[4:]) != 12340 {
		return out
	}
	le := binary.LittleEndian
	for p := 24; p+8 <= len(b); {
		id, n := int(le.Uint32(b[p:])), int(le.Uint32(b[p+4:]))
		if id == 0 && n == 0 {
			break
		}
		at := p + 8
		if n > len(b)-at {
			break
		}
		p = at + n
		if n < 12 {
			continue
		}
		q := at + 12
		for k := 0; k < 4 && q >= 0; k++ {
			z := bytes.IndexByte(b[q:p], 0)
			if z < 0 {
				q = -1
				break
			}
			q += z + 1
		}
		if q < 0 || q+4 > p {
			continue
		}
		out[id] = le.Uint32(b[q:])
	}
	return out
}

var reAddonPath = regexp.MustCompile(`(?i)^interface[\\/]addons[\\/]([^\\/]+)[\\/](.+)$`)
var reTexExt = regexp.MustCompile(`(?i)\.(blp|tga|png)$`)

func webRel(p string) string {
	s := strings.TrimSpace(p)
	if m := reAddonPath.FindStringSubmatch(s); m != nil {
		s = "addon/" + m[2]
	}
	s = strings.ReplaceAll(s, `\`, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return strings.ToLower(reTexExt.ReplaceAllString(s, "")) + ".png"
}

type texSource struct {
	set   *mpqSet
	addon string
	cache string
	mu    sync.Mutex
}

var errNoTex = errors.New("нет картинки")

func (s *texSource) image(p string) (*image.NRGBA, error) {
	p = strings.TrimSpace(p)
	if m := reAddonPath.FindStringSubmatch(p); m != nil {
		base := filepath.Join(s.addon, filepath.FromSlash(strings.ReplaceAll(m[2], `\`, "/")))
		list := []string{base}
		if filepath.Ext(base) == "" {
			list = []string{base + ".tga", base + ".blp"}
		}
		for _, f := range list {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if strings.EqualFold(filepath.Ext(f), ".blp") {
				return decodeBLP(b)
			}
			return decodeTGA(b)
		}
		return nil, errNoTex
	}
	name := strings.ReplaceAll(p, "/", `\`)
	name = reTexExt.ReplaceAllString(name, "") + ".blp"
	s.mu.Lock()
	b, err := s.set.read(name)
	s.mu.Unlock()
	if err != nil {
		return nil, errNoTex
	}
	return decodeBLP(b)
}

func (s *texSource) cacheKey(rel string, small bool, kind string) string {
	if s.cache == "" || strings.HasPrefix(rel, "addon/") {
		return ""
	}
	key := filepath.Join(s.cache, filepath.FromSlash(rel))
	if small {
		key += ".s"
	}
	return key + kind
}

func (s *texSource) pixels(p string, small bool) (*image.NRGBA, error) {
	img, err := s.image(p)
	if err != nil {
		return nil, err
	}
	if small && img.Rect.Dx() >= 32 && img.Rect.Dy() >= 32 {
		img = halfImage(img)
	}
	return img, nil
}

func cacheRead(key string) []byte {
	if key == "" {
		return nil
	}
	if b, err := os.ReadFile(key); err == nil && len(b) > 8 {
		return b
	}
	return nil
}

func cacheWrite(key string, b []byte) {
	if key != "" && os.MkdirAll(filepath.Dir(key), 0o755) == nil {
		_ = os.WriteFile(key, b, 0o644)
	}
}

func encodePNG(img *image.NRGBA) ([]byte, error) {
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *texSource) tex(p string, small bool) ([]byte, error) {
	key := s.cacheKey(webRel(p), small, webTexKind)
	if b := cacheRead(key); b != nil {
		return b, nil
	}
	img, err := s.pixels(p, small)
	if err != nil {
		return nil, err
	}
	b, err := encodePNG(img)
	if err != nil {
		return nil, err
	}
	if w := encodeWebP(img, webpQuant); gzipSize(w) < gzipSize(b)*webpWin/100 {
		b = w
	}
	cacheWrite(key, b)
	return b, nil
}

func halfImage(src *image.NRGBA) *image.NRGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	w, h := max(1, sw/2), max(1, sh/2)
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a int
			for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				i := src.PixOffset(min(sw-1, 2*x+d[0]), min(sh-1, 2*y+d[1]))
				al := int(src.Pix[i+3])
				r += int(src.Pix[i]) * al
				g += int(src.Pix[i+1]) * al
				b += int(src.Pix[i+2]) * al
				a += al
			}
			o := dst.PixOffset(x, y)
			if a > 0 {
				dst.Pix[o] = uint8((r + a/2) / a)
				dst.Pix[o+1] = uint8((g + a/2) / a)
				dst.Pix[o+2] = uint8((b + a/2) / a)
			}
			dst.Pix[o+3] = uint8((a + 2) / 4)
		}
	}
	return dst
}

var errTGA = errors.New("картинка TGA не читается")

func decodeTGA(b []byte) (*image.NRGBA, error) {
	if len(b) < 18 {
		return nil, errTGA
	}
	idLen, cmap, kind := int(b[0]), b[1], b[2]
	w, h := int(binary.LittleEndian.Uint16(b[12:])), int(binary.LittleEndian.Uint16(b[14:]))
	bpp, desc := int(b[16]), b[17]
	if cmap != 0 || (kind != 2 && kind != 10) || (bpp != 32 && bpp != 24) || w == 0 || h == 0 {
		return nil, errTGA
	}
	n := bpp / 8
	p := 18 + idLen
	src := make([]byte, w*h*n)
	if kind == 2 {
		if p+len(src) > len(b) {
			return nil, errTGA
		}
		copy(src, b[p:])
	} else {
		for o := 0; o < len(src) && p < len(b); {
			hd := b[p]
			p++
			cnt := int(hd&0x7f) + 1
			if hd&0x80 != 0 {
				if p+n > len(b) {
					break
				}
				for k := 0; k < cnt && o+n <= len(src); k++ {
					copy(src[o:], b[p:p+n])
					o += n
				}
				p += n
			} else {
				m := min(cnt*n, len(src)-o, len(b)-p)
				copy(src[o:], b[p:p+m])
				o += m
				p += cnt * n
			}
		}
	}
	top := desc&0x20 != 0
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := h - 1 - y
		if top {
			sy = y
		}
		for x := 0; x < w; x++ {
			s, o := (sy*w+x)*n, img.PixOffset(x, y)
			a := byte(255)
			if n == 4 {
				a = src[s+3]
			}
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = src[s+2], src[s+1], src[s], a
		}
	}
	return img, nil
}

func clientFont(game, name string) []byte {
	var b []byte
	if f, err := os.ReadFile(filepath.Join(game, "Fonts", name)); err == nil && isTTF(f) {
		b = f
	} else {
		for _, arc := range mpqOrder(filepath.Join(game, "Data")) {
			if f, err := mpqRead(arc, `Fonts\`+name); err == nil && isTTF(f) {
				b = f
				break
			}
		}
	}
	if w := webFont(b); w != nil {
		return w
	}
	return b
}
