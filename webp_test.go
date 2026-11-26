package main

import (
	"bytes"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/webp"
)

func testPicture(w, h int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(7)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			o := img.PixOffset(x, y)
			img.Pix[o] = uint8(x * 255 / w)
			img.Pix[o+1] = uint8(y * 255 / h)
			img.Pix[o+2] = uint8(128 + int(seed>>28) - 8)
			img.Pix[o+3] = 255
			if alpha {
				switch {
				case x < 3:
					img.Pix[o+3] = 0
				case y < 4:
					img.Pix[o+3] = uint8(x * 255 / w)
				}
			}
		}
	}
	return img
}

func TestEncodeWebP(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		alpha bool
	}{{64, 64, false}, {37, 21, true}, {16, 16, true}, {130, 70, false}} {
		img := testPicture(tc.w, tc.h, tc.alpha)
		data := encodeWebP(img, webpQuant)
		m, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%dx%d: %v", tc.w, tc.h, err)
		}
		if m.Bounds().Dx() != tc.w || m.Bounds().Dy() != tc.h {
			t.Fatalf("%dx%d: размер %v", tc.w, tc.h, m.Bounds())
		}
		var yc *image.YCbCr
		switch v := m.(type) {
		case *image.YCbCr:
			if tc.alpha {
				t.Fatalf("%dx%d: альфа потерялась", tc.w, tc.h)
			}
			yc = v
		case *image.NYCbCrA:
			if !tc.alpha {
				t.Fatalf("%dx%d: альфа у непрозрачной", tc.w, tc.h)
			}
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					if v.A[v.AOffset(x, y)] != img.Pix[img.PixOffset(x, y)+3] {
						t.Fatalf("%dx%d: альфа (%d,%d) не та", tc.w, tc.h, x, y)
					}
				}
			}
			yc = &v.YCbCr
		default:
			t.Fatalf("тип %T", m)
		}
		f := &vp8Frame{w: tc.w, h: tc.h, mbw: (tc.w + 15) / 16, mbh: (tc.h + 15) / 16}
		vp8Planes(img, f)
		var se float64
		for y := 0; y < tc.h; y++ {
			for x := 0; x < tc.w; x++ {
				d := float64(yc.Y[yc.YOffset(x, y)]) - float64(f.y[y*f.ys+x])
				se += d * d
			}
		}
		if psnr := 10 * math.Log10(255*255/(se/float64(tc.w*tc.h)+1e-9)); psnr < 40 {
			t.Errorf("%dx%d: яркость PSNR %.1f дБ", tc.w, tc.h, psnr)
		}
	}
}

func TestFilterAlpha(t *testing.T) {
	a := []byte{0, 10, 20, 30, 5, 15, 25, 35, 255, 0, 128, 64}
	for method := 0; method < 4; method++ {
		f := filterAlpha(a, 4, method)
		if int(f[0]>>2) != method {
			t.Fatalf("метод %d: заголовок %x", method, f[0])
		}
		back := append([]byte(nil), f[1:]...)
		for i := range back {
			x, y := i%4, i/4
			var p int
			switch {
			case method == 0, x == 0 && y == 0:
			case y == 0:
				p = int(back[i-1])
			case x == 0:
				p = int(back[i-4])
			case method == 1:
				p = int(back[i-1])
			case method == 2:
				p = int(back[i-4])
			default:
				p = min(255, max(0, int(back[i-1])+int(back[i-4])-int(back[i-5])))
			}
			back[i] += byte(p)
		}
		if !bytes.Equal(back, a) {
			t.Fatalf("метод %d: %v", method, back)
		}
	}
}

func TestLuaStrip(t *testing.T) {
	src := "local a = 1 -- коммент\n" +
		"\t\tlocal s = \"-- не коммент\" .. '\\'--' --[[ длинный\nкомментарий ]] local b=2\n" +
		"local l = [==[ -- [[ внутри ]] ]==]\n" +
		"x = a--[[c]]b\n" +
		"y = 1e-5 - -2\n" +
		"--[=[\n\n]=]z = 3\n"
	got := luaStrip(src)
	want := "local a = 1\n" +
		"local s = \"-- не коммент\" .. '\\'--'\nlocal b=2\n" +
		"local l = [==[ -- [[ внутри ]] ]==]\n" +
		"x = a b\n" +
		"y = 1e-5 - -2\n" +
		"\n\nz = 3\n"
	if got != want {
		t.Fatalf("получилось:\n%q\nждали:\n%q", got, want)
	}
	if strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Fatal("номера строк сдвинулись")
	}
}

func TestWebPageRaw(t *testing.T) {
	body := make([]byte, 0, 600)
	for i := 0; i < 600; i++ {
		body = append(body, byte(i))
	}
	tpl := "<title>{{TITLE}}</title><script>{{BUNDLE8}}</script>"
	page := webPage(tpl, "Рейд <1>", body)
	head := "<title>&#1056;&#1077;&#1081;&#1076; &lt;1&gt;</title><script>"
	if !bytes.HasPrefix(page, []byte(head)) || !bytes.HasSuffix(page, []byte("</script>")) {
		t.Fatalf("страница: %q", page[:min(len(page), 80)])
	}
	enc := page[len(head) : len(page)-len("</script>")]
	if bytes.ContainsAny(enc, "\x00\r<") {
		t.Fatal("в теле запретный байт")
	}
	var back []byte
	for i := 0; i < len(enc); i++ {
		c := enc[i]
		if c == '=' {
			i++
			c = []byte{0, '\r', '<', '='}[enc[i]-'0']
		}
		back = append(back, c)
	}
	if !bytes.Equal(back, body) {
		t.Fatal("тело не восстановилось")
	}
	if webGrowth(tpl) != webRawGrowth || webGrowth("{{BUNDLE}}") != webB64 {
		t.Fatal("рост по шаблону")
	}
}

func TestWebTemplate(t *testing.T) {
	dir := t.TempDir()
	if _, err := webTemplate(dir); err == nil {
		t.Fatal("без шаблона — ошибка")
	}
	_ = os.WriteFile(filepath.Join(dir, "standalone.html"), []byte("{{BUNDLE}}"), 0o644)
	if b, _ := webTemplate(dir); string(b) != "{{BUNDLE}}" {
		t.Fatalf("старый шаблон: %q", b)
	}
	_ = os.WriteFile(filepath.Join(dir, "standalone8.html"), []byte(webRawMark), 0o644)
	if b, _ := webTemplate(dir); string(b) != webRawMark {
		t.Fatalf("сырой шаблон не взят: %q", b)
	}
}
