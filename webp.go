package main

import (
	"encoding/binary"
	"image"
	"math"
)

var (
	vp8Bands  = [17]uint8{0, 1, 2, 3, 6, 4, 5, 6, 6, 6, 6, 6, 6, 6, 6, 7, 0}
	vp8Zigzag = [16]uint8{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	vp8Cat    = [4][]uint8{
		{173, 148, 140},
		{176, 155, 140, 135},
		{180, 157, 141, 134, 130},
		{254, 254, 243, 230, 196, 177, 153, 140, 133, 130, 129},
	}
)

const (
	vp8PlaneYAC = iota
	vp8PlaneY2
	vp8PlaneUV
)

const (
	vp8DC = iota
	vp8TM
	vp8VE
	vp8HE
)

type boolEnc struct {
	out    []byte
	rng    uint32
	bottom uint32
	count  int
}

func newBoolEnc() *boolEnc { return &boolEnc{rng: 255, count: 24} }

func (e *boolEnc) put(prob uint8, bit bool) {
	split := 1 + ((e.rng-1)*uint32(prob))>>8
	if bit {
		e.bottom += split
		e.rng -= split
	} else {
		e.rng = split
	}
	for e.rng < 128 {
		e.rng <<= 1
		if e.bottom&(1<<31) != 0 {
			i := len(e.out) - 1
			for i >= 0 && e.out[i] == 255 {
				e.out[i] = 0
				i--
			}
			if i >= 0 {
				e.out[i]++
			}
		}
		e.bottom <<= 1
		e.count--
		if e.count == 0 {
			e.out = append(e.out, byte(e.bottom>>24))
			e.bottom &= 1<<24 - 1
			e.count = 8
		}
	}
}

func (e *boolEnc) literal(v uint32, n int) {
	for n > 0 {
		n--
		e.put(128, v>>uint(n)&1 != 0)
	}
}

func (e *boolEnc) bytes() []byte {
	for i := 0; i < 32; i++ {
		e.put(128, false)
	}
	return e.out
}

type vp8Bit struct {
	slot int16
	prob uint8
	bit  bool
}

type vp8Tokens struct {
	bits []vp8Bit
}

func (t *vp8Tokens) table(slot int, bit bool) {
	t.bits = append(t.bits, vp8Bit{slot: int16(slot), bit: bit})
}

func (t *vp8Tokens) fixed(prob uint8, bit bool) {
	t.bits = append(t.bits, vp8Bit{slot: -1, prob: prob, bit: bit})
}

func vp8Slot(plane, band, ctx, node int) int {
	return ((plane*8+band)*3+ctx)*11 + node
}

func (t *vp8Tokens) block(plane, ctx int, c *[16]int32, first int) int {
	last := -1
	for n := 15; n >= first; n-- {
		if c[vp8Zigzag[n]] != 0 {
			last = n
			break
		}
	}
	n := first
	band, cx := int(vp8Bands[n]), ctx
	if last < 0 {
		t.table(vp8Slot(plane, band, cx, 0), false)
		return 0
	}
	t.table(vp8Slot(plane, band, cx, 0), true)
	for n < 16 {
		v := c[vp8Zigzag[n]]
		n++
		s := func(node int) int { return vp8Slot(plane, band, cx, node) }
		if v == 0 {
			t.table(s(1), false)
			band, cx = int(vp8Bands[n]), 0
			continue
		}
		t.table(s(1), true)
		a := v
		if a < 0 {
			a = -a
		}
		if a == 1 {
			t.table(s(2), false)
			band, cx = int(vp8Bands[n]), 1
		} else {
			t.table(s(2), true)
			switch {
			case a <= 4:
				t.table(s(3), false)
				if a == 2 {
					t.table(s(4), false)
				} else {
					t.table(s(4), true)
					t.table(s(5), a == 4)
				}
			case a <= 10:
				t.table(s(3), true)
				t.table(s(6), false)
				if a <= 6 {
					t.table(s(7), false)
					t.fixed(159, a == 6)
				} else {
					t.table(s(7), true)
					t.fixed(165, (a-7)>>1 != 0)
					t.fixed(145, (a-7)&1 != 0)
				}
			default:
				t.table(s(3), true)
				t.table(s(6), true)
				cat := 3
				switch {
				case a < 19:
					cat = 0
				case a < 35:
					cat = 1
				case a < 67:
					cat = 2
				}
				t.table(s(8), cat>>1 != 0)
				t.table(s(9+cat>>1), cat&1 != 0)
				extra := a - (3 + 8<<cat)
				tab := vp8Cat[cat]
				for i, p := range tab {
					t.fixed(p, extra>>uint(len(tab)-1-i)&1 != 0)
				}
			}
			band, cx = int(vp8Bands[n]), 2
		}
		t.fixed(128, v < 0)
		if n == 16 {
			return 1
		}
		if last < n {
			t.table(vp8Slot(plane, band, cx, 0), false)
			return 1
		}
		t.table(vp8Slot(plane, band, cx, 0), true)
	}
	return 1
}

func vp8Cost(p uint8, bit bool) float64 {
	q := float64(p) / 256
	if bit {
		q = 1 - q
	}
	return -math.Log2(q)
}

func (t *vp8Tokens) probs() (probs [4][8][3][11]uint8, update [4 * 8 * 3 * 11]bool) {
	probs = vp8DefaultProb
	var n0, n1 [4 * 8 * 3 * 11]int
	for _, b := range t.bits {
		if b.slot < 0 {
			continue
		}
		if b.bit {
			n1[b.slot]++
		} else {
			n0[b.slot]++
		}
	}
	for i := range n0 {
		pl, r := i/(8*3*11), i%(8*3*11)
		band, r := r/(3*11), r%(3*11)
		ctx, node := r/11, r%11
		old := vp8DefaultProb[pl][band][ctx][node]
		up := vp8UpdateProb[pl][band][ctx][node]
		total := n0[i] + n1[i]
		keep := vp8Cost(up, false)
		if total == 0 {
			continue
		}
		p := uint8(min(255, max(1, (n0[i]*256+total/2)/total)))
		oldCost := float64(n0[i])*vp8Cost(old, false) + float64(n1[i])*vp8Cost(old, true) + keep
		newCost := float64(n0[i])*vp8Cost(p, false) + float64(n1[i])*vp8Cost(p, true) + vp8Cost(up, true) + 8
		if newCost < oldCost {
			probs[pl][band][ctx][node] = p
			update[i] = true
		}
	}
	return probs, update
}

type vp8MB struct {
	ymode, cmode int
	y2           [16]int32
	y            [16][16]int32
	u, v         [4][16]int32
	skip         bool
}

type vp8Frame struct {
	w, h, mbw, mbh int
	ys, cs         int
	y, u, v        []uint8
	ry, ru, rv     []uint8
	qi             int
	q              struct{ y1, y2, uv [2]int32 }
	mbs            []vp8MB
}

func vp8Planes(img *image.NRGBA, f *vp8Frame) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	pw, ph := f.mbw*16, f.mbh*16
	f.ys, f.cs = pw, pw/2
	f.y = make([]uint8, pw*ph)
	f.u = make([]uint8, pw*ph/4)
	f.v = make([]uint8, pw*ph/4)
	px := func(x, y int) (int32, int32, int32, int32) {
		x, y = min(x, w-1), min(y, h-1)
		o := img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y)
		p := img.Pix[o : o+4]
		return int32(p[0]), int32(p[1]), int32(p[2]), int32(p[3])
	}
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			r, g, b, _ := px(x, y)
			f.y[y*pw+x] = uint8((16839*r + 33059*g + 6420*b + 16<<16 + 1<<15) >> 16)
		}
	}
	for y := 0; y < ph/2; y++ {
		for x := 0; x < pw/2; x++ {
			var sr, sg, sb, sa, pr, pg, pb int32
			for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				r, g, b, a := px(2*x+d[0], 2*y+d[1])
				sr, sg, sb, sa = sr+r*a, sg+g*a, sb+b*a, sa+a
				pr, pg, pb = pr+r, pg+g, pb+b
			}
			var r, g, b float64
			if sa > 0 {
				r, g, b = float64(sr)/float64(sa), float64(sg)/float64(sa), float64(sb)/float64(sa)
			} else {
				r, g, b = float64(pr)/4, float64(pg)/4, float64(pb)/4
			}
			u := (-9719*r - 19081*g + 28800*b) / 65536
			v := (28800*r - 24116*g - 4684*b) / 65536
			f.u[y*f.cs+x] = uint8(min(255, max(0, math.Round(u+128))))
			f.v[y*f.cs+x] = uint8(min(255, max(0, math.Round(v+128))))
		}
	}
}

func vp8Clip(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func vp8Predict(rec []uint8, stride, x0, y0, n, mode, mbx, mby int) []int32 {
	out := make([]int32, n*n)
	top := make([]int32, n)
	left := make([]int32, n)
	var corner int32
	for i := 0; i < n; i++ {
		if mby == 0 {
			top[i] = 127
		} else {
			top[i] = int32(rec[(y0-1)*stride+x0+i])
		}
		if mbx == 0 {
			left[i] = 129
		} else {
			left[i] = int32(rec[(y0+i)*stride+x0-1])
		}
	}
	switch {
	case mby == 0:
		corner = 127
	case mbx == 0:
		corner = 129
	default:
		corner = int32(rec[(y0-1)*stride+x0-1])
	}
	shift := 4
	if n == 16 {
		shift = 5
	}
	switch mode {
	case vp8DC:
		var dc int32
		switch {
		case mbx == 0 && mby == 0:
			dc = 128
		case mbx == 0:
			s := int32(n / 2)
			for _, t := range top {
				s += t
			}
			dc = s >> uint(shift-1)
		case mby == 0:
			s := int32(n / 2)
			for _, l := range left {
				s += l
			}
			dc = s >> uint(shift-1)
		default:
			s := int32(n)
			for i := 0; i < n; i++ {
				s += top[i] + left[i]
			}
			dc = s >> uint(shift)
		}
		for i := range out {
			out[i] = dc
		}
	case vp8VE:
		for j := 0; j < n; j++ {
			copy(out[j*n:], top)
		}
	case vp8HE:
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				out[j*n+i] = left[j]
			}
		}
	case vp8TM:
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				out[j*n+i] = int32(vp8Clip(left[j] + top[i] - corner))
			}
		}
	}
	return out
}

func vp8FDCT(src []int32, out *[16]int32) {
	var tmp [16]int32
	for i := 0; i < 4; i++ {
		d0, d1, d2, d3 := src[i*4], src[i*4+1], src[i*4+2], src[i*4+3]
		a0, a1, a2, a3 := d0+d3, d1+d2, d1-d2, d0-d3
		tmp[i*4] = (a0 + a1) * 8
		tmp[i*4+1] = (a2*2217 + a3*5352 + 1812) >> 9
		tmp[i*4+2] = (a0 - a1) * 8
		tmp[i*4+3] = (a3*2217 - a2*5352 + 937) >> 9
	}
	for i := 0; i < 4; i++ {
		a0, a1 := tmp[i]+tmp[12+i], tmp[4+i]+tmp[8+i]
		a2, a3 := tmp[4+i]-tmp[8+i], tmp[i]-tmp[12+i]
		out[i] = (a0 + a1 + 7) >> 4
		out[4+i] = (a2*2217 + a3*5352 + 12000) >> 16
		if a3 != 0 {
			out[4+i]++
		}
		out[8+i] = (a0 - a1 + 7) >> 4
		out[12+i] = (a3*2217 - a2*5352 + 51000) >> 16
	}
}

func vp8IDCT(c *[16]int32, dst []int32) {
	const c1, c2 = 85627, 35468
	var m [4][4]int32
	for i := 0; i < 4; i++ {
		a := c[i] + c[8+i]
		b := c[i] - c[8+i]
		cc := (c[4+i]*c2)>>16 - (c[12+i]*c1)>>16
		d := (c[4+i]*c1)>>16 + (c[12+i]*c2)>>16
		m[i][0], m[i][1], m[i][2], m[i][3] = a+d, b+cc, b-cc, a-d
	}
	for j := 0; j < 4; j++ {
		dc := m[0][j] + 4
		a := dc + m[2][j]
		b := dc - m[2][j]
		cc := (m[1][j]*c2)>>16 - (m[3][j]*c1)>>16
		d := (m[1][j]*c1)>>16 + (m[3][j]*c2)>>16
		dst[j*4], dst[j*4+1], dst[j*4+2], dst[j*4+3] = (a+d)>>3, (b+cc)>>3, (b-cc)>>3, (a-d)>>3
	}
}

func vp8FWHT(dc *[16]int32, out *[16]int32) {
	var tmp [16]int32
	for i := 0; i < 4; i++ {
		in := dc[i*4:]
		a0, a1 := in[0]+in[2], in[1]+in[3]
		a2, a3 := in[1]-in[3], in[0]-in[2]
		tmp[i*4], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3] = a0+a1, a3+a2, a3-a2, a0-a1
	}
	for i := 0; i < 4; i++ {
		a0, a1 := tmp[i]+tmp[8+i], tmp[4+i]+tmp[12+i]
		a2, a3 := tmp[4+i]-tmp[12+i], tmp[i]-tmp[8+i]
		out[i], out[4+i], out[8+i], out[12+i] = (a0+a1)>>1, (a3+a2)>>1, (a3-a2)>>1, (a0-a1)>>1
	}
}

func vp8IWHT(c *[16]int32, dc *[16]int32) {
	var m [16]int32
	for i := 0; i < 4; i++ {
		a0, a1 := c[i]+c[12+i], c[4+i]+c[8+i]
		a2, a3 := c[4+i]-c[8+i], c[i]-c[12+i]
		m[i], m[8+i], m[4+i], m[12+i] = a0+a1, a0-a1, a3+a2, a3-a2
	}
	for i := 0; i < 4; i++ {
		d := m[i*4] + 3
		a0, a1 := d+m[3+i*4], m[1+i*4]+m[2+i*4]
		a2, a3 := m[1+i*4]-m[2+i*4], d-m[3+i*4]
		dc[i*4], dc[i*4+1], dc[i*4+2], dc[i*4+3] = (a0+a1)>>3, (a3+a2)>>3, (a0-a1)>>3, (a3-a2)>>3
	}
}

func vp8Quant(c *[16]int32, q [2]int32, from int) bool {
	nz := false
	for i := from; i < 16; i++ {
		qq := q[min(i, 1)]
		v := c[i]
		neg := v < 0
		if neg {
			v = -v
		}
		bias := qq * 3 / 8
		if i == 0 {
			bias = qq / 2
		}
		l := min((v+bias)/qq, 2047)
		if neg {
			l = -l
		}
		c[i] = l
		nz = nz || l != 0
	}
	return nz
}

func vp8Dequant(c *[16]int32, q [2]int32) [16]int32 {
	var d [16]int32
	for i := range c {
		d[i] = c[i] * q[min(i, 1)]
	}
	return d
}

func vp8BestMode(src, rec []uint8, stride, x0, y0, n, mbx, mby int) (int, []int32) {
	best, bestErr := vp8DC, int64(-1)
	var bestPred []int32
	for _, mode := range []int{vp8DC, vp8VE, vp8HE, vp8TM} {
		p := vp8Predict(rec, stride, x0, y0, n, mode, mbx, mby)
		var e int64
		for j := 0; j < n; j++ {
			for i := 0; i < n; i++ {
				d := int64(src[(y0+j)*stride+x0+i]) - int64(p[j*n+i])
				e += d * d
			}
		}
		if bestErr < 0 || e < bestErr {
			best, bestErr, bestPred = mode, e, p
		}
	}
	return best, bestPred
}

func (f *vp8Frame) chromaPred(mbx, mby int) (int, []int32, []int32) {
	best, bestErr := vp8DC, int64(-1)
	var bu, bv []int32
	x0, y0 := mbx*8, mby*8
	for _, mode := range []int{vp8DC, vp8VE, vp8HE, vp8TM} {
		pu := vp8Predict(f.ru, f.cs, x0, y0, 8, mode, mbx, mby)
		pv := vp8Predict(f.rv, f.cs, x0, y0, 8, mode, mbx, mby)
		var e int64
		for j := 0; j < 8; j++ {
			for i := 0; i < 8; i++ {
				o := (y0+j)*f.cs + x0 + i
				du := int64(f.u[o]) - int64(pu[j*8+i])
				dv := int64(f.v[o]) - int64(pv[j*8+i])
				e += du*du + dv*dv
			}
		}
		if bestErr < 0 || e < bestErr {
			best, bestErr, bu, bv = mode, e, pu, pv
		}
	}
	return best, bu, bv
}

func (f *vp8Frame) encodeMB(mbx, mby int) vp8MB {
	var mb vp8MB
	x0, y0 := mbx*16, mby*16
	mode, pred := vp8BestMode(f.y, f.ry, f.ys, x0, y0, 16, mbx, mby)
	mb.ymode = mode
	var dcs [16]int32
	nz := false
	for b := 0; b < 16; b++ {
		bx, by := (b%4)*4, (b/4)*4
		var res [16]int32
		for j := 0; j < 4; j++ {
			for i := 0; i < 4; i++ {
				res[j*4+i] = int32(f.y[(y0+by+j)*f.ys+x0+bx+i]) - pred[(by+j)*16+bx+i]
			}
		}
		vp8FDCT(res[:], &mb.y[b])
		dcs[b] = mb.y[b][0]
		mb.y[b][0] = 0
		if vp8Quant(&mb.y[b], f.q.y1, 1) {
			nz = true
		}
	}
	vp8FWHT(&dcs, &mb.y2)
	if vp8Quant(&mb.y2, f.q.y2, 0) {
		nz = true
	}
	dq2 := vp8Dequant(&mb.y2, f.q.y2)
	var rdc [16]int32
	vp8IWHT(&dq2, &rdc)
	for b := 0; b < 16; b++ {
		bx, by := (b%4)*4, (b/4)*4
		dq := vp8Dequant(&mb.y[b], f.q.y1)
		dq[0] = rdc[b]
		var add [16]int32
		vp8IDCT(&dq, add[:])
		for j := 0; j < 4; j++ {
			for i := 0; i < 4; i++ {
				f.ry[(y0+by+j)*f.ys+x0+bx+i] = vp8Clip(pred[(by+j)*16+bx+i] + add[j*4+i])
			}
		}
	}
	cmode, pu, pv := f.chromaPred(mbx, mby)
	mb.cmode = cmode
	cx0, cy0 := mbx*8, mby*8
	for p, plane := range [2][]uint8{f.u, f.v} {
		pred, rec, coef := pu, f.ru, &mb.u
		if p == 1 {
			pred, rec, coef = pv, f.rv, &mb.v
		}
		for b := 0; b < 4; b++ {
			bx, by := (b%2)*4, (b/2)*4
			var res [16]int32
			for j := 0; j < 4; j++ {
				for i := 0; i < 4; i++ {
					res[j*4+i] = int32(plane[(cy0+by+j)*f.cs+cx0+bx+i]) - pred[(by+j)*8+bx+i]
				}
			}
			vp8FDCT(res[:], &coef[b])
			if vp8Quant(&coef[b], f.q.uv, 0) {
				nz = true
			}
			dq := vp8Dequant(&coef[b], f.q.uv)
			var add [16]int32
			vp8IDCT(&dq, add[:])
			for j := 0; j < 4; j++ {
				for i := 0; i < 4; i++ {
					rec[(cy0+by+j)*f.cs+cx0+bx+i] = vp8Clip(pred[(by+j)*8+bx+i] + add[j*4+i])
				}
			}
		}
	}
	mb.skip = !nz
	return mb
}

func (f *vp8Frame) tokens() *vp8Tokens {
	t := &vp8Tokens{}
	leftY2, upY2 := 0, make([]int, f.mbw)
	upNZ := make([][8]int, f.mbw)
	for mby := 0; mby < f.mbh; mby++ {
		leftY2 = 0
		var leftNZ [8]int
		for mbx := 0; mbx < f.mbw; mbx++ {
			mb := &f.mbs[mby*f.mbw+mbx]
			if mb.skip {
				leftY2, upY2[mbx] = 0, 0
				leftNZ, upNZ[mbx] = [8]int{}, [8]int{}
				continue
			}
			nz := t.block(vp8PlaneY2, leftY2+upY2[mbx], &mb.y2, 0)
			leftY2, upY2[mbx] = nz, nz
			up := &upNZ[mbx]
			for y := 0; y < 4; y++ {
				l := leftNZ[y]
				for x := 0; x < 4; x++ {
					l = t.block(vp8PlaneYAC, l+up[x], &mb.y[y*4+x], 1)
					up[x] = l
				}
				leftNZ[y] = l
			}
			for c, blocks := range [2]*[4][16]int32{&mb.u, &mb.v} {
				for y := 0; y < 2; y++ {
					l := leftNZ[4+c*2+y]
					for x := 0; x < 2; x++ {
						l = t.block(vp8PlaneUV, l+up[4+c*2+x], &blocks[y*2+x], 0)
						up[4+c*2+x] = l
					}
					leftNZ[4+c*2+y] = l
				}
			}
		}
	}
	return t
}

func encodeVP8(img *image.NRGBA, qi int) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	f := &vp8Frame{w: w, h: h, mbw: (w + 15) / 16, mbh: (h + 15) / 16, qi: qi}
	vp8Planes(img, f)
	f.ry, f.ru, f.rv = make([]uint8, len(f.y)), make([]uint8, len(f.u)), make([]uint8, len(f.v))
	f.q.y1 = [2]int32{vp8DCQ[qi], vp8ACQ[qi]}
	f.q.y2 = [2]int32{vp8DCQ[qi] * 2, max(8, vp8ACQ[qi]*155/100)}
	f.q.uv = [2]int32{vp8DCQ[min(qi, 117)], vp8ACQ[qi]}
	f.mbs = make([]vp8MB, f.mbw*f.mbh)
	skips := 0
	for mby := 0; mby < f.mbh; mby++ {
		for mbx := 0; mbx < f.mbw; mbx++ {
			mb := f.encodeMB(mbx, mby)
			f.mbs[mby*f.mbw+mbx] = mb
			if mb.skip {
				skips++
			}
		}
	}
	toks := f.tokens()
	probs, update := toks.probs()
	p2 := newBoolEnc()
	flat := probs[:]
	for _, b := range toks.bits {
		if b.slot < 0 {
			p2.put(b.prob, b.bit)
			continue
		}
		s := int(b.slot)
		p2.put(flat[s/(8*3*11)][s/(3*11)%8][s/11%3][s%11], b.bit)
	}
	p1 := newBoolEnc()
	p1.literal(0, 2)
	p1.put(128, false)
	p1.put(128, false)
	p1.literal(0, 6)
	p1.literal(0, 3)
	p1.put(128, false)
	p1.literal(0, 2)
	p1.literal(uint32(qi), 7)
	for i := 0; i < 5; i++ {
		p1.put(128, false)
	}
	p1.put(128, false)
	for i := 0; i < 4*8*3*11; i++ {
		pl, band, ctx, node := i/(8*3*11), i/(3*11)%8, i/11%3, i%11
		up := vp8UpdateProb[pl][band][ctx][node]
		p1.put(up, update[i])
		if update[i] {
			p1.literal(uint32(probs[pl][band][ctx][node]), 8)
		}
	}
	skipProb := uint8(min(254, max(1, (len(f.mbs)-skips)*256/len(f.mbs))))
	p1.put(128, true)
	p1.literal(uint32(skipProb), 8)
	for _, mb := range f.mbs {
		p1.put(skipProb, mb.skip)
		p1.put(145, true)
		switch mb.ymode {
		case vp8DC:
			p1.put(156, false)
			p1.put(163, false)
		case vp8VE:
			p1.put(156, false)
			p1.put(163, true)
		case vp8HE:
			p1.put(156, true)
			p1.put(128, false)
		case vp8TM:
			p1.put(156, true)
			p1.put(128, true)
		}
		switch mb.cmode {
		case vp8DC:
			p1.put(142, false)
		case vp8VE:
			p1.put(142, true)
			p1.put(114, false)
		case vp8HE:
			p1.put(142, true)
			p1.put(114, true)
			p1.put(183, false)
		case vp8TM:
			p1.put(142, true)
			p1.put(114, true)
			p1.put(183, true)
		}
	}
	first, rest := p1.bytes(), p2.bytes()
	out := make([]byte, 10, 10+len(first)+len(rest))
	tag := uint32(len(first))<<5 | 1<<4
	out[0], out[1], out[2] = byte(tag), byte(tag>>8), byte(tag>>16)
	out[3], out[4], out[5] = 0x9d, 0x01, 0x2a
	binary.LittleEndian.PutUint16(out[6:], uint16(w))
	binary.LittleEndian.PutUint16(out[8:], uint16(h))
	out = append(out, first...)
	return append(out, rest...)
}

func riffChunk(out []byte, id string, data []byte) []byte {
	out = append(out, id...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func filterAlpha(a []byte, w, method int) []byte {
	out := make([]byte, len(a)+1)
	out[0] = byte(method << 2)
	for i, v := range a {
		x, y := i%w, i/w
		var pred int
		switch {
		case method == 0:
		case x == 0 && y == 0:
		case y == 0:
			pred = int(a[i-1])
		case x == 0:
			pred = int(a[i-w])
		case method == 1:
			pred = int(a[i-1])
		case method == 2:
			pred = int(a[i-w])
		default:
			pred = min(255, max(0, int(a[i-1])+int(a[i-w])-int(a[i-w-1])))
		}
		out[i+1] = v - byte(pred)
	}
	return out
}

func alphaChunk(a []byte, w int) []byte {
	var best []byte
	bestSize := 0
	for method := 0; method < 4; method++ {
		f := filterAlpha(a, w, method)
		if n := gzipSize(f); best == nil || n < bestSize {
			best, bestSize = f, n
		}
	}
	return best
}

func encodeWebP(img *image.NRGBA, qi int) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	vp8 := encodeVP8(img, qi)
	var alpha []byte
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.Pix[img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y)+3] != 255 {
				alpha = make([]byte, 0, w*h+1)
				break
			}
		}
		if alpha != nil {
			break
		}
	}
	var body []byte
	body = append(body, "WEBP"...)
	if alpha != nil {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				alpha = append(alpha, img.Pix[img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y)+3])
			}
		}
		alpha = alphaChunk(alpha, w)
		x := make([]byte, 10)
		x[0] = 0x10
		put24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
		put24(x[4:], w-1)
		put24(x[7:], h-1)
		body = riffChunk(body, "VP8X", x)
		body = riffChunk(body, "ALPH", alpha)
	}
	body = riffChunk(body, "VP8 ", vp8)
	out := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}
