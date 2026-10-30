package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var secretRe = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password|passwd|authorization|bearer)\S{0,3}[=:\s]+)\S+|(sk-[A-Za-z0-9_-]{12,})|(gh[pousr]_[A-Za-z0-9]{20,})|(AKIA[0-9A-Z]{16})`)

// Redact masks likely secrets and truncates long strings for logging.
func Redact(s string) string {
	s = secretRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := secretRe.FindStringSubmatch(m)
		if sub[1] != "" {
			return sub[1] + "***"
		}
		return "***"
	})
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// LogEvent appends a JSON line to ~/.adalmao/decisions.log. Errors are ignored (hooks must stay quiet).
func LogEvent(fields map[string]any) {
	fields["ts"] = time.Now().Format(time.RFC3339)
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	_ = os.MkdirAll(Dir(), 0o755)
	f, err := os.OpenFile(filepath.Join(Dir(), "decisions.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
