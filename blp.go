package main

import (
	"encoding/binary"
	"errors"
	"image"
)

const blpHead = 1172

var errBLP = errors.New("картинка BLP не читается")

func decodeBLP(b []byte) (*image.NRGBA, error) {
	if len(b) < blpHead || string(b[:4]) != "BLP2" {
		return nil, errBLP
	}
	le := binary.LittleEndian
	kind, comp, alphaDepth, alphaType := le.Uint32(b[4:]), b[8], int(b[9]), b[10]
	w, h := int(le.Uint32(b[12:])), int(le.Uint32(b[16:]))
	off, size := int64(le.Uint32(b[20:])), int64(le.Uint32(b[84:]))
	if kind != 1 || w <= 0 || h <= 0 || w > 4096 || h > 4096 || off < blpHead || off+size > int64(len(b)) {
		return nil, errBLP
	}
	data := b[off : off+size]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var ok bool
	switch comp {
	case 1:
		ok = blpPalette(img, data, b[148:blpHead], alphaDepth)
	case 2:
		ok = blpDXT(img, data, alphaDepth, alphaType)
	case 3:
		ok = blpRaw(img, data)
	}
	if !ok {
		return nil, errBLP
	}
	return img, nil
}

func blpPalette(img *image.NRGBA, data, pal []byte, depth int) bool {
	n := img.Rect.Dx() * img.Rect.Dy()
	if len(data) < n+(n*depth+7)/8 {
		return false
	}
	alpha := data[n:]
	for i := 0; i < n; i++ {
		c := pal[int(data[i])*4:]
		a := byte(255)
		switch depth {
		case 1:
			if alpha[i/8]>>(i%8)&1 == 0 {
				a = 0
			}
		case 4:
			a = (alpha[i/2] >> (4 * (i % 2)) & 15) * 17
		case 8:
			a = alpha[i]
		}
		copy(img.Pix[i*4:], []byte{c[2], c[1], c[0], a})
	}
	return true
}

func blpRaw(img *image.NRGBA, data []byte) bool {
	n := img.Rect.Dx() * img.Rect.Dy()
	if len(data) < n*4 {
		return false
	}
	for i := 0; i < n; i++ {
		c := data[i*4:]
		copy(img.Pix[i*4:], []byte{c[2], c[1], c[0], c[3]})
	}
	return true
}

func blpDXT(img *image.NRGBA, data []byte, depth int, alphaType byte) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bw, bh := (w+3)/4, (h+3)/4
	mode := 1
	if depth > 1 {
		mode = 3
		if alphaType == 7 {
			mode = 5
		}
	}
	step := 16
	if mode == 1 {
		step = 8
	}
	if len(data) < bw*bh*step {
		return false
	}
	var px [16][4]byte
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			blk := data[(by*bw+bx)*step:]
			switch mode {
			case 1:
				dxtColor(&px, blk, true, depth == 1)
			case 3:
				dxtColor(&px, blk[8:], false, false)
				for i := 0; i < 16; i++ {
					px[i][3] = (blk[i/2] >> (4 * (i % 2)) & 15) * 17
				}
			case 5:
				dxtColor(&px, blk[8:], false, false)
				dxt5Alpha(&px, blk)
			}
			for i := 0; i < 16; i++ {
				x, y := bx*4+i%4, by*4+i/4
				if x < w && y < h {
					copy(img.Pix[img.PixOffset(x, y):], px[i][:])
				}
			}
		}
	}
	return true
}

func rgb565(c uint16) [4]int {
	r, g, b := int(c>>11&31), int(c>>5&63), int(c&31)
	return [4]int{r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2, 255}
}

func dxtColor(px *[16][4]byte, blk []byte, dxt1, holes bool) {
	le := binary.LittleEndian
	c0, c1 := le.Uint16(blk), le.Uint16(blk[2:])
	var pal [4][4]int
	pal[0], pal[1] = rgb565(c0), rgb565(c1)
	if !dxt1 || c0 > c1 {
		for k := 0; k < 3; k++ {
			pal[2][k] = (2*pal[0][k] + pal[1][k]) / 3
			pal[3][k] = (pal[0][k] + 2*pal[1][k]) / 3
		}
		pal[2][3], pal[3][3] = 255, 255
	} else {
		for k := 0; k < 3; k++ {
			pal[2][k] = (pal[0][k] + pal[1][k]) / 2
		}
		pal[2][3] = 255
		pal[3][3] = 255
		if holes {
			pal[3][3] = 0
		}
	}
	bits := le.Uint32(blk[4:])
	for i := 0; i < 16; i++ {
		c := pal[bits>>(2*i)&3]
		px[i] = [4]byte{byte(c[0]), byte(c[1]), byte(c[2]), byte(c[3])}
	}
}

func dxt5Alpha(px *[16][4]byte, blk []byte) {
	a0, a1 := int(blk[0]), int(blk[1])
	var pal [8]int
	pal[0], pal[1] = a0, a1
	if a0 > a1 {
		for i := 1; i < 7; i++ {
			pal[i+1] = ((7-i)*a0 + i*a1) / 7
		}
	} else {
		for i := 1; i < 5; i++ {
			pal[i+1] = ((5-i)*a0 + i*a1) / 5
		}
		pal[6], pal[7] = 0, 255
	}
	var bits uint64
	for i := 0; i < 6; i++ {
		bits |= uint64(blk[2+i]) << (8 * i)
	}
	for i := 0; i < 16; i++ {
		px[i][3] = byte(pal[bits>>(3*i)&7])
	}
}
