package budget

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adalmao/adalmao/internal/config"
)

// Lines copied from a real AdaL 1.11.5 transcript (shape only).
const sample = `{"type": "usage", "turn_id": "t1", "step": 0, "model": "qwen-qwen3.8-flash", "input_tokens": 100, "output_tokens": 10, "cached_read_input_tokens": 5, "cache_creation_tokens": 7, "reasoning_tokens": 0, "source": "main"}
{"type": "metadata", "conversation_id": "x", "note": "input_tokens mentioned but not a usage record"}
{"type": "usage", "turn_id": "t1", "step": 1, "model": "qwen-qwen3.8-flash", "input_tokens": 50, "output_tokens": 20, "cached_read_input_tokens": 0, "cache_creation_tokens": 0, "reasoning_tokens": 3, "source": "subagent"}
not json at all
`

func TestParseAndGuard(t *testing.T) {
	t.Setenv("ADALMAO_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), "conversation_x.jsonl")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	u, found, err := ParseTranscript(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if u.InputTokens != 150 || u.OutputTokens != 30 || u.CachedRead != 5 || u.CacheCreate != 7 || u.Reasoning != 3 {
		t.Fatalf("wrong totals: %+v", u)
	}
	if !Refresh("sess1", p) {
		t.Fatal("refresh failed")
	}
	cfg := config.Default()
	cfg.Budget.SessionLimit = 1.0
	if w := Guard(cfg, "sess1"); w != "" {
		t.Fatal("no prices set: guard must stay silent (no invented cost)")
	}
	cfg.Budget.InputPricePerMTok = 10000 // absurdly high so 150 tokens = $1.50 crosses the $1 limit
	if w := Guard(cfg, "sess1"); w == "" {
		t.Fatal("expected a warning")
	}
	if w := Guard(cfg, "sess1"); w != "" {
		t.Fatal("warning must not repeat")
	}
}
