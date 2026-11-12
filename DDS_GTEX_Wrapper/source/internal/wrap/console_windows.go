//go:build windows

package wrap

import (
	"syscall"
	"unsafe"
)

// ownConsole : vrai si la fenêtre de console n'appartient qu'à ce processus,
// c'est-à-dire quand l'exécutable a été lancé par glisser-déposer ou double-clic.
func ownConsole() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	var pids [4]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids[0])), 4)
	return n == 1
}
