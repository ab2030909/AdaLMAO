// Package decision holds the shared verdict type used by guardrails, enforce and sentinel.
package decision

// Level classifies how dangerous an operation is.
type Level int

const (
	Safe Level = iota
	Review
	Risky
	Blocked
)

func (l Level) String() string {
	switch l {
	case Review:
		return "review"
	case Risky:
		return "risky"
	case Blocked:
		return "blocked"
	}
	return "safe"
}

// Decision is the verdict for a single operation.
type Decision struct {
	Level      Level  `json:"level"`
	Rule       string `json:"rule"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion,omitempty"`
	Source     string `json:"source"` // enforce | guardrails
}

// Max returns the most severe decision (first wins on ties).
func Max(ds []Decision) Decision {
	best := Decision{Level: Safe}
	for _, d := range ds {
		if d.Level > best.Level {
			best = d
		}
	}
	return best
}
