package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adalmao/adalmao/internal/config"
)

func TestInstallUninstallPreservesUserData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ADAL_HOME", dir)
	t.Setenv("ADALMAO_HOME", filepath.Join(dir, "mao"))
	orig := `{"version":"1.11.0","projects":{"x":{"a":1}},"hooks":{"PreToolUse":[{"matcher":"bash","hooks":[{"type":"command","command":"python mine.py","timeout":5}]}]}}`
	if err := os.WriteFile(SettingsPath(), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "adalmao.exe")
	for i := 0; i < 2; i++ { // idempotent
		if _, err := Install(exe); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(SettingsPath())
	if n := strings.Count(string(b), "hook pre-tool-use"); n != 1 {
		t.Fatalf("want exactly 1 pre-tool-use hook, got %d", n)
	}
	if !strings.Contains(string(b), "python mine.py") || !strings.Contains(string(b), `"projects"`) {
		t.Fatal("user data lost on install")
	}
	if len(Registered()) != 4 {
		t.Fatalf("want 4 registered events, got %d", len(Registered()))
	}
	if _, err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ = os.ReadFile(SettingsPath())
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "adalmao") || !strings.Contains(string(b), "python mine.py") || m["projects"] == nil {
		t.Fatalf("uninstall removed wrong things: %s", b)
	}
}

func TestConfigPreservesUnknownFields(t *testing.T) {
	t.Setenv("ADALMAO_HOME", t.TempDir())
	if err := os.WriteFile(config.Path(), []byte(`{"general":{"enabled":true,"custom":7},"future_thing":{"x":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	c.General.ActiveModes = []string{"paranoid"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(config.Path())
	if !strings.Contains(string(b), "future_thing") || !strings.Contains(string(b), "custom") || !strings.Contains(string(b), "paranoid") {
		t.Fatalf("unknown fields lost: %s", b)
	}
}

func TestHookCommandForms(t *testing.T) {
	cases := []struct{ goos, exe, want string }{
		{"windows", `C:\bin\adalmao.exe`, `C:\bin\adalmao.exe hook pre-tool-use`},
		{"linux", "/usr/local/bin/adalmao", "/usr/local/bin/adalmao hook pre-tool-use"},
		{"linux", "/opt/my tools/adalmao", "'/opt/my tools/adalmao' hook pre-tool-use"},
	}
	for _, c := range cases {
		got := hookCommandFor(c.goos, c.exe, "PreToolUse")
		if got != c.want {
			t.Errorf("%s: got %s want %s", c.goos, got, c.want)
		}
		if !owned(got) {
			t.Errorf("command not recognised as owned: %s", got)
		}
	}
	if owned("python adalmao_hook.py") || owned("echo hook pre-tool-use") {
		t.Error("foreign command wrongly treated as owned")
	}
}
