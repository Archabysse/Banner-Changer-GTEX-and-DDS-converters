package wrap

import "fmt"

// Result : sortie d'une conversion.
type Result struct {
	Data    []byte
	Summary string   // ex. « 128x256, DXT3, 6 niveaux »
	Info    []string // remarques
	Warn    []string // avertissements
}

func summary(typ, w, h, levels int) string {
	s := "niveau"
	if levels > 1 {
		s = "niveaux"
	}
	return fmt.Sprintf("%dx%d, %s, %d %s", w, h, TypeName(typ), levels, s)
}

// ToDDSOptions : options de GTEXToDDS.
type ToDDSOptions struct {
	Swap bool // échange largeur/hauteur dans le DDS (option -S)
}

// GTEXToDDS convertit un GTEX en DDS standard, sans perte.
func GTEXToDDS(in []byte, o ToDDSOptions) (*Result, error) {
	t, warns, err := ParseGTEX(in)
	if err != nil {
		return nil, err
	}
	r := &Result{Data: BuildDDS(t, o.Swap), Warn: warns}
	l0 := t.Levels[0]
	r.Summary = summary(t.Type, l0.W, l0.H, len(t.Levels))
	if t.Type == 0 && t.Tag != defaultTag(0) {
		r.Info = append(r.Info, fmt.Sprintf("étiquette GTEX « %s » conservée dans le DDS", tagString(t.Tag)))
	}
	if t.Hdr[HOrigW] != t.Hdr[HFinalW] || t.Hdr[HOrigH] != t.Hdr[HFinalH] {
		r.Info = append(r.Info, fmt.Sprintf("dimensions d'origine %dx%d conservées dans le DDS (contenu dans un conteneur %dx%d)",
			t.Hdr[HOrigW], t.Hdr[HOrigH], t.Hdr[HFinalW], t.Hdr[HFinalH]))
	}
	return r, nil
}

// ToGTEXOptions : options de DDSToGTEX.
type ToGTEXOptions struct {
	Swap         bool   // échange largeur/hauteur lues dans le DDS (option -S)
	OrigW, OrigH int    // dimensions d'origine imposées (0 = automatique)
	Mips         int    // nombre de niveaux voulu (0 = automatique, voir ci-dessous)
	Existing     []byte // contenu du .gtex déjà présent à l'emplacement de sortie
}

// DDSToGTEX convertit un DDS en GTEX. L'en-tête GTEX est reconstruit, par ordre
// de priorité, à partir : des métadonnées laissées par ddswrapper2, du .gtex
// existant, des champs de l'ancien ddswrapper, puis de valeurs par défaut.
func DDSToGTEX(in []byte, o ToGTEXOptions) (*Result, error) {
	d, err := ParseDDS(in)
	if err != nil {
		return nil, err
	}
	r := &Result{Info: d.Info, Warn: d.Warn}
	gw, gh := d.W, d.H // valeurs écrites en g8/g9
	if o.Swap {
		gw, gh = d.H, d.W
	}
	size0 := levelSize(d.Type, d.W, d.H)

	var tpl *Texture
	if len(o.Existing) > 0 {
		if t, _, err := ParseGTEX(o.Existing); err == nil && t.Type == d.Type &&
			int(t.Hdr[HFinalW]) == gw && int(t.Hdr[HFinalH]) == gh {
			tpl = t
		}
	}
	metaType := -1
	if d.Meta != nil {
		if mt, err := tagType(d.Meta.Tag); err == nil {
			metaType = mt
		}
	}
	metaFresh := d.Meta != nil && metaType == d.Type &&
		int(d.Meta.Hdr[HFinalW]) == gw && int(d.Meta.Hdr[HFinalH]) == gh

	hdr := defaultHeader(d.Type, gw, gh, size0)
	tag := defaultTag(d.Type)
	origKnown := false
	srcLevels := -1 // nombre de niveaux de l'en-tête repris tel quel (métadonnées ou .gtex existant)
	switch {
	case metaFresh:
		hdr, tag, origKnown, srcLevels = d.Meta.Hdr, d.Meta.Tag, true, len(d.Levels)
		r.Info = append(r.Info, "en-tête GTEX restauré à partir des métadonnées du DDS")
	case tpl != nil:
		hdr, tag, origKnown, srcLevels = tpl.Hdr, tpl.Tag, true, len(tpl.Levels)
		r.Info = append(r.Info, "en-tête GTEX repris du fichier .gtex existant")
	default:
		if d.Legacy {
			for _, i := range []int{HDepth, HMips, HFmt, HRType, HIFF} {
				hdr[i] = d.LegacyHdr[i]
			}
		} else if d.Meta != nil && metaType == d.Type {
			for _, i := range []int{HDepth, HMips, HFmt, HRType, HIFF} {
				hdr[i] = d.Meta.Hdr[i]
			}
			tag = d.Meta.Tag
			r.Info = append(r.Info, "dimensions modifiées depuis la conversion : dimensions d'origine = dimensions finales")
			origKnown = true
		}
	}
	if o.OrigW > 0 {
		hdr[HOrigW], origKnown = uint32(o.OrigW), true
	}
	if o.OrigH > 0 {
		hdr[HOrigH], origKnown = uint32(o.OrigH), true
	}
	if !origKnown {
		r.Info = append(r.Info, "dimensions d'origine inconnues, prises égales aux dimensions finales (options -X et -Y, ou .gtex d'origine à côté du .dds)")
	}
	hdr[HFinalW], hdr[HFinalH] = uint32(gw), uint32(gh)

	// Nombre de niveaux : celui du DDS, sauf si le .gtex existant ou l'option -G
	// en imposent un autre. Sans aucune indication, au plus 6 (maximum observé
	// dans les fichiers du jeu).
	levels := d.Levels
	target := len(levels)
	switch {
	case o.Mips > 0:
		target = o.Mips
	case tpl != nil:
		target = len(tpl.Levels)
	case srcLevels < 0 && !d.Legacy && target > 6:
		target = 6
		r.Info = append(r.Info, fmt.Sprintf("%d niveaux dans le DDS : limités à 6, comme dans les fichiers du jeu (option -G pour changer)", len(levels)))
	}
	if target < len(levels) {
		if o.Mips > 0 || tpl != nil {
			r.Info = append(r.Info, fmt.Sprintf("%d niveau(x) de mipmap retiré(s) pour obtenir %d niveau(x)", len(levels)-target, target))
		}
		levels = levels[:target]
	} else if target > len(levels) {
		// les niveaux manquants sont recalculés depuis le niveau de base
		all := GenerateMips(d.Type, levels[0], target-1)
		var gen []Level
		if len(all) >= len(levels) {
			gen = all[len(levels)-1:]
		}
		levels = append(append([]Level{}, levels...), gen...)
		r.Info = append(r.Info, fmt.Sprintf("mipmaps absents du DDS : %d niveau(x) généré(s)", len(gen)))
		if len(levels) < target {
			r.Warn = append(r.Warn, fmt.Sprintf("%d niveaux demandés mais l'image n'en permet que %d", target, len(levels)))
		}
	}
	if srcLevels != len(levels) || srcLevels < 0 || int(hdr[HLevels]) != len(levels) {
		hdr[HLevels] = uint32(len(levels))
	}
	r.Data = buildGTEX(tag, hdr, levels, d.Trailer)
	r.Summary = summary(d.Type, gw, gh, len(levels))
	return r, nil
}
