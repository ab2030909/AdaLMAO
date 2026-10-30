// Package modes composes concise context fragments injected on UserPromptSubmit.
package modes

import (
	"fmt"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/templates"
)

// Priority is the precedence order when active modes conflict (earlier wins).
var Priority = []string{"paranoid", "debug", "focus", "ship-it", "perf", "tech-cofounder", "terse", "teacher"}

var conflicts = [][2]string{{"terse", "teacher"}, {"ship-it", "paranoid"}, {"focus", "tech-cofounder"}}

func Valid(name string) bool {
	for _, p := range Priority {
		if p == name {
			return true
		}
	}
	return false
}

// Normalize maps aliases (techcofounder, ship_it ...) to canonical names.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "shipit":
		return "ship-it"
	case "techcofounder", "tech", "cofounder":
		return "tech-cofounder"
	}
	return s
}

func load(name string) (desc string, bullets string, err error) {
	b, err := templates.Modes.ReadFile("modes/" + name + ".md")
	if err != nil {
		return "", "", err
	}
	lines := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
	desc = lines[0]
	if len(lines) > 1 {
		bullets = lines[1]
	}
	return desc, strings.TrimSpace(bullets), nil
}

// Description returns the one-line description of a mode.
func Description(name string) string {
	d, _, _ := load(name)
	return d
}

// Ordered returns the active modes sorted by priority, dropping unknown names.
func Ordered(active []string) []string {
	set := map[string]bool{}
	for _, a := range active {
		set[Normalize(a)] = true
	}
	var out []string
	for _, p := range Priority {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

// Conflicts returns human-readable notes about conflicting active modes.
func Conflicts(active []string) []string {
	ord := Ordered(active)
	idx := map[string]int{}
	for i, m := range ord {
		idx[m] = i
	}
	var notes []string
	for _, c := range conflicts {
		a, aok := idx[c[0]]
		b, bok := idx[c[1]]
		if aok && bok {
			w, l := c[0], c[1]
			if b < a {
				w, l = l, w
			}
			notes = append(notes, fmt.Sprintf("%s and %s conflict; %s takes precedence", w, l, w))
		}
	}
	return notes
}

// Compose builds the context block for the active modes (empty if none).
func Compose(active []string) string {
	ord := Ordered(active)
	if len(ord) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[AdaLmao modes: %s]\n", strings.Join(ord, ", "))
	for _, m := range ord {
		desc, bullets, err := load(m)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n%s\n", strings.ToUpper(m), desc, bullets)
	}
	if n := Conflicts(ord); len(n) > 0 {
		fmt.Fprintf(&b, "Precedence when instructions conflict: %s.\n", strings.Join(ord, " > "))
	}
	return strings.TrimSpace(b.String())
}

// Set validates and persists the active modes.
func Set(cfg *config.Config, names []string) error {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		n = Normalize(n)
		if !Valid(n) {
			return fmt.Errorf("unknown mode %q (available: %s)", n, strings.Join(Priority, ", "))
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) > config.MaxModes {
		return fmt.Errorf("at most %d modes can be active at once", config.MaxModes)
	}
	cfg.General.ActiveModes = out
	return cfg.Save()
}
