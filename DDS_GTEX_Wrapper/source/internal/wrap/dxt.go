package wrap

import "math"

// Décodeur/encodeur DXT1/3/5 minimal, utilisé uniquement pour régénérer les
// mipmaps quand le DDS n'en contient pas. Les niveaux déjà présents ne sont
// jamais recompressés.

type rgba struct{ R, G, B, A uint8 }

func decode565(c uint16) (int, int, int) {
	r, g, b := int(c>>11)&31, int(c>>5)&63, int(c)&31
	return r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2
}

func colorPalette(c0, c1 uint16, fourColor bool) [4][3]int {
	var p [4][3]int
	p[0][0], p[0][1], p[0][2] = decode565(c0)
	p[1][0], p[1][1], p[1][2] = decode565(c1)
	for i := 0; i < 3; i++ {
		if fourColor {
			p[2][i] = (2*p[0][i] + p[1][i]) / 3
			p[3][i] = (p[0][i] + 2*p[1][i]) / 3
		} else {
			p[2][i] = (p[0][i] + p[1][i]) / 2
		}
	}
	return p
}

func alphaTable(a0, a1 int) [8]int {
	var t [8]int
	t[0], t[1] = a0, a1
	if a0 > a1 {
		for i := 1; i < 7; i++ {
			t[i+1] = ((7-i)*a0 + i*a1) / 7
		}
	} else {
		for i := 1; i < 5; i++ {
			t[i+1] = ((5-i)*a0 + i*a1) / 5
		}
		t[6], t[7] = 0, 255
	}
	return t
}

// decodeLevel : niveau compressé (ou BGRA) vers des pixels RGBA.
func decodeLevel(typ, w, h int, data []byte) []rgba {
	px := make([]rgba, w*h)
	if typ == 0 {
		for i := range px {
			px[i] = rgba{data[i*4+2], data[i*4+1], data[i*4], data[i*4+3]}
		}
		return px
	}
	bs := 16
	if typ == 1 {
		bs = 8
	}
	bw, bh := (w+3)/4, (h+3)/4
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			blk := data[(by*bw+bx)*bs:]
			var alpha [16]int
			cb := blk
			switch typ {
			case 1:
				for i := range alpha {
					alpha[i] = 255
				}
			case 2, 3:
				for i := 0; i < 16; i++ {
					alpha[i] = int(blk[i/2]>>(4*uint(i%2))&15) * 17
				}
				cb = blk[8:]
			default:
				t := alphaTable(int(blk[0]), int(blk[1]))
				var bits uint64
				for i := 0; i < 6; i++ {
					bits |= uint64(blk[2+i]) << (8 * uint(i))
				}
				for i := 0; i < 16; i++ {
					alpha[i] = t[(bits>>(3*uint(i)))&7]
				}
				cb = blk[8:]
			}
			c0, c1 := uint16(cb[0])|uint16(cb[1])<<8, uint16(cb[2])|uint16(cb[3])<<8
			threeColor := typ == 1 && c0 <= c1
			pal := colorPalette(c0, c1, !threeColor)
			idx := le.Uint32(cb[4:])
			for i := 0; i < 16; i++ {
				x, y := bx*4+i%4, by*4+i/4
				if x >= w || y >= h {
					continue
				}
				s := (idx >> (2 * uint(i))) & 3
				if threeColor && s == 3 {
					px[y*w+x] = rgba{}
					continue
				}
				px[y*w+x] = rgba{uint8(pal[s][0]), uint8(pal[s][1]), uint8(pal[s][2]), uint8(alpha[i])}
			}
		}
	}
	return px
}

func pack565(p [3]float64) uint16 {
	q := func(v, max float64) int {
		v = math.Max(0, math.Min(255, v))
		return int(v*max/255 + 0.5)
	}
	return uint16(q(p[0], 31)<<11 | q(p[1], 63)<<5 | q(p[2], 31))
}

func nearest(pal [4][3]int, n int, r, g, b int) uint32 {
	best, bi := 1<<30, 0
	for i := 0; i < n; i++ {
		dr, dg, db := pal[i][0]-r, pal[i][1]-g, pal[i][2]-b
		if d := dr*dr + dg*dg + db*db; d < best {
			best, bi = d, i
		}
	}
	return uint32(bi)
}

// encodeColor écrit un bloc de couleur de 8 octets (ajustement sur l'axe principal).
func encodeColor(dst []byte, blk *[16]rgba, dxt1 bool) {
	var pts [16][3]float64
	n, transparent := 0, false
	for _, p := range blk {
		if dxt1 && p.A < 128 {
			transparent = true
			continue
		}
		pts[n] = [3]float64{float64(p.R), float64(p.G), float64(p.B)}
		n++
	}
	put := func(c0, c1 uint16, idx uint32) {
		dst[0], dst[1], dst[2], dst[3] = byte(c0), byte(c0>>8), byte(c1), byte(c1>>8)
		le.PutUint32(dst[4:], idx)
	}
	if n == 0 {
		put(0, 0, 0xFFFFFFFF) // mode 3 couleurs, tout transparent
		return
	}
	var mean [3]float64
	for i := 0; i < n; i++ {
		for k := 0; k < 3; k++ {
			mean[k] += pts[i][k] / float64(n)
		}
	}
	var cov [3][3]float64
	for i := 0; i < n; i++ {
		var d [3]float64
		for k := 0; k < 3; k++ {
			d[k] = pts[i][k] - mean[k]
		}
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				cov[a][b] += d[a] * d[b]
			}
		}
	}
	axis := [3]float64{1, 1, 1}
	for it := 0; it < 8; it++ {
		var v [3]float64
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				v[a] += cov[a][b] * axis[b]
			}
		}
		norm := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
		if norm < 1e-9 {
			axis = [3]float64{0, 0, 0}
			break
		}
		axis = [3]float64{v[0] / norm, v[1] / norm, v[2] / norm}
	}
	tmin, tmax := math.Inf(1), math.Inf(-1)
	for i := 0; i < n; i++ {
		t := 0.0
		for k := 0; k < 3; k++ {
			t += (pts[i][k] - mean[k]) * axis[k]
		}
		tmin, tmax = math.Min(tmin, t), math.Max(tmax, t)
	}
	var hi, lo [3]float64
	for k := 0; k < 3; k++ {
		hi[k], lo[k] = mean[k]+axis[k]*tmax, mean[k]+axis[k]*tmin
	}
	c0, c1 := pack565(hi), pack565(lo)
	if transparent {
		if c0 > c1 {
			c0, c1 = c1, c0
		}
		pal := colorPalette(c0, c1, false)
		var idx uint32
		for i, p := range blk {
			s := uint32(3)
			if p.A >= 128 {
				s = nearest(pal, 3, int(p.R), int(p.G), int(p.B))
			}
			idx |= s << (2 * uint(i))
		}
		put(c0, c1, idx)
		return
	}
	if c0 < c1 {
		c0, c1 = c1, c0
	}
	if c0 == c1 {
		put(c0, c1, 0) // aplat : l'indice 0 est exact dans les deux modes
		return
	}
	pal := colorPalette(c0, c1, true)
	var idx uint32
	for i, p := range blk {
		idx |= nearest(pal, 4, int(p.R), int(p.G), int(p.B)) << (2 * uint(i))
	}
	put(c0, c1, idx)
}

func encodeAlphaDXT3(dst []byte, blk *[16]rgba) {
	for i := 0; i < 16; i += 2 {
		lo := byte((int(blk[i].A)*15 + 127) / 255)
		hi := byte((int(blk[i+1].A)*15 + 127) / 255)
		dst[i/2] = lo | hi<<4
	}
}

func encodeAlphaDXT5(dst []byte, blk *[16]rgba) {
	amin, amax := 255, 0
	for _, p := range blk {
		a := int(p.A)
		if a < amin {
			amin = a
		}
		if a > amax {
			amax = a
		}
	}
	dst[0], dst[1] = byte(amax), byte(amin)
	for i := 2; i < 8; i++ {
		dst[i] = 0
	}
	if amin == amax {
		return // tous les indices à 0
	}
	t := alphaTable(amax, amin)
	var bits uint64
	for i, p := range blk {
		best, bi := 1<<30, 0
		for k := 0; k < 8; k++ {
			d := int(p.A) - t[k]
			if d < 0 {
				d = -d
			}
			if d < best {
				best, bi = d, k
			}
		}
		bits |= uint64(bi) << (3 * uint(i))
	}
	for i := 0; i < 6; i++ {
		dst[2+i] = byte(bits >> (8 * uint(i)))
	}
}

// encodeLevel : pixels RGBA vers un niveau du type demandé.
func encodeLevel(typ, w, h int, px []rgba) []byte {
	out := make([]byte, levelSize(typ, w, h))
	if typ == 0 {
		for i, p := range px {
			out[i*4], out[i*4+1], out[i*4+2], out[i*4+3] = p.B, p.G, p.R, p.A
		}
		return out
	}
	bs := 16
	if typ == 1 {
		bs = 8
	}
	bw, bh := (w+3)/4, (h+3)/4
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			var blk [16]rgba
			for i := 0; i < 16; i++ {
				x, y := bx*4+i%4, by*4+i/4
				if x >= w {
					x = w - 1
				}
				if y >= h {
					y = h - 1
				}
				blk[i] = px[y*w+x]
			}
			dst := out[(by*bw+bx)*bs:]
			switch typ {
			case 1:
				encodeColor(dst, &blk, true)
			case 2, 3:
				encodeAlphaDXT3(dst, &blk)
				encodeColor(dst[8:], &blk, false)
			default:
				encodeAlphaDXT5(dst, &blk)
				encodeColor(dst[8:], &blk, false)
			}
		}
	}
	return out
}

// downsample : réduction par moyenne simple 2x2 (c'est ce qui se rapproche le
// plus des mipmaps présents dans les fichiers du jeu).
func downsample(px []rgba, w, h int) ([]rgba, int, int) {
	nw, nh := nextDims(w, h)
	out := make([]rgba, nw*nh)
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			var s [4]int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx, sy := 2*x+dx, 2*y+dy
					if sx >= w {
						sx = w - 1
					}
					if sy >= h {
						sy = h - 1
					}
					p := px[sy*w+sx]
					s[0], s[1], s[2], s[3] = s[0]+int(p.R), s[1]+int(p.G), s[2]+int(p.B), s[3]+int(p.A)
				}
			}
			out[y*nw+x] = rgba{uint8((s[0] + 2) / 4), uint8((s[1] + 2) / 4), uint8((s[2] + 2) / 4), uint8((s[3] + 2) / 4)}
		}
	}
	return out, nw, nh
}

// GenerateMips produit jusqu'à count mipmaps à partir du niveau de base
// (moins si l'image atteint 1x1 avant).
func GenerateMips(typ int, l0 Level, count int) []Level {
	var out []Level
	px := decodeLevel(typ, l0.W, l0.H, l0.Data)
	w, h := l0.W, l0.H
	for i := 0; i < count && (w > 1 || h > 1); i++ {
		px, w, h = downsample(px, w, h)
		out = append(out, Level{w, h, encodeLevel(typ, w, h, px)})
	}
	return out
}
