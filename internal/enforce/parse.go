package enforce

import (
	"path"
	"regexp"
	"strings"
)

// Cmd is one normalized simple command.
type Cmd struct {
	Prog      string   // lowercase basename without .exe
	Sub       string   // git subcommand, when Prog == "git"
	Args      []string // args after prog (and after git sub)
	Redirects []string // targets of > and >>
	Op        string   // operator that follows this command (; && || | &)
	Text      string   // normalized text used for user-rule matching
}

type seg struct {
	toks      []string
	redirects []string
	op        string
}

// Parse splits a shell command line into normalized simple commands. It understands quotes,
// separators (; && || | &), $(...) and `...`, sudo/env/nohup-style wrappers, `bash -c`,
// `cmd /c`, `powershell -Command`, and git global options. It is a practical approximation,
// not a full shell parser.
func Parse(command string) []Cmd { return parse(command, 0) }

var subRe = regexp.MustCompile("\\$\\(([^()]*)\\)|`([^`]*)`")

func parse(command string, depth int) []Cmd {
	if depth > 4 || strings.TrimSpace(command) == "" {
		return nil
	}
	var out []Cmd
	for _, m := range subRe.FindAllStringSubmatch(command, -1) {
		inner := m[1]
		if inner == "" {
			inner = m[2]
		}
		out = append(out, parse(inner, depth+1)...)
	}
	for _, s := range split(command) {
		out = append(out, normalize(s, depth)...)
	}
	return out
}

func split(cmd string) []seg {
	var segs []seg
	var cur seg
	var b strings.Builder
	has := false
	var q rune
	rs := []rune(cmd)
	flush := func() {
		if has {
			cur.toks = append(cur.toks, b.String())
		}
		b.Reset()
		has = false
	}
	endSeg := func(op string) {
		flush()
		cur.op = op
		if len(cur.toks) > 0 {
			segs = append(segs, cur)
		}
		cur = seg{}
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if q != 0 {
			if r == q {
				q = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		var next rune
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		switch r {
		case '\'', '"':
			q = r
			has = true
		case ' ', '\t', '\r':
			flush()
		case '\n', ';':
			endSeg(";")
		case '&':
			if next == '&' {
				i++
				endSeg("&&")
			} else if i > 0 && rs[i-1] == '>' {
				b.WriteRune(r)
				has = true
			} else {
				endSeg("&")
			}
		case '|':
			if next == '|' {
				i++
				endSeg("||")
			} else {
				endSeg("|")
			}
		case '>':
			// drop fd prefix like 2>
			if has && isDigits(b.String()) {
				b.Reset()
				has = false
			}
			flush()
			tok := ">"
			if next == '>' {
				i++
				tok = ">>"
			}
			cur.toks = append(cur.toks, tok)
		default:
			b.WriteRune(r)
			has = true
		}
	}
	endSeg("")
	return segs
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var wrapperSkip = map[string]bool{"sudo": true, "env": true, "command": true, "nohup": true, "time": true, "exec": true, "builtin": true, "nice": true, "xargs": true, "doas": true, "stdbuf": true, "setsid": true, "call": true, "start": true}
var shellProgs = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true}

func progName(t string) string {
	t = strings.ToLower(strings.ReplaceAll(t, "\\", "/"))
	t = path.Base(t)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		t = strings.TrimSuffix(t, ext)
	}
	return t
}

func isAssign(t string) bool {
	i := strings.Index(t, "=")
	if i <= 0 {
		return false
	}
	for _, r := range t[:i] {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func normalize(s seg, depth int) []Cmd {
	// split redirects out of tokens
	var toks []string
	var redirs []string
	for i := 0; i < len(s.toks); i++ {
		if s.toks[i] == ">" || s.toks[i] == ">>" {
			if i+1 < len(s.toks) {
				redirs = append(redirs, s.toks[i+1])
				i++
			}
			continue
		}
		toks = append(toks, s.toks[i])
	}
	if len(toks) > 0 {
		toks[0] = strings.TrimLeft(toks[0], "({")
		if toks[0] == "" {
			toks = toks[1:]
		}
	}
	for len(toks) > 0 {
		last := strings.TrimRight(toks[len(toks)-1], ")}")
		if last == "" {
			toks = toks[:len(toks)-1]
			continue
		}
		toks[len(toks)-1] = last
		break
	}
	var extra []Cmd
	for len(toks) > 0 {
		t := toks[0]
		if isAssign(t) {
			toks = toks[1:]
			continue
		}
		p := progName(t)
		if wrapperSkip[p] {
			toks = toks[1:]
			// skip wrapper flags (and assignments for env)
			for len(toks) > 0 && (strings.HasPrefix(toks[0], "-") || isAssign(toks[0])) {
				if (p == "sudo" || p == "doas") && (toks[0] == "-u" || toks[0] == "-g") && len(toks) > 1 {
					toks = toks[1:]
				}
				toks = toks[1:]
			}
			continue
		}
		if shellProgs[p] || p == "cmd" || p == "powershell" || p == "pwsh" {
			for i := 1; i < len(toks); i++ {
				f := strings.ToLower(toks[i])
				if f == "-c" || f == "/c" || f == "/k" || f == "-command" || f == "-c:" || strings.HasSuffix(f, "c") && strings.HasPrefix(f, "-") && shellProgs[p] {
					inner := strings.Join(toks[i+1:], " ")
					if len(toks) == i+2 {
						inner = toks[i+1]
					}
					extra = parse(inner, depth+1)
					break
				}
			}
		}
		break
	}
	var cmds []Cmd
	if len(toks) > 0 {
		c := Cmd{Prog: progName(toks[0]), Args: toks[1:], Redirects: redirs, Op: s.op}
		if c.Prog == "git" {
			c.Sub, c.Args = gitSub(c.Args)
		}
		c.Text = strings.ToLower(strings.TrimSpace(c.Prog + " " + c.Sub + " " + strings.Join(c.Args, " ")))
		c.Text = strings.Join(strings.Fields(c.Text), " ")
		cmds = append(cmds, c)
	}
	return append(cmds, extra...)
}

var gitArgOpts = map[string]bool{"-c": true, "-C": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true}

func gitSub(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return strings.ToLower(a), args[i+1:]
		}
		if gitArgOpts[a] {
			i++
		}
	}
	return "", nil
}
