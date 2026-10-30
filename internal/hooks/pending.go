package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/adalmao/adalmao/internal/config"
)

var unsafeID = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func pendingPath(session string) string {
	s := unsafeID.ReplaceAllString(session, "")
	if s == "" {
		s = "default"
	}
	return filepath.Join(config.Dir(), "state", "pending-"+s+".json")
}

// queueNotes stores non-blocking warnings until the next PostToolUse for the session.
func queueNotes(session string, notes []string) {
	if len(notes) == 0 {
		return
	}
	p := pendingPath(session)
	var cur []string
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	cur = append(cur, notes...)
	if len(cur) > 10 {
		cur = cur[len(cur)-10:]
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, b, 0o644)
	}
}

// takeNotes returns and clears queued warnings.
func takeNotes(session string) []string {
	p := pendingPath(session)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	_ = os.Remove(p)
	var cur []string
	_ = json.Unmarshal(b, &cur)
	return cur
}
