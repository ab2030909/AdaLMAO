// Package sentinel detects suspicious project changes using git state and local coverage reports.
package sentinel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/git"
	"github.com/adalmao/adalmao/internal/guardrails"
)

type Finding struct {
	Key string // stable fingerprint for de-duplication
	Msg string
	Sev string // info | warning | block
}

var assertRe = regexp.MustCompile(`(?i)\b(assert\w*|expect\s*\(|t\.(error|fatal|errorf|fatalf)|require\.\w+|should\b|self\.assert\w+)`)

// Check inspects the git working tree under cwd. It never modifies anything.
func Check(cfg *config.Config, cwd string) []Finding {
	if !git.Available() {
		return nil
	}
	root := git.Root(cwd)
	if root == "" {
		return nil
	}
	sev := cfg.Sentinel.Severity
	var out []Finding

	entries, err := git.Status(root)
	if err == nil {
		var delTests, delOther []string
		for _, e := range entries {
			rel := strings.ToLower(filepath.ToSlash(e.Path))
			if e.Deleted() {
				if guardrails.IsTestFile(rel) {
					delTests = append(delTests, e.Path)
				} else {
					delOther = append(delOther, e.Path)
				}
			}
			if _, ok := guardrails.UserProtected(cfg, rel); ok && !e.Deleted() {
				out = append(out, Finding{"prot:" + e.Path, "Protected path modified: " + e.Path, sev})
			}
		}
		sort.Strings(delTests)
		if len(delTests) > 0 {
			out = append(out, Finding{"deltest:" + strings.Join(delTests, ","), "Test file(s) deleted: " + strings.Join(delTests, ", "), sev})
		}
		var importantDel, cfgDel []string
		for _, p := range delOther {
			l := strings.ToLower(filepath.ToSlash(p))
			if _, ok := guardrails.UserProtected(cfg, l); ok || guardrails.IsConfigFile(l) {
				importantDel = append(importantDel, p)
			} else {
				cfgDel = append(cfgDel, p)
			}
		}
		if len(importantDel) > 0 {
			out = append(out, Finding{"delimp:" + strings.Join(importantDel, ","), "Important file(s) deleted: " + strings.Join(importantDel, ", "), sev})
		}
		if len(cfgDel) >= 5 {
			out = append(out, Finding{"delmany:" + strconv.Itoa(len(cfgDel)), fmt.Sprintf("%d files deleted in the working tree", len(cfgDel)), "warning"})
		}
	}

	// Reduced assertions in modified test files.
	if diff, err := git.RunT(8*time.Second, root, "diff", "-U0", "HEAD"); err == nil && diff != "" {
		removed, added := 0, 0
		var files []string
		inTest := false
		cur := ""
		for _, l := range strings.Split(diff, "\n") {
			switch {
			case strings.HasPrefix(l, "+++ "):
				cur = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
				inTest = guardrails.IsTestFile(strings.ToLower(filepath.ToSlash(cur)))
			case strings.HasPrefix(l, "--- "):
			case inTest && strings.HasPrefix(l, "-") && assertRe.MatchString(l):
				removed++
				files = appendUnique(files, cur)
			case inTest && strings.HasPrefix(l, "+") && assertRe.MatchString(l):
				added++
			}
		}
		if removed > added {
			out = append(out, Finding{fmt.Sprintf("assert:%d:%d", removed, added),
				fmt.Sprintf("Test assertions reduced by %d (removed %d, added %d) in: %s", removed-added, removed, added, strings.Join(files, ", ")), sev})
		}
	}

	out = append(out, coverageFindings(root)...)
	return out
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// Coverage returns the coverage percent from a known report file, or ok=false if none is present.
func Coverage(root string) (pct float64, file string, ok bool) {
	cands := []string{"coverage.out", "cover.out", "coverage/lcov.info", "lcov.info", "coverage/coverage-summary.json", "coverage.xml", "cobertura.xml"}
	for _, c := range cands {
		p := filepath.Join(root, filepath.FromSlash(c))
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if v, ok := parseCoverage(c, string(b)); ok {
			return v, c, true
		}
	}
	return 0, "", false
}

var (
	goCovRe   = regexp.MustCompile(`(?m)^[^:\s]+:\S+ (\d+) (\d+)$`)
	lfRe      = regexp.MustCompile(`(?m)^LF:(\d+)`)
	lhRe      = regexp.MustCompile(`(?m)^LH:(\d+)`)
	lineRate  = regexp.MustCompile(`line-rate="([0-9.]+)"`)
	totalLine = regexp.MustCompile(`"total"\s*:\s*\{\s*"lines"\s*:\s*\{[^}]*"pct"\s*:\s*([0-9.]+)`)
)

func parseCoverage(name, s string) (float64, bool) {
	switch {
	case strings.HasSuffix(name, ".out"):
		var tot, cov int
		for _, m := range goCovRe.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(m[1])
			c, _ := strconv.Atoi(m[2])
			tot += n
			if c > 0 {
				cov += n
			}
		}
		if tot == 0 {
			return 0, false
		}
		return float64(cov) * 100 / float64(tot), true
	case strings.HasSuffix(name, "lcov.info"):
		lf, lh := sum(lfRe, s), sum(lhRe, s)
		if lf == 0 {
			return 0, false
		}
		return float64(lh) * 100 / float64(lf), true
	case strings.HasSuffix(name, ".json"):
		if m := totalLine.FindStringSubmatch(s); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			return v, true
		}
	case strings.HasSuffix(name, ".xml"):
		if m := lineRate.FindStringSubmatch(s); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			return v * 100, true
		}
	}
	return 0, false
}

func sum(re *regexp.Regexp, s string) int {
	t := 0
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		t += n
	}
	return t
}

func coverageFindings(root string) []Finding {
	pct, file, ok := Coverage(root)
	if !ok {
		return nil
	}
	sp := filepath.Join(config.Dir(), "state", "coverage.json")
	state := map[string]float64{}
	if b, err := os.ReadFile(sp); err == nil {
		_ = json.Unmarshal(b, &state)
	}
	prev, had := state[root]
	var out []Finding
	if had && pct < prev-0.5 {
		out = append(out, Finding{fmt.Sprintf("cov:%.1f", pct), fmt.Sprintf("Coverage dropped from %.1f%% to %.1f%% (%s)", prev, pct, file), "warning"})
	}
	if !had || pct > prev || (had && pct < prev-0.5) {
		state[root] = pct
		if b, err := json.Marshal(state); err == nil {
			_ = os.MkdirAll(filepath.Dir(sp), 0o755)
			_ = os.WriteFile(sp, b, 0o644)
		}
	}
	return out
}

// Fresh filters findings already reported in this session and records the new ones.
func Fresh(session string, fs []Finding) []Finding {
	if len(fs) == 0 {
		return nil
	}
	safe := regexp.MustCompile(`[^A-Za-z0-9_-]`).ReplaceAllString(session, "")
	if safe == "" {
		safe = "default"
	}
	sp := filepath.Join(config.Dir(), "state", "sentinel-"+safe+".json")
	seen := map[string]bool{}
	if b, err := os.ReadFile(sp); err == nil {
		_ = json.Unmarshal(b, &seen)
	}
	var out []Finding
	for _, f := range fs {
		if !seen[f.Key] {
			seen[f.Key] = true
			out = append(out, f)
		}
	}
	if len(out) > 0 {
		if b, err := json.Marshal(seen); err == nil {
			_ = os.MkdirAll(filepath.Dir(sp), 0o755)
			_ = os.WriteFile(sp, b, 0o644)
		}
	}
	return out
}

// Format renders findings as context text for the agent.
func Format(fs []Finding, severity string) string {
	var b strings.Builder
	b.WriteString("[AdaLmao Sentinel] Possible regressions detected:\n")
	for _, f := range fs {
		fmt.Fprintf(&b, "- [%s] %s\n", f.Sev, f.Msg)
	}
	if severity == "block" {
		b.WriteString("Severity is 'block': stop what you are doing, do not continue editing, and tell the user about these findings before proceeding. (A PostToolUse hook cannot undo the change itself.)")
	} else {
		b.WriteString("Review these before continuing; restore anything that was not intentionally requested.")
	}
	return b.String()
}
