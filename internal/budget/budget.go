// Package budget reads real token usage (when AdaL's transcript exposes it) and implements the guard warnings.
// Nothing here is invented: with no usage data in the transcript, usage is reported as unavailable.
package budget

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
)

type Usage struct {
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	Source       string    `json:"source"`
	Updated      time.Time `json:"updated"`
	Warned       string    `json:"warned"` // "", "threshold", "limit"
}

var unsafeRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func statePath(session string) string {
	s := unsafeRe.ReplaceAllString(session, "")
	if s == "" {
		s = "default"
	}
	return filepath.Join(config.Dir(), "state", "usage-"+s+".json")
}

// Load returns stored usage for a session (ok=false when nothing has been recorded).
func Load(session string) (Usage, bool) {
	var u Usage
	b, err := os.ReadFile(statePath(session))
	if err != nil || json.Unmarshal(b, &u) != nil {
		return u, false
	}
	return u, true
}

func save(session string, u Usage) {
	p := statePath(session)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.Marshal(u); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

// LatestSession returns the most recently updated stored session id.
func LatestSession() string {
	ents, _ := os.ReadDir(filepath.Join(config.Dir(), "state"))
	var best string
	var bt time.Time
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, "usage-") || !strings.HasSuffix(n, ".json") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(bt) {
			bt, best = info.ModTime(), strings.TrimSuffix(strings.TrimPrefix(n, "usage-"), ".json")
		}
	}
	return best
}

// Refresh scans the transcript for provider-reported usage and stores the totals.
// Returns false if the transcript has no recognizable usage fields.
func Refresh(session, transcript string) bool {
	if transcript == "" {
		return false
	}
	f, err := os.Open(transcript)
	if err != nil {
		return false
	}
	defer f.Close()
	var in, out int64
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, "tokens") {
			continue
		}
		var v any
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		walk(v, &in, &out, &found)
	}
	if !found {
		return false
	}
	u, _ := Load(session)
	u.InputTokens, u.OutputTokens, u.Source, u.Updated = in, out, "transcript", time.Now()
	save(session, u)
	return true
}

func num(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		if f, ok := m[k].(float64); ok {
			return int64(f), true
		}
	}
	return 0, false
}

func walk(v any, in, out *int64, found *bool) {
	switch t := v.(type) {
	case map[string]any:
		i, iok := num(t, "input_tokens", "prompt_tokens")
		o, ook := num(t, "output_tokens", "completion_tokens")
		if iok || ook {
			*in += i
			*out += o
			*found = true
			return // do not descend into a usage object
		}
		for _, c := range t {
			walk(c, in, out, found)
		}
	case []any:
		for _, c := range t {
			walk(c, in, out, found)
		}
	}
}

// Cost returns an ESTIMATE based on user-supplied prices (ok=false when no prices configured).
func Cost(cfg *config.Config, u Usage) (float64, bool) {
	b := cfg.Budget
	if b.InputPricePerMTok <= 0 && b.OutputPricePerMTok <= 0 {
		return 0, false
	}
	return float64(u.InputTokens)/1e6*b.InputPricePerMTok + float64(u.OutputTokens)/1e6*b.OutputPricePerMTok, true
}

// Guard returns a warning once per level when the estimated cost crosses the configured threshold.
// It can only warn; AdaL exposes no way for a hook to stop API spending.
func Guard(cfg *config.Config, session string) string {
	b := cfg.Budget
	if !b.Enabled || b.SessionLimit <= 0 {
		return ""
	}
	u, ok := Load(session)
	if !ok {
		return ""
	}
	cost, ok := Cost(cfg, u)
	if !ok {
		return ""
	}
	level := ""
	switch {
	case cost >= b.SessionLimit:
		level = "limit"
	case cost >= b.SessionLimit*b.WarningThreshold:
		level = "threshold"
	}
	if level == "" || u.Warned == level || (u.Warned == "limit") {
		return ""
	}
	u.Warned = level
	save(session, u)
	if level == "limit" {
		return fmt.Sprintf("[AdaLmao Guard] Estimated session cost $%.2f has reached the configured limit of $%.2f. Tell the user and ask before continuing. (AdaLmao can warn but cannot stop spending.)", cost, b.SessionLimit)
	}
	return fmt.Sprintf("[AdaLmao Guard] Estimated session cost $%.2f is at %.0f%% of the $%.2f limit. Mention this briefly to the user. (Estimate based on user-supplied prices.)", cost, cost/b.SessionLimit*100, b.SessionLimit)
}

// Report renders the budget report.
func Report(cfg *config.Config, session string) string {
	var s strings.Builder
	s.WriteString("😂 AdaLmao Budget Report\n\n")
	if session == "" {
		session = LatestSession()
	}
	u, ok := Load(session)
	if session == "" || !ok {
		s.WriteString("Usage data unavailable\n")
		s.WriteString("  AdaLmao reads token counts from the session transcript passed to hooks. No usage fields\n")
		s.WriteString("  have been recorded yet (or AdaL does not expose them). Nothing is invented.\n")
	} else {
		fmt.Fprintf(&s, "Session:        %s\n", session)
		fmt.Fprintf(&s, "Source:         provider-reported tokens found in %s (updated %s)\n", u.Source, u.Updated.Format("2006-01-02 15:04"))
		fmt.Fprintf(&s, "Input tokens:   %d\n", u.InputTokens)
		fmt.Fprintf(&s, "Output tokens:  %d\n", u.OutputTokens)
		if c, ok := Cost(cfg, u); ok {
			fmt.Fprintf(&s, "Estimated cost: $%.4f (ESTIMATE from your budget.*_price_per_mtok settings)\n", c)
		} else {
			s.WriteString("Estimated cost: unavailable (set budget.input_price_per_mtok / output_price_per_mtok to enable)\n")
		}
	}
	b := cfg.Budget
	fmt.Fprintf(&s, "\nBudget limit:   $%.2f per session (warn at %.0f%%) — guard %s\n", b.SessionLimit, b.WarningThreshold*100, onOff(b.Enabled))
	if ok {
		if c, cok := Cost(cfg, u); cok && b.SessionLimit > 0 {
			switch {
			case c >= b.SessionLimit:
				s.WriteString("Status:         OVER LIMIT (estimate)\n")
			case c >= b.SessionLimit*b.WarningThreshold:
				s.WriteString("Status:         approaching limit (estimate)\n")
			default:
				s.WriteString("Status:         within budget (estimate)\n")
			}
		} else {
			s.WriteString("Status:         cannot evaluate without a cost estimate\n")
		}
	}
	return s.String()
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
