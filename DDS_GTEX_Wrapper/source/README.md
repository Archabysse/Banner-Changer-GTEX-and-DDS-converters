# DDS ⇄ GTEX Wrapper

Outils en ligne de commande / glisser-déposer pour convertir les textures **GTEX** de
*Warhammer: Mark of Chaos* en **DDS** et inversement, **sans perte**.

| Programme | Rôle |
|---|---|
| `gtexwrapper` | `.dds` → `.gtex` (l'inverse de `ddswrapper` de Zoinkity) |
| `ddswrapper2` | `.gtex` → `.dds` (remplace `ddswrapper`; copie aussi les `.tga` / `.bmp`) ; plusieurs fichiers à la fois |

Utilisation : glisser-déposer un ou plusieurs fichiers sur l'exécutable, ou
`gtexwrapper fichier1.dds fichier2.dds` (`-?` pour l'aide). Voir [`LISEZMOI.txt`](LISEZMOI.txt)
pour le détail des options et des limites.

## Ce que font ces programmes (et rien d'autre)

Lecture de fichiers donnés en argument, écriture du fichier converti **dans le même
dossier** (ou celui donné par `-O`). Aucun accès réseau, aucune installation, aucune
modification du système, aucune dépendance externe : bibliothèque standard Go uniquement.
La seule API Windows appelée est `GetConsoleProcessList`
([`console_windows.go`](internal/wrap/console_windows.go)) pour savoir si la fenêtre doit
rester ouverte en cas d'erreur.

## Format GTEX (déduit de 11 fichiers du jeu)

Décrit en tête de [`internal/wrap/format.go`](internal/wrap/format.go) : en-tête de 54 octets
(dont le nombre de niveaux, la taille d'origine du contenu et celle du conteneur), niveau de
base brut, puis chaque mipmap précédé de `largeur, hauteur, taille`. Le DDS produit est un
DDS standard ; l'en-tête GTEX complet est conservé dans ses champs réservés pour que
GTEX → DDS → GTEX redonne un fichier identique à l'octet près.

## Compiler

Go 1.22+ (aucun autre outil) :

```sh
go test ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o gtexwrapper.exe ./cmd/gtexwrapper
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ddswrapper2.exe ./cmd/ddswrapper2
```
(`build.bat` / `build.sh` font la même chose.) Avec `-trimpath` et la même version de Go la
compilation est reproductible : chacun peut recompiler et comparer l'empreinte (SHA-256) avec
l'exécutable publié.

## Licence

Aucune licence n'est encore choisie : tant que le propriétaire du dépôt n'en ajoute pas
(ex. MIT), tous droits réservés par défaut.
