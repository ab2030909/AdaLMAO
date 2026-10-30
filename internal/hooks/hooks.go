// Package hooks implements the AdaL lifecycle-hook entry points (stdin JSON in, exit code / stdout out).
//
// Documented AdaL contract: hooks receive one JSON object on stdin; exit code 2 blocks PreToolUse and
// UserPromptSubmit (stderr is the reason); exit 0 with stdout JSON may carry additionalContext.
// Timeouts and launch failures are fail-open, so AdaLmao is a guardrail layer, not a sandbox.
package hooks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adalmao/adalmao/internal/budget"
	"github.com/adalmao/adalmao/internal/config"
	"github.com/adalmao/adalmao/internal/decision"
	"github.com/adalmao/adalmao/internal/enforce"
	"github.com/adalmao/adalmao/internal/guardrails"
	"github.com/adalmao/adalmao/internal/modes"
	"github.com/adalmao/adalmao/internal/sentinel"
)

// Input is the union of documented hook payload fields.
type Input struct {
	SessionID      string         `json:"session_id"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	TranscriptPath string         `json:"transcript_path"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	Prompt         string         `json:"prompt"`
}

// Result is what the process should do.
type Result struct {
	Exit   int
	Stdout string
	Stderr string
}

// Events AdaLmao registers for (installer uses this list).
var Events = []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"}

// EventFromArg maps the CLI arg (e.g. "pre-tool-use") to an event name.
func EventFromArg(a string) string {
	switch strings.ToLower(strings.ReplaceAll(a, "_", "-")) {
	case "pre-tool-use", "pretooluse":
		return "PreToolUse"
	case "post-tool-use", "posttooluse":
		return "PostToolUse"
	case "user-prompt-submit", "userpromptsubmit":
		return "UserPromptSubmit"
	case "stop":
		return "Stop"
	}
	return ""
}

// Handle processes one hook invocation. It must never panic or block for long.
func Handle(event string, stdin []byte) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Result{} // fail open on internal errors
		}
	}()
	captureIfRequested(event, stdin)
	var in Input
	if err := json.Unmarshal(stdin, &in); err != nil {
		return Result{}
	}
	if in.HookEventName != "" {
		event = in.HookEventName
	}
	cfg := config.MustLoad()
	if !cfg.General.Enabled {
		return Result{}
	}
	switch event {
	case "PreToolUse":
		return preTool(cfg, in)
	case "PostToolUse":
		return postTool(cfg, in)
	case "UserPromptSubmit":
		return userPrompt(cfg, in)
	case "Stop":
		budget.Refresh(in.SessionID, in.TranscriptPath)
	}
	return Result{}
}

// Evaluate runs all pre-execution checks for a tool call.
func Evaluate(cfg *config.Config, cwd, tool string, input map[string]any) []decision.Decision {
	kind, _ := guardrails.ClassifyTool(tool, input)
	if kind == guardrails.OpBash {
		cmd, _ := input["command"].(string)
		if cmd == "" {
			cmd, _ = input["cmd"].(string)
		}
		return enforce.EvalBash(cfg, cwd, cmd)
	}
	return guardrails.EvalFile(cfg, cwd, tool, input)
}

func preTool(cfg *config.Config, in Input) Result {
	ds := Evaluate(cfg, in.Cwd, in.ToolName, in.ToolInput)
	top := decision.Max(ds)
	if top.Level == decision.Safe {
		return Result{}
	}
	summary := ""
	if c, ok := in.ToolInput["command"].(string); ok {
		summary = config.Redact(c)
	} else if p, ok := in.ToolInput["file_path"].(string); ok {
		summary = p
	} else if p, ok := in.ToolInput["path"].(string); ok {
		summary = p
	}
	config.LogEvent(map[string]any{"event": "PreToolUse", "tool": in.ToolName, "level": top.Level.String(), "rule": top.Rule, "source": top.Source, "target": summary, "session": in.SessionID})

	if top.Level == decision.Blocked {
		var b strings.Builder
		b.WriteString("🚨 NICE TRY!\n\nAdaLmao blocked a dangerous operation. It was NOT executed.\n\n")
		fmt.Fprintf(&b, "Rule:   %s\nReason: %s\n", top.Rule, top.Reason)
		if top.Suggestion != "" {
			fmt.Fprintf(&b, "Safer alternative: %s\n", top.Suggestion)
		}
		for _, o := range ds {
			if o.Level == decision.Blocked && o.Rule != top.Rule {
				fmt.Fprintf(&b, "Also: [%s] %s\n", o.Rule, o.Reason)
			}
		}
		b.WriteString("\nDo not retry this operation or work around it. Tell the user and ask how to proceed.")
		return Result{Exit: 2, Stderr: b.String()}
	}
	// AdaL documents that PreToolUse cannot add context, so queue the notes and deliver them with the
	// next PostToolUse result for this session.
	var notes []string
	for _, o := range ds {
		if o.Level >= decision.Review {
			n := fmt.Sprintf("(%s) %s", o.Level, o.Reason)
			if o.Suggestion != "" {
				n += " — " + o.Suggestion
			}
			notes = append(notes, n)
		}
	}
	queueNotes(in.SessionID, notes)
	return Result{}
}
func postTool(cfg *config.Config, in Input) Result {
	var parts []string
	if notes := takeNotes(in.SessionID); len(notes) > 0 {
		parts = append(parts, "[AdaLmao] Heads-up on the tool call that just ran:\n- "+strings.Join(notes, "\n- "))
	}
	kind, _ := guardrails.ClassifyTool(in.ToolName, in.ToolInput)
	mutating := false
	switch kind {
	case guardrails.OpBash, guardrails.OpCreate, guardrails.OpEdit, guardrails.OpRewrite, guardrails.OpDelete:
		mutating = true
	}
	if cfg.Sentinel.Enabled && mutating {
		if fs := sentinel.Fresh(in.SessionID, sentinel.Check(cfg, in.Cwd)); len(fs) > 0 {
			config.LogEvent(map[string]any{"event": "PostToolUse", "tool": in.ToolName, "findings": len(fs), "first": fs[0].Msg, "session": in.SessionID})
			parts = append(parts, sentinel.Format(fs, cfg.Sentinel.Severity))
		}
	}
	if len(parts) == 0 {
		return Result{}
	}
	return contextJSON(strings.Join(parts, "\n\n"))
}
func userPrompt(cfg *config.Config, in Input) Result {
	var parts []string
	if m := modes.Compose(cfg.General.ActiveModes); m != "" {
		parts = append(parts, m)
	}
	if cfg.Budget.Enabled {
		budget.Refresh(in.SessionID, in.TranscriptPath)
		if w := budget.Guard(cfg, in.SessionID); w != "" {
			parts = append(parts, w)
		}
	}
	if len(parts) == 0 {
		return Result{}
	}
	return contextJSON(strings.Join(parts, "\n\n"))
}

func contextJSON(text string) Result {
	b, _ := json.Marshal(map[string]string{"additionalContext": text})
	return Result{Stdout: string(b)}
}
