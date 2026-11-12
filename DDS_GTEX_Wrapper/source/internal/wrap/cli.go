package wrap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tool désigne l'application construite à partir de ce paquet.
type Tool int

const (
	ToolGTEXWrapper Tool = iota // .dds -> .gtex
	ToolDDSWrapper2             // .gtex -> .dds (ainsi que .tga/.bmp comme ddswrapper)
)

const version = "1.0"

type options struct {
	quiet, swap, noBackup, pause bool
	out                          string
	origW, origH, mips           int
}

func (t Tool) name() string {
	if t == ToolGTEXWrapper {
		return "gtexwrapper"
	}
	return "ddswrapper2"
}

func help(t Tool, w io.Writer) {
	if t == ToolGTEXWrapper {
		fmt.Fprintf(w, "\nGTEX Wrapper %s\tConvertit des fichiers .dds en fichiers GTEX de Warhammer: Mark of Chaos.\n", version)
		fmt.Fprintf(w, "Fait l'inverse de ddswrapper : l'en-tête et les mipmaps du GTEX d'origine sont conservés.\n")
		fmt.Fprintf(w, "\nusage : %s <fichier.dds> [<fichier2.dds> ...] [options]\n", t.name())
		fmt.Fprintf(w, "        ou glissez-déposez un ou plusieurs fichiers .dds sur %s.exe\n", t.name())
	} else {
		fmt.Fprintf(w, "\nDDS Wrapper 2 %s\tEnveloppe des fichiers GTEX de Warhammer: Mark of Chaos dans un .dds.\n", version)
		fmt.Fprintf(w, "Comme ddswrapper (les .tga et .bmp sont simplement copiés), mais sans perte et en série.\n")
		fmt.Fprintf(w, "\nusage : %s <fichier.gtex> [<fichier2.gtex> ...] [options]\n", t.name())
		fmt.Fprintf(w, "        ou glissez-déposez un ou plusieurs fichiers sur %s.exe\n", t.name())
	}
	fmt.Fprintf(w, "les options peuvent commencer par '-' ou '/', en majuscules ou minuscules\n")
	fmt.Fprintf(w, "\tO<chemin>\tfichier de sortie (un seul fichier) ou dossier de sortie (plusieurs)\n")
	fmt.Fprintf(w, "\tS\téchange largeur et hauteur dans l'en-tête de sortie\n")
	if t == ToolGTEXWrapper {
		fmt.Fprintf(w, "\tX<n>\tlargeur d'origine du contenu (ex. -X91), si elle n'est pas connue autrement\n")
		fmt.Fprintf(w, "\tY<n>\thauteur d'origine du contenu\n")
		fmt.Fprintf(w, "\tG<n>\timpose n niveaux de mipmaps (ex. -G6, ou -G1 pour aucun) ; les manquants sont générés\n")
	}
	fmt.Fprintf(w, "\tB\tne pas sauvegarder (.bak) un fichier de sortie déjà existant\n")
	fmt.Fprintf(w, "\tP\tattendre une touche à la fin, même sans erreur\n")
	fmt.Fprintf(w, "\tQ\tmode silencieux : seules les erreurs sont affichées\n")
	fmt.Fprintf(w, "\tH ou ?\tce message\n")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Main exécute l'outil et renvoie le code de sortie.
func Main(tool Tool, args []string) int {
	var o options
	var files []string
	for _, a := range args {
		if len(a) >= 2 && (a[0] == '-' || a[0] == '/') && !exists(a) {
			if a == "--help" {
				help(tool, os.Stdout)
				return pauseAndExit(0, true)
			}
			rest := a[2:]
			switch c := a[1] | 0x20; c { // minuscule
			case 'q':
				o.quiet = true
			case 's':
				o.swap = true
			case 'b':
				o.noBackup = true
			case 'p':
				o.pause = true
			case 'o':
				if rest == "" {
					fmt.Fprintln(os.Stderr, "L'option -O doit être suivie d'un chemin (ex. -OC:\\sortie).")
					return pauseAndExit(2, true)
				}
				o.out = rest
			case 'x', 'y', 'g':
				n, err := strconv.Atoi(rest)
				if err != nil || n <= 0 || n > maxDim || (c == 'g' && n > 16) {
					fmt.Fprintf(os.Stderr, "Valeur invalide pour l'option %s : « %s ».\n", a[:2], rest)
					return pauseAndExit(2, true)
				}
				switch c {
				case 'x':
					o.origW = n
				case 'y':
					o.origH = n
				default:
					o.mips = n
				}
			case 'h', '?':
				help(tool, os.Stdout)
				return pauseAndExit(0, true)
			default:
				fmt.Fprintf(os.Stderr, "Option inconnue : %s (taper -? pour l'aide).\n", a)
				return pauseAndExit(2, true)
			}
			continue
		}
		files = append(files, a)
	}
	if len(files) == 0 {
		help(tool, os.Stdout)
		return pauseAndExit(1, true)
	}

	r := &runner{tool: tool, o: o, multi: len(files) > 1, produced: map[string]string{}}
	for _, f := range files {
		r.process(f)
	}
	if !o.quiet || r.errors > 0 {
		fmt.Printf("\n%d fichier(s) converti(s)", r.ok)
		if r.skipped > 0 {
			fmt.Printf(", %d inchangé(s)", r.skipped)
		}
		if r.errors > 0 {
			fmt.Printf(", %d ERREUR(S)", r.errors)
		}
		fmt.Println(".")
	}
	code := 0
	if r.errors > 0 {
		code = 1
	}
	return pauseAndExit(code, o.pause || r.errors > 0 || r.warnings > 0)
}

// pauseAndExit attend une touche si le programme tourne dans sa propre fenêtre
// de console (lancé par glisser-déposer) et que l'utilisateur doit lire quelque chose.
func pauseAndExit(code int, wanted bool) int {
	if wanted && ownConsole() {
		fmt.Print("\nAppuyez sur Entrée pour fermer...")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return code
}

type runner struct {
	tool                          Tool
	o                             options
	multi                         bool
	produced                      map[string]string
	ok, skipped, errors, warnings int
}

func (r *runner) say(format string, a ...any) {
	if !r.o.quiet {
		fmt.Printf(format, a...)
	}
}

func (r *runner) fail(in string, err error) {
	r.errors++
	fmt.Fprintf(os.Stderr, "ERREUR  %s\n        %v\n", filepath.Base(in), err)
}

// isTGA / isBMP reprennent la détection du ddswrapper d'origine.
func isBMP(b []byte) bool { return len(b) >= 2 && b[0] == 'B' && b[1] == 'M' }

func isTGA(b []byte) bool {
	if len(b) < 18 {
		return false
	}
	idLen := int(b[0])
	if idLen > 32 {
		return false
	}
	if idLen > 0 {
		return len(b) >= 18+idLen && idLen >= 13 && string(b[18:31]) == "Saved by D3DX"
	}
	return len(b) >= 18 && string(b[len(b)-18:len(b)-3]) == "TRUEVISION-XFILE"
}

func (r *runner) outPath(in, ext string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	dir := filepath.Dir(in)
	if r.o.out != "" {
		st, err := os.Stat(r.o.out)
		isDir := (err == nil && st.IsDir()) || strings.HasSuffix(r.o.out, "/") || strings.HasSuffix(r.o.out, "\\")
		if r.multi && !isDir {
			isDir = true
		}
		if !isDir {
			return r.o.out, nil
		}
		if err := os.MkdirAll(r.o.out, 0o755); err != nil {
			return "", fmt.Errorf("impossible de créer le dossier de sortie : %v", err)
		}
		dir = r.o.out
	}
	return filepath.Join(dir, base+ext), nil
}

func (r *runner) process(in string) {
	data, err := os.ReadFile(in)
	if err != nil {
		r.fail(in, err)
		return
	}
	var ext string
	var res *Result
	var cvt func(existing []byte) (*Result, error)

	if r.tool == ToolGTEXWrapper {
		ext = ".gtex"
		switch {
		case len(data) >= 6 && string(data[:6]) == "GTEX00":
			r.fail(in, fmt.Errorf("c'est déjà un fichier GTEX (pour l'inverse, utilisez ddswrapper2)"))
			return
		}
		cvt = func(existing []byte) (*Result, error) {
			return DDSToGTEX(data, ToGTEXOptions{Swap: r.o.swap, OrigW: r.o.origW, OrigH: r.o.origH, Mips: r.o.mips, Existing: existing})
		}
	} else {
		switch {
		case len(data) >= 6 && string(data[:6]) == "GTEX00":
			ext = ".dds"
			cvt = func([]byte) (*Result, error) { return GTEXToDDS(data, ToDDSOptions{Swap: r.o.swap}) }
		case isBMP(data):
			ext = ".bmp"
			cvt = func([]byte) (*Result, error) { return &Result{Data: data, Summary: "image BMP copiée"}, nil }
		case isTGA(data):
			ext = ".tga"
			cvt = func([]byte) (*Result, error) { return &Result{Data: data, Summary: "image TGA copiée"}, nil }
		case len(data) >= 4 && string(data[:4]) == "DDS ":
			r.fail(in, fmt.Errorf("c'est déjà un fichier DDS (pour l'inverse, utilisez gtexwrapper)"))
			return
		default:
			r.fail(in, fmt.Errorf("format non reconnu (ni GTEX, ni TGA, ni BMP)"))
			return
		}
	}

	out, err := r.outPath(in, ext)
	if err != nil {
		r.fail(in, err)
		return
	}
	if sti, e1 := os.Stat(in); e1 == nil {
		if sto, e2 := os.Stat(out); e2 == nil && os.SameFile(sti, sto) {
			if ext == ".tga" || ext == ".bmp" {
				r.say("IGNORÉ  %s : déjà au format %s\n", filepath.Base(in), ext)
				r.skipped++
				return
			}
			r.fail(in, fmt.Errorf("le fichier de sortie serait le fichier d'entrée"))
			return
		}
	}
	key := strings.ToLower(filepath.Clean(out))
	if prev, dup := r.produced[key]; dup {
		r.fail(in, fmt.Errorf("même fichier de sortie que %s (%s)", prev, out))
		return
	}
	r.produced[key] = filepath.Base(in)

	existing, _ := os.ReadFile(out)
	res, err = cvt(existing)
	if err != nil {
		r.fail(in, err)
		return
	}
	status, note, err := writeOutput(out, res.Data, r.o.noBackup)
	if err != nil {
		r.fail(in, err)
		return
	}
	if status == "inchangé" {
		r.skipped++
	} else {
		r.ok++
	}
	r.say("OK      %s -> %s  (%s)", filepath.Base(in), filepath.Base(out), res.Summary)
	if status == "inchangé" {
		r.say("  [identique à l'existant, non réécrit]")
	}
	r.say("\n")
	if note != "" {
		r.say("        sauvegarde : %s\n", note)
	}
	for _, s := range res.Info {
		r.say("        (i) %s\n", s)
	}
	for _, s := range res.Warn {
		r.warnings++
		fmt.Fprintf(os.Stderr, "        /!\\ %s\n", s)
	}
}

// writeOutput écrit le fichier de sortie. Un fichier existant différent est
// d'abord renommé en .bak (ou .bak2, .bak3...), sauf si noBackup.
func writeOutput(path string, data []byte, noBackup bool) (status, backup string, err error) {
	if old, e := os.ReadFile(path); e == nil {
		if bytes.Equal(old, data) {
			return "inchangé", "", nil
		}
		if !noBackup {
			bak := path + ".bak"
			for n := 2; exists(bak); n++ {
				bak = fmt.Sprintf("%s.bak%d", path, n)
			}
			if err := os.Rename(path, bak); err != nil {
				return "", "", fmt.Errorf("sauvegarde impossible : %v", err)
			}
			backup = filepath.Base(bak)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	return "écrit", backup, nil
}
