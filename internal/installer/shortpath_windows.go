//go:build windows

package installer

import (
	"syscall"
	"unsafe"
)

// shortPath returns the 8.3 short form of p (no spaces) or p unchanged when unavailable.
func shortPath(p string) string {
	src, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")
	buf := make([]uint16, 520)
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) >= len(buf) {
		return p
	}
	return syscall.UTF16ToString(buf[:n])
}
