package cli

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func runtimeOS() string { return runtime.GOOS }

// capture runs a read-only command with a short timeout and returns the first line of output.
func capture(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
