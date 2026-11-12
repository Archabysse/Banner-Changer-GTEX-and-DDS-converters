// ddswrapper2 : convertit des fichiers .gtex en .dds (Warhammer: Mark of Chaos), en série et sans perte.
package main

import (
	"os"

	"gtexwrap/internal/wrap"
)

func main() { os.Exit(wrap.Main(wrap.ToolDDSWrapper2, os.Args[1:])) }
