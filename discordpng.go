package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	pngCols    = 2
	pngCardW   = 560
	pngPad     = 20
	pngGap     = 16
	pngRowH    = 22
	pngBossH   = 50
	pngHeadH   = 92
	pngFootH   = 44
	pngTopRows = 15
)

var (
	pngBG      = color.RGBA{0x1e, 0x1f, 0x24, 0xff}
	pngCard    = color.RGBA{0x2a, 0x2c, 0x33, 0xff}
	pngTrack   = color.RGBA{0x33, 0x35, 0x3d, 0xff}
	pngText    = color.RGBA{0xf2, 0xf3, 0xf5, 0xff}
	pngDim     = color.RGBA{0xa0, 0xa4, 0xad, 0xff}
	pngShadow  = color.RGBA{0x00, 0x00, 0x00, 0xc0}
	pngKill    = color.RGBA{0x57, 0xd1, 0x6b, 0xff}
	pngWipe    = color.RGBA{0xf0, 0x5a, 0x5a, 0xff}
	pngAccent  = color.RGBA{0xff, 0xc8, 0x4a, 0xff}
	pngUnknown = color.RGBA{0x9a, 0x9a, 0x9a, 0xff}
)

var classColors = map[string]color.RGBA{
	"DEATHKNIGHT": {0xc4, 0x1f, 0x3b, 0xff},
	"DRUID":       {0xff, 0x7d, 0x0a, 0xff},
	"HUNTER":      {0xab, 0xd4, 0x73, 0xff},
	"MAGE":        {0x69, 0xcc, 0xf0, 0xff},
	"PALADIN":     {0xf5, 0x8c, 0xba, 0xff},
	"PRIEST":      {0xff, 0xff, 0xff, 0xff},
	"ROGUE":       {0xff, 0xf5, 0x69, 0xff},
	"SHAMAN":      {0x00, 0x70, 0xde, 0xff},
	"WARLOCK":     {0x94, 0x82, 0xc9, 0xff},
	"WARRIOR":     {0xc7, 0x9c, 0x6e, 0xff},
}

var specNames = map[string][3]string{
	"DEATHKNIGHT": {"кровь", "лёд", "нечестивость"},
	"DRUID":       {"баланс", "сила зверя", "исцеление"},
	"HUNTER":      {"чувство зверя", "стрельба", "выживание"},
	"MAGE":        {"тайная магия", "огонь", "лёд"},
	"PALADIN":     {"свет", "защита", "воздаяние"},
	"PRIEST":      {"послушание", "свет", "тьма"},
	"ROGUE":       {"ликвидация", "бой", "скрытность"},
	"SHAMAN":      {"стихии", "совершенствование", "исцеление"},
	"WARLOCK":     {"колдовство", "демонология", "разрушение"},
	"WARRIOR":     {"оружие", "неистовство", "защита"},
}

type pngFonts struct {
	title, bold, text, small font.Face
}

var (
	fontsOnce sync.Once
	fontsVal  *pngFonts
	fontsErr  error
)

func loadFonts() (*pngFonts, error) {
	fontsOnce.Do(func() {
		reg, err := opentype.Parse(goregular.TTF)
		if err != nil {
			fontsErr = err
			return
		}
		bold, err := opentype.Parse(gobold.TTF)
		if err != nil {
			fontsErr = err
			return
		}
		face := func(f *opentype.Font, size float64) font.Face {
			if fontsErr != nil {
				return nil
			}
			fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
			if err != nil {
				fontsErr = err
			}
			return fc
		}
		fontsVal = &pngFonts{title: face(bold, 28), bold: face(bold, 18), text: face(reg, 15), small: face(reg, 13)}
	})
	return fontsVal, fontsErr
}

type canvas struct {
	img *image.RGBA
	f   *pngFonts
}

func (c *canvas) rect(x, y, w, h int, col color.Color) {
	draw.Draw(c.img, image.Rect(x, y, x+w, y+h), image.NewUniform(col), image.Point{}, draw.Over)
}

func (c *canvas) width(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

func (c *canvas) text(face font.Face, x, y int, s string, col color.Color) int {
	d := &font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return d.Dot.X.Ceil()
}

func (c *canvas) shadowed(face font.Face, x, y int, s string, col color.Color) int {
	c.text(face, x+1, y+1, s, pngShadow)
	return c.text(face, x, y, s, col)
}

func (c *canvas) fit(face font.Face, s string, w int) string {
	if c.width(face, s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		if t := string(r) + "…"; c.width(face, t) <= w {
			return t
		}
	}
	return s
}

func classColor(class string) color.RGBA {
	if col, ok := classColors[class]; ok {
		return col
	}
	return pngUnknown
}

func fade(col color.RGBA, a uint8) color.RGBA {
	k := float64(a) / 255
	return color.RGBA{uint8(float64(col.R) * k), uint8(float64(col.G) * k), uint8(float64(col.B) * k), a}
}

func specName(class string, spec int) string {
	if s, ok := specNames[class]; ok && spec >= 1 && spec <= 3 {
		return s[spec-1]
	}
	return ""
}

func groupNum(n float64) string {
	v := int64(math.Round(n))
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func shortNum(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

func clock(sec float64) string {
	s := int(math.Round(sec))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func hoursRUShort(sec float64) string {
	m := int(math.Round(sec / 60))
	if m < 60 {
		return fmt.Sprintf("%d мин", m)
	}
	return fmt.Sprintf("%d ч %02d мин", m/60, m%60)
}

func (p discordPost) mode() string {
	s := ""
	if p.Size > 0 {
		s = fmt.Sprint(p.Size)
	}
	if p.Heroic {
		s += " гер"
	} else if s != "" {
		s += " об"
	}
	return strings.TrimSpace(s)
}

func (p discordPost) killed() int {
	n := 0
	for _, b := range p.Bosses {
		if b.Kill {
			n++
		}
	}
	return n
}

func (p discordPost) title() string {
	t := p.Raid
	if m := p.mode(); m != "" {
		t += " " + m
	}
	return t
}

func (p discordPost) facts() string {
	parts := []string{}
	if p.Date != "" {
		parts = append(parts, p.Date)
	}
	if p.Busy > 0 {
		parts = append(parts, hoursRUShort(p.Busy))
	}
	bosses := fmt.Sprintf("убито %d из %d", p.killed(), len(p.Bosses))
	if p.Known > 0 {
		bosses += fmt.Sprintf(" (в подземелье %d)", p.Known)
	}
	parts = append(parts, bosses, fmt.Sprintf("вайпов %d", p.Wipes))
	if p.Players > 0 {
		parts = append(parts, fmt.Sprintf("игроков %d", p.Players))
	}
	if p.Dead > 0 {
		parts = append(parts, fmt.Sprintf("смертей %d", p.Dead))
	}
	return strings.Join(parts, " · ")
}

func cardHeight(b discordBoss) int {
	n := min(len(b.Rows), pngTopRows)
	if n == 0 {
		n = 1
	}
	return pngBossH + n*pngRowH + 12
}

func renderPost(p discordPost) ([]byte, error) {
	f, err := loadFonts()
	if err != nil {
		return nil, err
	}
	w := pngPad*2 + pngCols*pngCardW + (pngCols-1)*pngGap
	var rowsH []int
	for i := 0; i < len(p.Bosses); i += pngCols {
		h := 0
		for j := i; j < i+pngCols && j < len(p.Bosses); j++ {
			h = max(h, cardHeight(p.Bosses[j]))
		}
		rowsH = append(rowsH, h)
	}
	h := pngHeadH + pngFootH + pngPad
	for _, rh := range rowsH {
		h += rh + pngGap
	}
	c := &canvas{img: image.NewRGBA(image.Rect(0, 0, w, h)), f: f}
	c.rect(0, 0, w, h, pngBG)
	c.rect(0, 0, w, 4, pngAccent)
	c.text(f.title, pngPad, 44, c.fit(f.title, p.title(), w-pngPad*2), pngText)
	c.text(f.text, pngPad, 74, c.fit(f.text, p.facts(), w-pngPad*2), pngDim)
	y := pngHeadH
	for r, rh := range rowsH {
		for k := 0; k < pngCols; k++ {
			i := r*pngCols + k
			if i >= len(p.Bosses) {
				break
			}
			x := pngPad + k*(pngCardW+pngGap)
			c.card(x, y, rh, p.Bosses[i])
		}
		y += rh + pngGap
	}
	c.footer(p, y, w)
	var buf bytes.Buffer
	if err := png.Encode(&buf, c.img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *canvas) card(x, y, h int, b discordBoss) {
	f := c.f
	c.rect(x, y, pngCardW, h, pngCard)
	state, col := "убит", pngKill
	if !b.Kill {
		state, col = "вайп", pngWipe
	}
	c.rect(x, y, 4, h, col)
	right := state + " · " + clock(b.Time)
	if b.Tries > 1 {
		right += fmt.Sprintf(" · попыток %d", b.Tries)
	}
	rw := c.width(f.text, right)
	c.text(f.text, x+pngCardW-12-rw, y+26, right, col)
	c.text(f.bold, x+16, y+26, c.fit(f.bold, b.Name, pngCardW-40-rw), pngText)
	sub := "ДПС рейда " + groupNum(b.DPS)
	if b.Deaths > 0 {
		sub += fmt.Sprintf(" · смертей %d", b.Deaths)
	}
	c.text(f.small, x+16, y+44, sub, pngDim)
	top := 0.0
	for _, r := range b.Rows {
		top = math.Max(top, r.DPS)
	}
	ry := y + pngBossH
	if len(b.Rows) == 0 {
		c.text(f.text, x+16, ry+16, "нет урона", pngDim)
		return
	}
	barX, barW := x+40, pngCardW-40-12
	for i, r := range b.Rows {
		if i >= pngTopRows {
			break
		}
		c.text(f.small, x+14, ry+16, fmt.Sprint(i+1), pngDim)
		c.rect(barX, ry+2, barW, pngRowH-4, pngTrack)
		fill := 0
		if top > 0 {
			fill = int(math.Round(float64(barW) * r.DPS / top))
		}
		cc := classColor(r.Class)
		c.rect(barX, ry+2, fill, pngRowH-4, fade(cc, 0x80))
		name := r.Name
		if s := specName(r.Class, r.Spec); s != "" {
			name += " · " + s
		}
		val := groupNum(r.DPS)
		vw := c.width(f.text, val)
		dmg := shortNum(r.Dmg)
		dw := c.width(f.small, dmg)
		c.shadowed(f.text, barX+6, ry+16, c.fit(f.text, name, barW-vw-dw-30), pngText)
		c.shadowed(f.small, barX+barW-vw-dw-14, ry+16, dmg, pngDim)
		c.shadowed(f.text, barX+barW-6-vw, ry+16, val, pngText)
		ry += pngRowH
	}
}

func (c *canvas) footer(p discordPost, y, w int) {
	f := c.f
	x := pngPad
	if len(p.Deaths) > 0 {
		x = c.text(f.text, x, y+18, "Смерти: ", pngDim)
		for i, d := range p.Deaths {
			s := fmt.Sprintf("%s %d", d.Name, d.N)
			if i < len(p.Deaths)-1 {
				s += ", "
			}
			if x+c.width(f.text, s) > w-pngPad-200 {
				break
			}
			x = c.text(f.text, x, y+18, s, classColor(d.Class))
		}
	}
	made := ""
	if p.Made > 0 {
		made = " · " + time.Unix(p.Made, 0).Format("02.01.2006 15:04")
	}
	sign := "Raid Helper" + made
	c.text(f.small, w-pngPad-c.width(f.small, sign), y+18, sign, pngDim)
}
