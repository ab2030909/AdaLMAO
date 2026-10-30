// Package budget reads real token usage from AdaL's session transcript and implements the guard warnings.
//
// Verified against AdaL 1.11.5: the transcript (`transcript_path` in every hook payload) is JSONL and
// contains records like {"type":"usage","model":...,"input_tokens":..,"output_tokens":..,
// "cached_read_input_tokens":..,"cache_creation_tokens":..,"reasoning_tokens":..,"source":"main|subagent"}.
// Nothing is invented: with no usage records, usage is reported as unavailable. AdaL does not report prices,
// so cost is only ever an estimate from prices you configure.
package budget

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
)

type ModelUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CachedRead  int64 `json:"cached_read"`
	CacheCreate int64 `json:"cache_create"`
}

type Usage struct {
	InputTokens  int64                 `json:"input_tokens"`
	OutputTokens int64                 `json:"output_tokens"`
	CachedRead   int64                 `json:"cached_read_tokens"`
	CacheCreate  int64                 `json:"cache_creation_tokens"`
	Reasoning    int64                 `json:"reasoning_tokens"`
	ByModel      map[string]ModelUsage `json:"by_model,omitempty"`
	Source       string                `json:"source"`
	Updated      time.Time             `json:"updated"`
	Warned       string                `json:"warned"` // "", "threshold", "limit"
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

type usageRecord struct {
	Type        string `json:"type"`
	Model       string `json:"model"`
	Input       int64  `json:"input_tokens"`
	Output      int64  `json:"output_tokens"`
	CachedRead  int64  `json:"cached_read_input_tokens"`
	CacheCreate int64  `json:"cache_creation_tokens"`
	Reasoning   int64  `json:"reasoning_tokens"`
}

// ParseTranscript sums the usage records in a transcript. found is false if there are none.
func ParseTranscript(path string) (u Usage, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return u, false, err
	}
	defer f.Close()
	u.ByModel = map[string]ModelUsage{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var r usageRecord
		if json.Unmarshal(line, &r) != nil || r.Type != "usage" {
			continue
		}
		found = true
		u.InputTokens += r.Input
		u.OutputTokens += r.Output
		u.CachedRead += r.CachedRead
		u.CacheCreate += r.CacheCreate
		u.Reasoning += r.Reasoning
		m := u.ByModel[r.Model]
		m.Input += r.Input
		m.Output += r.Output
		m.CachedRead += r.CachedRead
		m.CacheCreate += r.CacheCreate
		u.ByModel[r.Model] = m
	}
	return u, found, sc.Err()
}

// Refresh scans the transcript for provider-reported usage and stores the totals.
// Returns false if the transcript has no usage records.
func Refresh(session, transcript string) bool {
	if transcript == "" {
		return false
	}
	u, found, err := ParseTranscript(transcript)
	if err != nil || !found {
		return false
	}
	prev, _ := Load(session)
	u.Warned = prev.Warned
	u.Source, u.Updated = "AdaL transcript usage records", time.Now()
	save(session, u)
	return true
}

// Cost returns an ESTIMATE based on user-supplied prices (ok=false when no prices configured).
// Cached tokens are priced with cached_input_price_per_mtok (0 = excluded from the estimate).
func Cost(cfg *config.Config, u Usage) (float64, bool) {
	b := cfg.Budget
	if b.InputPricePerMTok <= 0 && b.OutputPricePerMTok <= 0 && b.CachedInputPricePerMTok <= 0 {
		return 0, false
	}
	c := float64(u.InputTokens)/1e6*b.InputPricePerMTok +
		float64(u.OutputTokens)/1e6*b.OutputPricePerMTok +
		float64(u.CachedRead+u.CacheCreate)/1e6*b.CachedInputPricePerMTok
	return c, true
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
	if level == "" || u.Warned == level || u.Warned == "limit" {
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
		s.WriteString("  No usage has been recorded yet. AdaLmao reads token counts from the session transcript that\n")
		s.WriteString("  AdaL passes to hooks (UserPromptSubmit/Stop), so it fills in after you use AdaL with the hooks installed.\n")
	} else {
		fmt.Fprintf(&s, "Session:        %s\n", session)
		fmt.Fprintf(&s, "Source:         %s (provider-reported; updated %s)\n", u.Source, u.Updated.Format("2006-01-02 15:04"))
		fmt.Fprintf(&s, "Input tokens:   %d (uncached)\n", u.InputTokens)
		fmt.Fprintf(&s, "Output tokens:  %d\n", u.OutputTokens)
		fmt.Fprintf(&s, "Cached tokens:  %d read, %d written\n", u.CachedRead, u.CacheCreate)
		if u.Reasoning > 0 {
			fmt.Fprintf(&s, "Reasoning:      %d\n", u.Reasoning)
		}
		if len(u.ByModel) > 0 {
			names := make([]string, 0, len(u.ByModel))
			for n := range u.ByModel {
				names = append(names, n)
			}
			sort.Strings(names)
			s.WriteString("By model:\n")
			for _, n := range names {
				m := u.ByModel[n]
				fmt.Fprintf(&s, "  %-32s in %d  out %d  cache-read %d  cache-write %d\n", n, m.Input, m.Output, m.CachedRead, m.CacheCreate)
			}
		}
		if c, ok := Cost(cfg, u); ok {
			fmt.Fprintf(&s, "Estimated cost: $%.4f (ESTIMATE from the budget.*_price_per_mtok values you set; AdaL does not report prices)\n", c)
		} else {
			s.WriteString("Estimated cost: unavailable (set budget.input_price_per_mtok / output_price_per_mtok [/ cached_input_price_per_mtok])\n")
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
