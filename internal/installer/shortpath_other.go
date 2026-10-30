//go:build !windows

package installer

func shortPath(p string) string { return p }
