// gtexwrapper : convertit des fichiers .dds en .gtex (Warhammer: Mark of Chaos).
package main

import (
	"os"

	"gtexwrap/internal/wrap"
)

func main() { os.Exit(wrap.Main(wrap.ToolGTEXWrapper, os.Args[1:])) }
