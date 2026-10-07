package main

import (
	"encoding/binary"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func blpFile(comp, depth, alphaType byte, w, h int, pal []uint32, data []byte) []byte {
	b := make([]byte, blpHead)
	le := binary.LittleEndian
	copy(b, "BLP2")
	le.PutUint32(b[4:], 1)
	b[8], b[9], b[10] = comp, depth, alphaType
	le.PutUint32(b[12:], uint32(w))
	le.PutUint32(b[16:], uint32(h))
	le.PutUint32(b[20:], blpHead)
	le.PutUint32(b[84:], uint32(len(data)))
	for i, c := range pal {
		le.PutUint32(b[148+i*4:], c)
	}
	return append(b, data...)
}

func dxtBlock(c0, c1 uint16, idx uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint16(b, c0)
	binary.LittleEndian.PutUint16(b[2:], c1)
	binary.LittleEndian.PutUint32(b[4:], idx)
	return b
}

func TestDecodeBLP(t *testing.T) {
	red, blue := uint16(0xF800), uint16(0x001F)
	px := func(b []byte) color.NRGBA {
		img, err := decodeBLP(b)
		if err != nil {
			t.Fatal(err)
		}
		return img.NRGBAAt(1, 0)
	}
	idx := uint32(2 << 2)
	if c := px(blpFile(2, 0, 0, 4, 4, nil, dxtBlock(red, blue, idx))); c != (color.NRGBA{170, 0, 85, 255}) {
		t.Errorf("DXT1, 2/3 красного: %v", c)
	}
	if c := px(blpFile(2, 1, 0, 4, 4, nil, dxtBlock(blue, red, 3<<2))); c.A != 0 {
		t.Errorf("DXT1 с прозрачностью: %v", c)
	}
	dxt3 := append([]byte{0x50, 0, 0, 0, 0, 0, 0, 0}, dxtBlock(blue, red, 1<<2)...)
	if c := px(blpFile(2, 8, 1, 4, 4, nil, dxt3)); c != (color.NRGBA{255, 0, 0, 85}) {
		t.Errorf("DXT3: %v", c)
	}
	dxt5 := append([]byte{255, 0, 1 << 3, 0, 0, 0, 0, 0}, dxtBlock(red, blue, 0)...)
	if c := px(blpFile(2, 8, 7, 4, 4, nil, dxt5)); c != (color.NRGBA{255, 0, 0, 0}) {
		t.Errorf("DXT5, индекс 1 — a1: %v", c)
	}
	dxt5[2] = 2 << 3
	if c := px(blpFile(2, 8, 7, 4, 4, nil, dxt5)); c.A != 218 {
		t.Errorf("DXT5, индекс 2 — 6/7 a0: %v", c)
	}
	pal := blpFile(1, 8, 0, 2, 1, []uint32{0xFF000000, 0xFF102030}, []byte{0, 1, 255, 77})
	if c := px(pal); c != (color.NRGBA{0x10, 0x20, 0x30, 77}) {
		t.Errorf("палитра: %v", c)
	}
	pal1 := blpFile(1, 1, 0, 2, 1, []uint32{0, 0xFF102030}, []byte{0, 1, 0b01})
	if c := px(pal1); c.A != 0 {
		t.Errorf("палитра, 1 бит альфы: %v", c)
	}
	raw := blpFile(3, 8, 0, 2, 1, nil, []byte{0, 0, 0, 0, 1, 2, 3, 4})
	if c := px(raw); c != (color.NRGBA{3, 2, 1, 4}) {
		t.Errorf("BGRA: %v", c)
	}
	for _, bad := range [][]byte{nil, []byte("BLP1"), blpFile(2, 0, 0, 8, 8, nil, dxtBlock(red, blue, 0))} {
		if _, err := decodeBLP(bad); err == nil {
			t.Errorf("битый файл прочитан: %d байт", len(bad))
		}
	}
}

func TestMPQOrder(t *testing.T) {
	data := t.TempDir()
	loc := filepath.Join(data, "ruRU")
	if err := os.Mkdir(loc, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"common.MPQ", "common-2.MPQ", "expansion.MPQ", "lichking.MPQ", "patch.MPQ", "patch-2.MPQ", "patch-3.MPQ"} {
		_ = os.WriteFile(filepath.Join(data, n), nil, 0o644)
	}
	for _, n := range []string{"locale-ruRU.MPQ", "patch-ruRU.MPQ", "patch-ruRU-2.MPQ", "patch-ruRU-3.MPQ", "patch-ruRU-y.mpq", "patch-ruRU-Z.MPQ", "speech-ruRU.MPQ"} {
		_ = os.WriteFile(filepath.Join(loc, n), nil, 0o644)
	}
	var got []string
	for _, p := range mpqOrder(data) {
		got = append(got, filepath.Base(p))
	}
	want := []string{"patch-ruRU-Z.MPQ", "patch-ruRU-y.mpq", "patch-ruRU-3.MPQ", "patch-ruRU-2.MPQ", "patch-ruRU.MPQ", "locale-ruRU.MPQ", "speech-ruRU.MPQ",
		"patch-3.MPQ", "patch-2.MPQ", "patch.MPQ", "lichking.MPQ", "expansion.MPQ", "common-2.MPQ", "common.MPQ"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("порядок\n%v\nнужен\n%v", got, want)
	}
}

func TestIconKey(t *testing.T) {
	for in, want := range map[string]string{
		"INV_Alchemy_EndlessFlask_06":          "inv_alchemy_endlessflask_06",
		`Interface\Icons\INV_Misc_Fish_52`:     "inv_misc_fish_52",
		`Interface\Icons\INV_Misc_Fish_52.blp`: "inv_misc_fish_52",
		"":                                     "",
		`..\..\x`:                              "x",
		"Spell Fire":                           "",
		strings.Repeat("a", 3) + "/" + "Ability_Foo": "ability_foo",
	} {
		if got := iconKey(in); got != want {
			t.Errorf("%q → %q, нужно %q", in, got, want)
		}
	}
}

func TestGameIcons(t *testing.T) {
	addons := os.Getenv("DISCORD_SAMPLE_ADDONS")
	if addons == "" {
		t.Skip("DISCORD_SAMPLE_ADDONS не задан")
	}
	s := gameMPQ(gameDir(addons))
	defer s.Close()
	for _, n := range []string{`Interface\Icons\INV_Alchemy_EndlessFlask_06.blp`, `Interface\Icons\INV_Misc_Fish_52.blp`, `Interface\Icons\INV_Misc_Food_DimSum.blp`, classAtlas[0], goldIconPath} {
		b, err := s.read(n)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		img, err := decodeBLP(b)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		at := img.Rect.Dx() / 2
		if n == classAtlas[0] {
			at = img.Rect.Dx() / 8
		}
		c := img.NRGBAAt(at, at)
		t.Logf("%s: сжатие %d, альфа %d/%d, %v, центр %v", n, b[8], b[9], b[10], img.Rect.Size(), c)
		if c.A == 0 || int(c.R)+int(c.G)+int(c.B) == 0 {
			t.Errorf("%s: пустой центр", n)
		}
	}
}

func TestConsumableItems(t *testing.T) {
	p := bigPost(t)
	if len(p.Top) != 5 || p.Top[0].Name != "Теневор" || p.Top[0].Class != "ROGUE" || p.Top[0].T == 0 {
		t.Errorf("топ: %+v", p.Top)
	}
	f := p.Consumables[0]
	if f.Key != "flask" || !f.HasCost || len(f.Items) != 4 || f.Items[0].Icon != "INV_Alchemy_EndlessFlask_06" || !f.Items[0].HasCost ||
		f.Items[0].Cost != 14*330000 || f.Items[3].HasCost {
		t.Errorf("настои: %+v", f)
	}
	if o := p.Consumables[6]; o.HasCost || len(o.Items) != 1 || o.Items[0].Icon != "" {
		t.Errorf("прочее без иконки и цены: %+v", o)
	}
	if goldAmount(14562000) != "1&#8239;456" || goldAmount(42000) != "4,2" || goldAmount(30000) != "3" {
		t.Errorf("золото: %s %s %s", goldAmount(14562000), goldAmount(42000), goldAmount(30000))
	}
	many := p
	many.Consumables = nil
	for i := 0; i < 6; i++ {
		c := discordConsumable{Key: "food", Label: "Еда", V: 60}
		for k := 0; k < 10; k++ {
			c.Items = append(c.Items, discordItem{Name: "Рыбный пир", V: 6})
		}
		many.Consumables = append(many.Consumables, c)
	}
	if pg := summaryHTML(many, pageArt{}); pg.H <= htmlH || strings.Count(pg.HTML, `class="it"`) != 60 {
		t.Errorf("60 строк не влезли в 1080 — страница выше, без обрезки: %d", pg.H)
	}
}
