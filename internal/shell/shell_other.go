//go:build !windows

package shell

import "os/exec"

// Command mimics how AdaL launches hooks on Unix: /bin/sh -c <command> (documented).
func Command(command string) *exec.Cmd { return exec.Command("/bin/sh", "-c", command) }
