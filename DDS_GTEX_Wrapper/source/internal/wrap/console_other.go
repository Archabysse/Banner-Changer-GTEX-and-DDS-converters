//go:build !windows

package wrap

func ownConsole() bool { return false }
