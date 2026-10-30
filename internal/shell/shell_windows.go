//go:build windows

package shell

import (
	"os/exec"
	"syscall"
)

// Command mimics how AdaL launches hooks on Windows: cmd.exe /c <command> (documented).
func Command(command string) *exec.Cmd {
	c := exec.Command("cmd.exe")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /c " + command}
	return c
}
