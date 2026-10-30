package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/adalmao/adalmao/internal/config"
)

// captureIfRequested records raw hook payloads when ADALMAO_CAPTURE=1. It exists for integration
// verification (`adalmao verify`) and debugging. Payloads may contain code and prompts, so it is
// strictly opt-in and writes only under the AdaLmao data directory.
func captureIfRequested(event string, stdin []byte) {
	if os.Getenv("ADALMAO_CAPTURE") != "1" {
		return
	}
	rec := map[string]any{
		"ts":      time.Now().Format(time.RFC3339Nano),
		"arg":     event,
		"payload": json.RawMessage(validOrQuoted(stdin)),
		"pid":     os.Getpid(),
		"ppid":    os.Getppid(),
		"comspec": os.Getenv("ComSpec"),
		"shell":   os.Getenv("SHELL"),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = os.MkdirAll(config.Dir(), 0o755)
	f, err := os.OpenFile(filepath.Join(config.Dir(), "capture.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func validOrQuoted(b []byte) []byte {
	if json.Valid(b) {
		return b
	}
	q, _ := json.Marshal(string(b))
	return q
}
