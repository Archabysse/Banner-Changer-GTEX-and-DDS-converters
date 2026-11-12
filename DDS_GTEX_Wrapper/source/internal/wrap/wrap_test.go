package wrap

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Skipf("exemple de texture absent (%s) : voir testdata/README.md", name)
	}
	return b
}

func psnr(a, b []rgba) float64 {
	var se float64
	for i := range a {
		for _, d := range []int{int(a[i].R) - int(b[i].R), int(a[i].G) - int(b[i].G), int(a[i].B) - int(b[i].B), int(a[i].A) - int(b[i].A)} {
			se += float64(d * d)
		}
	}
	mse := se / float64(len(a)*4)
	if mse == 0 {
		return 99
	}
	return 10 * math.Log10(255*255/mse)
}

// La texture d'exemple : 91x256 dans un conteneur 128x256, DXT3, 6 niveaux.
func TestSampleStructure(t *testing.T) {
	g := load(t, "banner_colormask.gtex")
	tx, warns, err := ParseGTEX(g)
	if err != nil || len(warns) > 0 {
		t.Fatal(err, warns)
	}
	if tx.Type != 3 || tx.Hdr[HOrigW] != 91 || tx.Hdr[HFinalW] != 128 || tx.Hdr[HFinalH] != 256 || len(tx.Trailer) != 0 {
		t.Fatalf("en-tête inattendu : %+v", tx.Hdr)
	}
	want := [][2]int{{128, 256}, {64, 128}, {32, 64}, {16, 32}, {8, 16}, {4, 8}}
	if len(tx.Levels) != len(want) {
		t.Fatalf("%d niveaux", len(tx.Levels))
	}
	for i, l := range tx.Levels {
		if l.W != want[i][0] || l.H != want[i][1] || len(l.Data) != levelSize(3, l.W, l.H) {
			t.Fatalf("niveau %d : %dx%d, %d octets", i, l.W, l.H, len(l.Data))
		}
	}
}

// GTEX -> DDS -> GTEX doit redonner exactement le fichier d'origine.
func TestRoundTripExact(t *testing.T) {
	g := load(t, "banner_colormask.gtex")
	r1, err := GTEXToDDS(g, ToDDSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDDS(r1.Data)
	if err != nil || len(d.Levels) != 6 || d.W != 128 || d.H != 256 {
		t.Fatalf("DDS produit invalide : %v %+v", err, d)
	}
	r2, err := DDSToGTEX(r1.Data, ToGTEXOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r2.Data, g) {
		t.Fatal("l'aller-retour GTEX -> DDS -> GTEX n'est pas identique à l'original")
	}
}

// Le DDS de l'ancien ddswrapper : 91 est perdu, tout le reste est récupérable.
func TestLegacyDDS(t *testing.T) {
	g := load(t, "banner_colormask.gtex")
	old := load(t, "banner_colormask_old.dds")

	// avec le .gtex d'origine à côté : identique
	r, err := DDSToGTEX(old, ToGTEXOptions{Existing: g})
	if err != nil || !bytes.Equal(r.Data, g) {
		t.Fatalf("avec .gtex existant : %v", err)
	}
	// avec -X91 : identique aussi (la hauteur d'origine vaut la hauteur finale)
	r, err = DDSToGTEX(old, ToGTEXOptions{OrigW: 91})
	if err != nil || !bytes.Equal(r.Data, g) {
		t.Fatalf("avec -X91 : %v", err)
	}
	// sans rien : seule la largeur d'origine (octets 0x0E..0x11) diffère
	r, err = DDSToGTEX(old, ToGTEXOptions{})
	if err != nil || len(r.Data) != len(g) {
		t.Fatal(err)
	}
	for i := range g {
		if g[i] != r.Data[i] && (i < 0x0E || i > 0x11) {
			t.Fatalf("différence inattendue à l'octet 0x%X", i)
		}
	}
	if !strings.Contains(strings.Join(r.Info, "|"), "inconnues") {
		t.Fatal("l'information sur les dimensions d'origine inconnues manque")
	}
}

// DDS standard à un seul niveau et sans métadonnées, comme l'exporte un éditeur.
func editorDDS(tx *Texture, levels int, extra []Level) []byte {
	cp := *tx
	cp.Levels = append(append([]Level{}, tx.Levels[:levels]...), extra...)
	dds := BuildDDS(&cp, false)
	le.PutUint32(dds[124:], 0)
	le.PutUint32(dds[116:], 0)
	for i := 32; i < 76; i++ {
		dds[i] = 0
	}
	return dds
}

func levelCount(t *testing.T, b []byte) int {
	t.Helper()
	g, _, err := ParseGTEX(b)
	if err != nil {
		t.Fatal(err)
	}
	if int(g.Hdr[HLevels]) != len(g.Levels) {
		t.Fatalf("g0=%d mais %d niveaux", g.Hdr[HLevels], len(g.Levels))
	}
	return len(g.Levels)
}

// Gestion du nombre de niveaux (g0) pour un DDS d'éditeur.
func TestMipLevels(t *testing.T) {
	g := load(t, "banner_colormask.gtex")
	tx, _, _ := ParseGTEX(g)
	one := editorDDS(tx, 1, nil)

	// sans indication : on ne fabrique rien, 1 niveau reste 1 niveau
	r, err := DDSToGTEX(one, ToGTEXOptions{OrigW: 91})
	if err != nil || levelCount(t, r.Data) != 1 {
		t.Fatalf("sans indication : %v", err)
	}
	// avec le .gtex d'origine à côté (6 niveaux) : les 5 mipmaps sont régénérés
	r, err = DDSToGTEX(one, ToGTEXOptions{Existing: g})
	if err != nil || levelCount(t, r.Data) != 6 {
		t.Fatalf("avec .gtex existant : %v", err)
	}
	gen, _, _ := ParseGTEX(r.Data)
	if !bytes.Equal(gen.Levels[0].Data, tx.Levels[0].Data) || gen.Hdr[HOrigW] != 91 {
		t.Fatal("niveau de base ou en-tête modifié")
	}
	// -G6 explicite
	r, err = DDSToGTEX(one, ToGTEXOptions{Mips: 6})
	if err != nil || levelCount(t, r.Data) != 6 {
		t.Fatalf("-G6 : %v", err)
	}
	// 1) l'encodeur est fidèle au filtre ; 2) les niveaux restent cohérents avec ceux du jeu
	// (le filtre exact du jeu est inconnu : 27 dB au niveau 1 même sans aucune compression).
	gen, _, _ = ParseGTEX(r.Data)
	ref := decodeLevel(3, 128, 256, tx.Levels[0].Data)
	w, h := 128, 256
	for i := 1; i < 6; i++ {
		ref, w, h = downsample(ref, w, h)
		got := decodeLevel(3, gen.Levels[i].W, gen.Levels[i].H, gen.Levels[i].Data)
		orig := decodeLevel(3, tx.Levels[i].W, tx.Levels[i].H, tx.Levels[i].Data)
		pe, pg := psnr(ref, got), psnr(orig, got)
		t.Logf("niveau %d (%dx%d) : encodeur %.1f dB, comparé au niveau du jeu %.1f dB", i, w, h, pe, pg)
		// les niveaux minuscules (1 à 8 blocs) sont intrinsèquement difficiles à compresser en DXT
		min := 27.0
		if i >= 4 {
			min = 18
		}
		if pe < min {
			t.Errorf("niveau %d : encodage infidèle (%.1f dB)", i, pe)
		}
		if pg < 14 {
			t.Errorf("niveau %d : incohérent avec l'original (%.1f dB)", i, pg)
		}
	}
	// chaîne complète d'un éditeur (9 niveaux, jusqu'à 1x1) sans indication : limitée à 6
	full := editorDDS(tx, 1, GenerateMips(3, tx.Levels[0], 8))
	r, err = DDSToGTEX(full, ToGTEXOptions{})
	if err != nil || levelCount(t, r.Data) != 6 {
		t.Fatalf("chaîne complète : %v", err)
	}
	// -G1 retire les mipmaps
	r, err = DDSToGTEX(full, ToGTEXOptions{Mips: 1})
	if err != nil || levelCount(t, r.Data) != 1 {
		t.Fatalf("-G1 : %v", err)
	}
}

// Tous les exemples réels du jeu : aller-retour identique à l'octet près.
func TestAllSamplesRoundTrip(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.gtex")
	if len(files) < 10 {
		t.Skipf("exemples de textures absents (%d trouvés) : voir testdata/README.md", len(files))
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		r1, err := GTEXToDDS(b, ToDDSOptions{})
		if err != nil {
			t.Errorf("%s : %v", f, err)
			continue
		}
		if _, err := ParseDDS(r1.Data); err != nil {
			t.Errorf("%s : DDS produit illisible : %v", f, err)
		}
		r2, err := DDSToGTEX(r1.Data, ToGTEXOptions{})
		if err != nil || !bytes.Equal(r2.Data, b) {
			t.Errorf("%s : aller-retour différent (%v)", f, err)
		}
		// le même fichier passé par un éditeur (en-tête effacé) avec le .gtex d'origine à côté
		tx, _, _ := ParseGTEX(b)
		r3, err := DDSToGTEX(editorDDS(tx, len(tx.Levels), nil), ToGTEXOptions{Existing: b})
		if err != nil || !bytes.Equal(r3.Data, b) {
			t.Errorf("%s : reconstruction via le .gtex existant différente (%v)", f, err)
		}
	}
}

// DDS de l'ancien ddswrapper pour une texture sans mipmaps (dwFlags = g0 = 1).
func TestLegacyNoMips(t *testing.T) {
	g := load(t, "amber_pendant_100x100.gtex")
	tx, _, _ := ParseGTEX(g)
	old := editorDDS(tx, 1, nil)
	// l'ancien outil écrit g0 dans dwFlags, g3/g4 dans depth/mips, g5..g7 dans les réservés
	for i := 32; i < 76; i++ {
		old[i] = 0
	}
	le.PutUint32(old[8:], tx.Hdr[HLevels])
	le.PutUint32(old[24:], tx.Hdr[HDepth])
	le.PutUint32(old[28:], tx.Hdr[HMips])
	le.PutUint32(old[32:], tx.Hdr[HFmt])
	le.PutUint32(old[36:], tx.Hdr[HRType])
	le.PutUint32(old[40:], tx.Hdr[HIFF])
	d, err := ParseDDS(old)
	if err != nil || !d.Legacy {
		t.Fatalf("ancien DDS non reconnu : %v", err)
	}
	r, err := DDSToGTEX(old, ToGTEXOptions{OrigW: 100, OrigH: 100})
	if err != nil || !bytes.Equal(r.Data, g) {
		t.Fatalf("ancien DDS DXT1 : %v", err)
	}
}

// Encodage/décodage des trois formats DXT.
func TestDXTRoundTrip(t *testing.T) {
	const w, h = 64, 32
	px := make([]rgba, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px[y*w+x] = rgba{uint8(x * 4), uint8(y * 8), uint8((x + y) * 2), uint8(255 - x*3)}
		}
	}
	for _, typ := range []int{0, 1, 3, 5} {
		ref := px
		if typ == 1 {
			ref = append([]rgba{}, px...)
			for i := range ref {
				ref[i].A = 255
			}
		}
		out := decodeLevel(typ, w, h, encodeLevel(typ, w, h, ref))
		p := psnr(ref, out)
		t.Logf("type %d : PSNR %.1f dB", typ, p)
		if p < 28 {
			t.Errorf("type %d : PSNR trop bas (%.1f dB)", typ, p)
		}
	}
}

// Texture non compressée : aller-retour, et DDS 24 bits / masques différents.
func TestUncompressed(t *testing.T) {
	const w, h = 16, 8
	data := make([]byte, w*h*4)
	for i := range data {
		data[i] = byte(i * 7)
	}
	var hdr = defaultHeader(0, w, h, len(data))
	hdr[HOrigW] = 13
	g := buildGTEX([4]byte{'A', 'R', 'G', 'B'}, hdr, []Level{{w, h, data}}, nil)
	r1, err := GTEXToDDS(g, ToDDSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := DDSToGTEX(r1.Data, ToGTEXOptions{})
	if err != nil || !bytes.Equal(r2.Data, g) {
		t.Fatalf("aller-retour non compressé : %v", err)
	}
	// DDS 24 bits (B,G,R) écrit à la main
	dds := make([]byte, ddsHeaderLen)
	copy(dds, "DDS ")
	le.PutUint32(dds[4:], 124)
	le.PutUint32(dds[8:], 0x100F)
	le.PutUint32(dds[12:], 2)
	le.PutUint32(dds[16:], 2)
	le.PutUint32(dds[76:], 32)
	le.PutUint32(dds[80:], 0x40)
	le.PutUint32(dds[88:], 24)
	le.PutUint32(dds[92:], 0xFF0000)
	le.PutUint32(dds[96:], 0xFF00)
	le.PutUint32(dds[100:], 0xFF)
	le.PutUint32(dds[108:], 0x1000)
	dds = append(dds, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12) // 2x2 pixels, pitch 6
	d, err := ParseDDS(dds)
	if err != nil || d.Type != 0 || !bytes.Equal(d.Levels[0].Data, []byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255, 10, 11, 12, 255}) {
		t.Fatalf("conversion 24 bits : %v %v", err, d)
	}
}

func TestErrors(t *testing.T) {
	g := load(t, "banner_colormask.gtex")
	if _, err := GTEXToDDS(g[:40], ToDDSOptions{}); err == nil {
		t.Error("GTEX tronqué accepté")
	}
	if _, err := GTEXToDDS(g[:20000], ToDDSOptions{}); err == nil {
		t.Error("GTEX aux données tronquées accepté")
	}
	if _, err := GTEXToDDS([]byte("DDS "+strings.Repeat("x", 200)), ToDDSOptions{}); err != ErrNotGTEX {
		t.Error("DDS pris pour un GTEX")
	}
	if _, err := DDSToGTEX(g, ToGTEXOptions{}); err != ErrNotDDS {
		t.Error("GTEX pris pour un DDS")
	}
	r1, _ := GTEXToDDS(g, ToDDSOptions{})
	if _, err := DDSToGTEX(r1.Data[:5000], ToGTEXOptions{}); err == nil {
		t.Error("DDS aux données tronquées accepté")
	}
	bad := append([]byte{}, g...)
	copy(bad[6:], "DXT9")
	if _, err := GTEXToDDS(bad, ToDDSOptions{}); err == nil {
		t.Error("type DXT9 accepté")
	}
}

// Des octets inconnus après les mipmaps traversent l'aller-retour.
func TestTrailerPreserved(t *testing.T) {
	g := append(load(t, "banner_colormask.gtex"), 1, 2, 3, 4, 5)
	r1, err := GTEXToDDS(g, ToDDSOptions{})
	if err != nil || len(r1.Warn) == 0 {
		t.Fatal(err, r1)
	}
	r2, err := DDSToGTEX(r1.Data, ToGTEXOptions{})
	if err != nil || !bytes.Equal(r2.Data, g) {
		t.Fatalf("octets finaux perdus : %v", err)
	}
}
