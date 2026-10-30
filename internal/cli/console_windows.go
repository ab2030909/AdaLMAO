//go:build windows

package cli

import "syscall"

// Switch the console to UTF-8 so emoji/box characters render instead of mojibake.
func init() {
	k := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = k.NewProc("SetConsoleOutputCP").Call(65001)
}
