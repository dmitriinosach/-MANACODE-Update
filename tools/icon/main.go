package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func shrink(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	sx, sy := float64(b.Dx())/float64(size), float64(b.Dy())/float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, x1 := float64(x)*sx, float64(x+1)*sx
			y0, y1 := float64(y)*sy, float64(y+1)*sy
			var r, g, bl, a, wsum float64
			for py := int(y0); float64(py) < y1; py++ {
				wy := min(y1, float64(py+1)) - max(y0, float64(py))
				for px := int(x0); float64(px) < x1; px++ {
					wx := min(x1, float64(px+1)) - max(x0, float64(px))
					w := wx * wy
					c := color.NRGBAModel.Convert(src.At(b.Min.X+px, b.Min.Y+py)).(color.NRGBA)
					ca := float64(c.A) / 255
					r += float64(c.R) * ca * w
					g += float64(c.G) * ca * w
					bl += float64(c.B) * ca * w
					a += ca * w
					wsum += w
				}
			}
			if a > 0 {
				out.SetNRGBA(x, y, color.NRGBA{uint8(r/a + 0.5), uint8(g/a + 0.5), uint8(bl/a + 0.5), uint8(a/wsum*255 + 0.5)})
			}
		}
	}
	return out
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Println("icon logo.png папка-вывода")
		os.Exit(1)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		panic(err)
	}
	out := os.Args[2]
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}
	var images [][]byte
	for _, s := range sizes {
		images = append(images, encode(shrink(src, s)))
	}
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s)
		if s >= 256 {
			dim = 0
		}
		_ = binary.Write(&ico, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, Bits           uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, b := range images {
		ico.Write(b)
	}
	if err := os.WriteFile(filepath.Join(out, "logo.ico"), ico.Bytes(), 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(out, "logo.png"), encode(shrink(src, 128)), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("logo.ico и logo.png в", out)
}
