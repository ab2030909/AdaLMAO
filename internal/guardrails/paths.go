package guardrails

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
)

// Norm converts p to a forward-slash path relative to cwd when possible (lowercased for matching).
func Norm(p, cwd string) string {
	p = strings.Trim(p, "\"'")
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[1:])
		}
	}
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	}
	return strings.ToLower(filepath.ToSlash(p))
}

func segments(rel string) []string { return strings.Split(rel, "/") }

func IsEnvFile(rel string) bool {
	b := path.Base(rel)
	for _, ok := range []string{".example", ".sample", ".template", ".dist"} {
		if strings.HasSuffix(b, ok) {
			return false
		}
	}
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}

func InGitDir(rel string) bool {
	for _, s := range segments(rel) {
		if s == ".git" {
			return true
		}
	}
	return false
}

func IsTestFile(rel string) bool {
	b := path.Base(rel)
	switch {
	case strings.HasSuffix(b, "_test.go"), strings.HasSuffix(b, "_test.py"), strings.HasPrefix(b, "test_") && strings.HasSuffix(b, ".py"):
		return true
	case strings.Contains(b, ".test.") || strings.Contains(b, ".spec."):
		return true
	case strings.HasSuffix(b, "test.java"), strings.HasSuffix(b, "tests.cs"), strings.HasSuffix(b, "_spec.rb"), strings.HasSuffix(b, "_test.rb"):
		return true
	}
	for _, s := range segments(path.Dir(rel)) {
		if s == "tests" || s == "test" || s == "__tests__" || s == "spec" {
			return true
		}
	}
	return false
}

var configNames = map[string]bool{
	"package.json": true, "package-lock.json": true, "go.mod": true, "go.sum": true, "tsconfig.json": true,
	"pyproject.toml": true, "requirements.txt": true, "cargo.toml": true, "dockerfile": true,
	"docker-compose.yml": true, "docker-compose.yaml": true, "makefile": true, ".gitignore": true,
	"pom.xml": true, "build.gradle": true, ".eslintrc.json": true, "webpack.config.js": true, "vite.config.ts": true,
}

func IsConfigFile(rel string) bool {
	if configNames[path.Base(rel)] {
		return true
	}
	return strings.HasPrefix(rel, ".github/workflows/")
}

// IsSelfConfig reports whether abs targets AdaL's settings or AdaLmao's own data (tamper protection).
func IsSelfConfig(p, cwd string) bool {
	p = strings.Trim(p, "\"'")
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p = strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
	adal := strings.ToLower(filepath.ToSlash(filepath.Join(config.AdalDir(), "settings.json")))
	mao := strings.ToLower(filepath.ToSlash(config.Dir())) + "/"
	return p == adal || strings.HasPrefix(p, mao)
}

// UserProtected checks user-defined protected paths (exact, glob, directory prefix, or basename glob).
func UserProtected(cfg *config.Config, rel string) (string, bool) {
	for _, pat := range cfg.Guardrails.ProtectedPaths {
		pat = strings.ToLower(filepath.ToSlash(strings.TrimSpace(pat)))
		pat = strings.TrimPrefix(pat, "./")
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/") {
			if strings.HasPrefix(rel+"/", pat) {
				return pat, true
			}
			continue
		}
		if ok, _ := path.Match(pat, rel); ok {
			return pat, true
		}
		if ok, _ := path.Match(pat, path.Base(rel)); ok && !strings.Contains(pat, "/") {
			return pat, true
		}
		if strings.HasPrefix(rel, pat+"/") {
			return pat, true
		}
	}
	return "", false
}
