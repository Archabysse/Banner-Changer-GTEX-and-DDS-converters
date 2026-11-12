// Package wrap convertit les textures GTEX de Warhammer: Mark of Chaos vers
// le format DDS, et inversement, sans perte d'information.
//
// Format GTEX (octets, little-endian) :
//
//	0x00  6   "GTEX00"
//	0x06  4   étiquette de type : "DXT1".."DXT5" (autre chose = non compressé)
//	0x0A  4   g0  nombre de niveaux d'image (6 = avec mipmaps, 1 = sans)
//	0x0E  4   g1  largeur d'origine du contenu
//	0x12  4   g2  hauteur d'origine du contenu
//	0x16  4   g3  profondeur        \
//	0x1A  4   g4  niveaux de mipmap  |  ce sont les champs d'un D3DXIMAGE_INFO
//	0x1E  4   g5  format (21 = A8R8G8B8) |  de l'image source (TGA 91x256...)
//	0x22  4   g6  type de ressource  |
//	0x26  4   g7  format de fichier  /
//	0x2A  4   g8  largeur finale (le conteneur, puissance de 2)
//	0x2E  4   g9  hauteur finale
//	0x32  4   g10 taille du niveau de base
//	0x36  ... niveau de base (brut), puis pour chaque mipmap :
//	          largeur(4) hauteur(4) taille(4) données
package wrap

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var le = binary.LittleEndian

const (
	gtexHeaderLen = 54
	ddsHeaderLen  = 128
	maxDim        = 32768
	sigGTX1       = 0x31585447 // "GTX1" : signature des métadonnées dans le DDS
)

// Indices des 11 mots de 32 bits qui suivent l'étiquette dans l'en-tête GTEX.
const (
	HLevels = iota
	HOrigW
	HOrigH
	HDepth
	HMips
	HFmt
	HRType
	HIFF
	HFinalW
	HFinalH
	HSize
	hCount
)

// Header contient g0..g10.
type Header [hCount]uint32

// defaultHeader : valeurs observées sur les textures connues. Le format de
// l'image source (g5) vaut 20 (R8G8B8) pour le DXT1 et 21 (A8R8G8B8) sinon.
func defaultHeader(typ, w, h, size int) Header {
	fmtSrc := uint32(21)
	if typ == 1 {
		fmtSrc = 20
	}
	return Header{1, uint32(w), uint32(h), 1, 1, fmtSrc, 3, 2, uint32(w), uint32(h), uint32(size)}
}

// Level est un niveau d'image (sans l'éventuel mini-en-tête GTEX).
type Level struct {
	W, H int
	Data []byte
}

// Texture est la représentation commune GTEX/DDS.
type Texture struct {
	Type    int // 0 = non compressé (BGRA 32 bits), 1..5 = DXTn
	Tag     [4]byte
	Hdr     Header
	Levels  []Level
	Trailer []byte // octets non reconnus après la dernière mipmap, conservés tels quels
}

var (
	ErrNotGTEX = errors.New("ce n'est pas un fichier GTEX (en-tête « GTEX00 » absent)")
	ErrNotDDS  = errors.New("ce n'est pas un fichier DDS (en-tête « DDS » absent)")
)

func tagString(tag [4]byte) string {
	b := make([]byte, 4)
	for i, c := range tag {
		if c < 32 || c > 126 {
			c = '?'
		}
		b[i] = c
	}
	return string(b)
}

func defaultTag(typ int) [4]byte {
	return [4]byte{'D', 'X', 'T', byte('0' + typ)}
}

// tagType déduit le type de l'étiquette, comme le ddswrapper d'origine :
// « DXTn » donne n, toute autre étiquette est traitée comme non compressée.
func tagType(tag [4]byte) (int, error) {
	if tag[0] == 'D' && tag[1] == 'X' && tag[2] == 'T' {
		if tag[3] >= '0' && tag[3] <= '5' {
			return int(tag[3] - '0'), nil
		}
		return 0, fmt.Errorf("type GTEX non pris en charge (« %s »)", tagString(tag))
	}
	return 0, nil
}

// TypeName donne un nom lisible du type.
func TypeName(typ int) string {
	if typ == 0 {
		return "non compressé"
	}
	return fmt.Sprintf("DXT%d", typ)
}

// levelSize : taille en octets d'un niveau w x h.
func levelSize(typ, w, h int) int {
	if typ == 0 {
		return w * h * 4
	}
	bs := 16
	if typ == 1 {
		bs = 8
	}
	return ((w + 3) / 4) * ((h + 3) / 4) * bs
}

func nextDims(w, h int) (int, int) {
	w /= 2
	h /= 2
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// splitPayload découpe la charge utile GTEX : niveau de base brut puis niveaux
// précédés de (largeur, hauteur, taille). Tout ce qui ne suit pas ce schéma
// est renvoyé comme trailer.
func splitPayload(typ, w, h int, p []byte) (levels []Level, trailer []byte, err error) {
	base := levelSize(typ, w, h)
	if len(p) < base {
		return nil, nil, fmt.Errorf("données d'image tronquées (%d octets au lieu de %d)", len(p), base)
	}
	levels = []Level{{w, h, p[:base]}}
	rest := p[base:]
	cw, ch := w, h
	for len(rest) >= 12 {
		nw, nh := nextDims(cw, ch)
		lw, lh, ls := int(le.Uint32(rest)), int(le.Uint32(rest[4:])), int(le.Uint32(rest[8:]))
		if lw != nw || lh != nh || ls != levelSize(typ, nw, nh) || 12+ls > len(rest) {
			break
		}
		levels = append(levels, Level{nw, nh, rest[12 : 12+ls]})
		rest = rest[12+ls:]
		cw, ch = nw, nh
	}
	return levels, rest, nil
}

// ParseGTEX lit un fichier GTEX.
func ParseGTEX(b []byte) (*Texture, []string, error) {
	if len(b) < 6 || string(b[:6]) != "GTEX00" {
		return nil, nil, ErrNotGTEX
	}
	if len(b) < gtexHeaderLen {
		return nil, nil, errors.New("fichier GTEX tronqué (en-tête incomplet)")
	}
	var tag [4]byte
	copy(tag[:], b[6:10])
	typ, err := tagType(tag)
	if err != nil {
		return nil, nil, err
	}
	t := &Texture{Type: typ, Tag: tag}
	for i := range t.Hdr {
		t.Hdr[i] = le.Uint32(b[10+4*i:])
	}
	var warns []string
	w, h := int(t.Hdr[HFinalW]), int(t.Hdr[HFinalH])
	if w <= 0 || h <= 0 {
		w, h = int(t.Hdr[HOrigW]), int(t.Hdr[HOrigH])
		warns = append(warns, "dimensions finales nulles dans l'en-tête : dimensions d'origine utilisées")
		t.Hdr[HFinalW], t.Hdr[HFinalH] = uint32(w), uint32(h)
	}
	if w <= 0 || h <= 0 || w > maxDim || h > maxDim {
		return nil, nil, fmt.Errorf("dimensions invalides dans l'en-tête GTEX (%dx%d)", w, h)
	}
	levels, trailer, err := splitPayload(typ, w, h, b[gtexHeaderLen:])
	if err != nil {
		return nil, nil, err
	}
	t.Levels, t.Trailer = levels, trailer
	if len(trailer) > 0 {
		warns = append(warns, fmt.Sprintf("%d octet(s) non reconnu(s) après les mipmaps : conservés tels quels", len(trailer)))
	}
	return t, warns, nil
}

// buildGTEX assemble un fichier GTEX.
func buildGTEX(tag [4]byte, hdr Header, levels []Level, trailer []byte) []byte {
	n := gtexHeaderLen + len(trailer)
	for i, l := range levels {
		n += len(l.Data)
		if i > 0 {
			n += 12
		}
	}
	out := make([]byte, gtexHeaderLen, n)
	copy(out, "GTEX00")
	copy(out[6:], tag[:])
	for i, v := range hdr {
		le.PutUint32(out[10+4*i:], v)
	}
	for i, l := range levels {
		if i > 0 {
			var m [12]byte
			le.PutUint32(m[0:], uint32(l.W))
			le.PutUint32(m[4:], uint32(l.H))
			le.PutUint32(m[8:], uint32(len(l.Data)))
			out = append(out, m[:]...)
		}
		out = append(out, l.Data...)
	}
	return append(out, trailer...)
}

// BuildDDS écrit un DDS standard. Les 11 mots de l'en-tête GTEX sont conservés
// dans dwReserved1, l'étiquette dans dwCaps3 et la signature dans dwReserved2 :
// ces champs sont ignorés par les lecteurs DDS et permettent de reconstruire
// l'en-tête GTEX exact. swap échange largeur et hauteur dans le DDS (option -S).
func BuildDDS(t *Texture, swap bool) []byte {
	l0 := t.Levels[0]
	dw, dh := l0.W, l0.H
	if swap {
		dw, dh = dh, dw
	}
	flags := uint32(0x1 | 0x2 | 0x4 | 0x1000) // CAPS | HEIGHT | WIDTH | PIXELFORMAT
	caps := uint32(0x1000)                    // TEXTURE
	if len(t.Levels) > 1 {
		flags |= 0x20000 // MIPMAPCOUNT
		caps |= 0x8 | 0x400000
	}
	n := ddsHeaderLen + len(t.Trailer)
	for _, l := range t.Levels {
		n += len(l.Data)
	}
	out := make([]byte, ddsHeaderLen, n)
	copy(out, "DDS ")
	le.PutUint32(out[4:], 124)
	le.PutUint32(out[12:], uint32(dh))
	le.PutUint32(out[16:], uint32(dw))
	le.PutUint32(out[24:], 1)
	le.PutUint32(out[28:], uint32(len(t.Levels)))
	for i, v := range t.Hdr {
		le.PutUint32(out[32+4*i:], v)
	}
	le.PutUint32(out[76:], 32) // DDS_PIXELFORMAT.dwSize
	if t.Type == 0 {
		flags |= 0x8 // PITCH
		le.PutUint32(out[20:], uint32(l0.W*4))
		le.PutUint32(out[80:], 0x41) // RGB | ALPHAPIXELS
		le.PutUint32(out[88:], 32)
		le.PutUint32(out[92:], 0x00FF0000)
		le.PutUint32(out[96:], 0x0000FF00)
		le.PutUint32(out[100:], 0x000000FF)
		le.PutUint32(out[104:], 0xFF000000)
	} else {
		flags |= 0x80000 // LINEARSIZE
		le.PutUint32(out[20:], uint32(len(l0.Data)))
		le.PutUint32(out[80:], 0x4) // FOURCC
		fcc := defaultTag(t.Type)
		copy(out[84:], fcc[:])
	}
	le.PutUint32(out[8:], flags)
	le.PutUint32(out[108:], caps)
	copy(out[116:], t.Tag[:])
	le.PutUint32(out[124:], sigGTX1)
	for _, l := range t.Levels {
		out = append(out, l.Data...)
	}
	return append(out, t.Trailer...)
}

// Meta : métadonnées GTEX retrouvées dans un DDS produit par ddswrapper2.
type Meta struct {
	Hdr Header
	Tag [4]byte
}

// DDS : contenu d'un fichier DDS lu.
type DDS struct {
	Type      int
	W, H      int
	Levels    []Level // données au format GTEX (BGRA pour le non compressé)
	Trailer   []byte
	Meta      *Meta  // signature GTX1 présente
	Legacy    bool   // produit par l'ancien ddswrapper
	LegacyHdr Header // champs récupérés de l'ancien ddswrapper
	Info      []string
	Warn      []string
}

func dxgiType(f uint32) int {
	switch f {
	case 70, 71, 72:
		return 1
	case 73, 74, 75:
		return 3
	case 76, 77, 78:
		return 5
	}
	return -1
}

// ParseDDS lit un fichier DDS (en-tête standard, DX10 compris).
func ParseDDS(b []byte) (*DDS, error) {
	if len(b) < 4 || string(b[:4]) != "DDS " {
		return nil, ErrNotDDS
	}
	if len(b) < ddsHeaderLen || le.Uint32(b[4:]) != 124 {
		return nil, errors.New("en-tête DDS invalide ou tronqué")
	}
	d := &DDS{}
	flags := le.Uint32(b[8:])
	h, w := int(le.Uint32(b[12:])), int(le.Uint32(b[16:]))
	mipCount := int(le.Uint32(b[28:]))
	pfFlags, fcc, bits := le.Uint32(b[80:]), string(b[84:88]), int(le.Uint32(b[88:]))
	rm, gm, bm, am := le.Uint32(b[92:]), le.Uint32(b[96:]), le.Uint32(b[100:]), le.Uint32(b[104:])
	if w <= 0 || h <= 0 || w > maxDim || h > maxDim {
		return nil, fmt.Errorf("dimensions DDS invalides (%dx%d)", w, h)
	}
	if le.Uint32(b[112:])&(0x200|0x200000) != 0 {
		return nil, errors.New("les cubemaps et textures volumiques ne sont pas pris en charge")
	}
	d.W, d.H = w, h
	off := ddsHeaderLen
	typ := -1
	var srcBits int // >0 : non compressé à convertir
	switch {
	case pfFlags&0x4 != 0:
		switch fcc {
		case "DXT1", "DXT2", "DXT3", "DXT4", "DXT5":
			typ = int(fcc[3] - '0')
		case "DX10":
			if len(b) < ddsHeaderLen+20 {
				return nil, errors.New("en-tête DDS DX10 tronqué")
			}
			dxgi, dim, misc, arr := le.Uint32(b[128:]), le.Uint32(b[132:]), le.Uint32(b[136:]), le.Uint32(b[140:])
			typ = dxgiType(dxgi)
			if typ < 0 {
				return nil, fmt.Errorf("format DXGI %d non pris en charge (seuls BC1, BC2 et BC3 le sont)", dxgi)
			}
			if dim != 3 || arr > 1 || misc&0x4 != 0 {
				return nil, errors.New("seules les textures 2D simples sont prises en charge")
			}
			off += 20
		default:
			return nil, fmt.Errorf("format DDS « %s » non pris en charge (seuls DXT1 à DXT5 et RVB 24/32 bits le sont)", tagString([4]byte{fcc[0], fcc[1], fcc[2], fcc[3]}))
		}
	case pfFlags&0x40 != 0 && (bits == 32 || bits == 24):
		typ = 0
		srcBits = bits
	default:
		return nil, errors.New("format de pixels DDS non pris en charge (seuls DXT1 à DXT5 et RVB 24/32 bits le sont)")
	}
	d.Type = typ
	if le.Uint32(b[124:]) == sigGTX1 {
		m := &Meta{}
		for i := range m.Hdr {
			m.Hdr[i] = le.Uint32(b[32+4*i:])
		}
		copy(m.Tag[:], b[116:120])
		d.Meta = m
	}
	// DDS de l'ancien ddswrapper : dwFlags contient g0 (1 ou 6), donc jamais le
	// bit PIXELFORMAT que tout éditeur positionne ; le reste de la charge utile
	// est de l'octet GTEX brut.
	d.Legacy = d.Meta == nil && typ > 0 && flags&0x1000 == 0
	if d.Legacy {
		d.LegacyHdr[HLevels] = flags
		d.LegacyHdr[HDepth] = le.Uint32(b[24:])
		d.LegacyHdr[HMips] = le.Uint32(b[28:])
		d.LegacyHdr[HFmt] = le.Uint32(b[32:])
		d.LegacyHdr[HRType] = le.Uint32(b[36:])
		d.LegacyHdr[HIFF] = le.Uint32(b[40:])
		levels, trailer, err := splitPayload(typ, w, h, b[off:])
		if err != nil {
			return nil, err
		}
		d.Levels, d.Trailer = levels, trailer
		d.Info = append(d.Info, "DDS de l'ancien ddswrapper reconnu")
		if len(trailer) > 0 {
			d.Warn = append(d.Warn, fmt.Sprintf("%d octet(s) non reconnu(s) après les mipmaps : conservés tels quels", len(trailer)))
		}
		return d, nil
	}
	n := 1
	if flags&0x20000 != 0 && mipCount > 1 {
		n = mipCount
		if n > 32 {
			n = 32
		}
	}
	p := b[off:]
	cw, ch := w, h
	for i := 0; i < n; i++ {
		var src int
		if srcBits > 0 {
			src = ((cw*srcBits + 7) / 8) * ch
		} else {
			src = levelSize(typ, cw, ch)
		}
		if len(p) < src {
			if i == 0 {
				return nil, fmt.Errorf("données d'image tronquées (%d octets au lieu de %d)", len(p), src)
			}
			d.Warn = append(d.Warn, fmt.Sprintf("DDS tronqué : %d mipmap(s) sur %d lu(s)", i, n))
			break
		}
		data := p[:src]
		if srcBits > 0 {
			conv, err := toBGRA(data, cw, ch, srcBits, rm, gm, bm, am)
			if err != nil {
				return nil, err
			}
			data = conv
		}
		d.Levels = append(d.Levels, Level{cw, ch, data})
		p = p[src:]
		cw, ch = nextDims(cw, ch)
	}
	d.Trailer = p
	if srcBits > 0 && !(srcBits == 32 && rm == 0x00FF0000 && gm == 0xFF00 && bm == 0xFF && am == 0xFF000000) {
		d.Info = append(d.Info, fmt.Sprintf("pixels %d bits convertis en A8R8G8B8", srcBits))
	}
	return d, nil
}

// chanShift : décalage d'un masque de canal de 8 bits ; ok=false si le masque
// n'est pas un octet contigu. Un masque nul est accepté (canal absent).
func chanShift(m uint32) (shift uint, ok bool) {
	if m == 0 {
		return 0, true
	}
	for m&1 == 0 {
		m >>= 1
		shift++
	}
	return shift, m == 0xFF
}

// toBGRA convertit des pixels 24/32 bits (masques quelconques d'octets) en BGRA 32 bits.
func toBGRA(src []byte, w, h, bits int, rm, gm, bm, am uint32) ([]byte, error) {
	rs, ok1 := chanShift(rm)
	gs, ok2 := chanShift(gm)
	bs, ok3 := chanShift(bm)
	as, ok4 := chanShift(am)
	if !(ok1 && ok2 && ok3 && ok4) || (rm|gm|bm) == 0 {
		return nil, errors.New("masques de couleur DDS non pris en charge (canaux de 8 bits attendus)")
	}
	pitch := (w*bits + 7) / 8
	out := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		row := src[y*pitch:]
		for x := 0; x < w; x++ {
			var v uint32
			if bits == 32 {
				v = le.Uint32(row[x*4:])
			} else {
				v = uint32(row[x*3]) | uint32(row[x*3+1])<<8 | uint32(row[x*3+2])<<16
			}
			o := (y*w + x) * 4
			out[o] = byte((v & bm) >> bs)
			out[o+1] = byte((v & gm) >> gs)
			out[o+2] = byte((v & rm) >> rs)
			if am == 0 {
				out[o+3] = 255
			} else {
				out[o+3] = byte((v & am) >> as)
			}
		}
	}
	return out, nil
}
