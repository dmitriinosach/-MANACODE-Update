package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var classAtlas = []string{`Interface\Glues\CharacterCreate\UI-CharacterCreate-Classes.blp`, `Interface\WorldStateFrame\Icons-Classes.blp`}

var classTCoords = map[string][4]float64{
	"WARRIOR":     {0, 0.25, 0, 0.25},
	"MAGE":        {0.25, 0.49609375, 0, 0.25},
	"ROGUE":       {0.49609375, 0.7421875, 0, 0.25},
	"DRUID":       {0.7421875, 0.98828125, 0, 0.25},
	"HUNTER":      {0, 0.25, 0.25, 0.5},
	"SHAMAN":      {0.25, 0.49609375, 0.25, 0.5},
	"PRIEST":      {0.49609375, 0.7421875, 0.25, 0.5},
	"WARLOCK":     {0.7421875, 0.98828125, 0.25, 0.5},
	"PALADIN":     {0, 0.25, 0.5, 0.75},
	"DEATHKNIGHT": {0.25, 0.49609375, 0.5, 0.75},
}

const goldIconPath = `Interface\MoneyFrame\UI-GoldIcon.blp`

var iconCacheDir = func() string {
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, "ManaCode", "icons")
}

var (
	iconMu  sync.Mutex
	iconMem = map[string][]byte{}
)

type pageArt struct {
	Font    []byte
	Icons   map[string]string
	Classes map[string]string
	Gold    string
}

type artSource struct {
	set *mpqSet
}

func iconKey(name string) string {
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".blp"), ".BLP")
	ok := name != ""
	for _, r := range name {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			ok = false
		}
	}
	if !ok {
		return ""
	}
	return strings.ToLower(name)
}

func (s *artSource) png(key, path string, cut func(*image.NRGBA) image.Image) []byte {
	iconMu.Lock()
	defer iconMu.Unlock()
	if b, ok := iconMem[key]; ok {
		return b
	}
	dir := iconCacheDir()
	if dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, key+".png")); err == nil && bytes.HasPrefix(b, []byte("\x89PNG")) {
			iconMem[key] = b
			return b
		}
	}
	raw, err := s.set.read(path)
	if err != nil {
		return nil
	}
	img, err := decodeBLP(raw)
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, cut(img)); err != nil {
		return nil
	}
	b := buf.Bytes()
	iconMem[key] = b
	if dir != "" && os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(dir, key+".png"), b, 0o644)
	}
	return b
}

func crop(img *image.NRGBA, x0, x1, y0, y1 float64) image.Image {
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	r := image.Rect(int(x0*w+0.5), int(y0*h+0.5), int(x1*w+0.5), int(y1*h+0.5))
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Rect, img, r.Min, draw.Src)
	return out
}

func dataURI(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

func (s *artSource) item(name string) string {
	key := iconKey(name)
	if key == "" {
		return ""
	}
	return dataURI(s.png(key, `Interface\Icons\`+key+".blp", func(img *image.NRGBA) image.Image {
		return crop(img, 0.08, 0.92, 0.08, 0.92)
	}))
}

func (s *artSource) class(class string) string {
	tc, ok := classTCoords[class]
	if !ok {
		return ""
	}
	cut := func(img *image.NRGBA) image.Image {
		return crop(img, tc[0], tc[1], tc[2], tc[3])
	}
	for i, path := range classAtlas {
		if b := s.png("class-"+strings.ToLower(class)+"-"+string(rune('a'+i)), path, cut); b != nil {
			return dataURI(b)
		}
	}
	return ""
}

func (s *artSource) gold() string {
	return dataURI(s.png("money-gold", goldIconPath, func(img *image.NRGBA) image.Image { return img }))
}

func loadArt(game string, p discordPost) pageArt {
	art := pageArt{Font: gameFont(game), Icons: map[string]string{}, Classes: map[string]string{}}
	s := &artSource{set: gameMPQ(game)}
	defer s.set.Close()
	for _, c := range p.Consumables {
		for _, it := range c.Items {
			if key := iconKey(it.Icon); key != "" {
				if _, done := art.Icons[key]; !done {
					art.Icons[key] = s.item(key)
				}
			}
		}
	}
	for class := range classTCoords {
		if uri := s.class(class); uri != "" {
			art.Classes[class] = uri
		}
	}
	art.Gold = s.gold()
	return art
}

func (a pageArt) icon(name string) string {
	return a.Icons[iconKey(name)]
}
