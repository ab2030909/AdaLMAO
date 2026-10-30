// Package config manages ~/.adalmao/config.json (defaults, validation, unknown-field preservation).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const CurrentVersion = 1
const MaxModes = 6

type General struct {
	Enabled     bool     `json:"enabled"`
	ActiveModes []string `json:"active_modes"`
}

type Guardrails struct {
	Enabled                  bool     `json:"enabled"`
	ProtectEnvFiles          bool     `json:"protect_env_files"`
	ProtectGitDir            bool     `json:"protect_git_directory"`
	ProtectTests             bool     `json:"protect_tests"`
	ProtectConfigFiles       bool     `json:"protect_config_files"`
	WarnOnPublicAPIChanges   bool     `json:"warn_on_public_api_changes"`
	WarnOnDefaultValueChange bool     `json:"warn_on_default_value_changes"`
	ProtectedPaths           []string `json:"protected_paths"`
}

type Rule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Pattern string `json:"pattern"`
	Regex   bool   `json:"regex,omitempty"`
	Action  string `json:"action"` // block | warn
	Reason  string `json:"reason"`
}

type Enforce struct {
	Enabled         bool   `json:"enabled"`
	AllowDCOSignoff bool   `json:"allow_dco_signoff"`
	// DisabledBuiltins lists built-in rule IDs the user has switched off (e.g. "git-clean-force").
	DisabledBuiltins []string `json:"disabled_builtins"`
	Rules            []Rule   `json:"rules"`
}

type Sentinel struct {
	Enabled  bool   `json:"enabled"`
	Severity string `json:"severity"` // info | warning | block
}

type Budget struct {
	Enabled          bool    `json:"enabled"`
	SessionLimit     float64 `json:"session_limit"`
	WarningThreshold float64 `json:"warning_threshold"`
	// Optional user-supplied prices (USD per 1M tokens). Costs derived from them are estimates.
	InputPricePerMTok  float64 `json:"input_price_per_mtok"`
	OutputPricePerMTok float64 `json:"output_price_per_mtok"`
}

type Git struct {
	PreflightEnabled bool   `json:"preflight_enabled"`
	BaseBranch       string `json:"base_branch"`
}

type Attest struct {
	Enabled bool   `json:"enabled"`
	Trailer string `json:"trailer"`
}

type Config struct {
	Version    int                 `json:"version"`
	General    General             `json:"general"`
	Guardrails Guardrails          `json:"guardrails"`
	Enforce    Enforce             `json:"enforce"`
	Sentinel   Sentinel            `json:"sentinel"`
	Budget     Budget              `json:"budget"`
	Git        Git                 `json:"git"`
	Attest     Attest              `json:"attest"`
	Profiles   map[string][]string `json:"profiles"`

	raw map[string]json.RawMessage // original document, used to preserve unknown fields
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{
		Version:  CurrentVersion,
		General:  General{Enabled: true, ActiveModes: []string{"focus"}},
		Guardrails: Guardrails{
			Enabled: true, ProtectEnvFiles: true, ProtectGitDir: true, ProtectTests: true,
			ProtectConfigFiles: true, WarnOnPublicAPIChanges: true, WarnOnDefaultValueChange: true,
			ProtectedPaths: []string{},
		},
		Enforce:  Enforce{Enabled: true, Rules: []Rule{}},
		Sentinel: Sentinel{Enabled: true, Severity: "warning"},
		Budget:   Budget{Enabled: true, SessionLimit: 5.0, WarningThreshold: 0.8},
		Git:      Git{PreflightEnabled: true, BaseBranch: ""},
		Attest:   Attest{Enabled: false, Trailer: "Assisted-by: AdaL"},
		Profiles: map[string][]string{
			"builder":   {"ship-it"},
			"reviewer":  {"paranoid"},
			"optimizer": {"perf"},
			"mentor":    {"teacher"},
		},
	}
}

// Dir returns the AdaLmao data directory ($ADALMAO_HOME or ~/.adalmao).
func Dir() string {
	if d := os.Getenv("ADALMAO_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		h = "."
	}
	return filepath.Join(h, ".adalmao")
}

// AdalDir returns AdaL's config directory ($ADAL_HOME or ~/.adal).
func AdalDir() string {
	if d := os.Getenv("ADAL_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		h = "."
	}
	return filepath.Join(h, ".adal")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config. A missing file yields defaults. An invalid file returns defaults plus an error.
func Load() (*Config, error) {
	c := Default()
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return Default(), fmt.Errorf("invalid %s: %w (using defaults)", Path(), err)
	}
	_ = json.Unmarshal(b, &c.raw)
	c.migrate()
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// MustLoad is for hooks: never fails, falls back to defaults.
func MustLoad() *Config {
	c, _ := Load()
	return c
}

func (c *Config) migrate() {
	if c.Version < CurrentVersion {
		c.Version = CurrentVersion
	}
	if c.Profiles == nil {
		c.Profiles = Default().Profiles
	}
}

var validAction = map[string]bool{"block": true, "warn": true}

// Validate checks values. It does not mutate.
func (c *Config) Validate() error {
	var errs []string
	if len(c.General.ActiveModes) > MaxModes {
		errs = append(errs, fmt.Sprintf("at most %d active modes allowed", MaxModes))
	}
	if c.Budget.WarningThreshold <= 0 || c.Budget.WarningThreshold > 1 {
		errs = append(errs, "budget.warning_threshold must be in (0, 1]")
	}
	if c.Budget.SessionLimit < 0 {
		errs = append(errs, "budget.session_limit must be >= 0")
	}
	switch c.Sentinel.Severity {
	case "info", "warning", "block":
	default:
		errs = append(errs, "sentinel.severity must be info, warning or block")
	}
	ids := map[string]bool{}
	for i, r := range c.Enforce.Rules {
		if r.ID == "" || r.Pattern == "" {
			errs = append(errs, fmt.Sprintf("enforce.rules[%d]: id and pattern are required", i))
			continue
		}
		if ids[r.ID] {
			errs = append(errs, fmt.Sprintf("enforce.rules[%d]: duplicate id %q", i, r.ID))
		}
		ids[r.ID] = true
		if !validAction[r.Action] {
			errs = append(errs, fmt.Sprintf("enforce.rules[%d]: action must be block or warn", i))
		}
		if r.Regex {
			if _, err := regexp.Compile(r.Pattern); err != nil {
				errs = append(errs, fmt.Sprintf("enforce.rules[%d]: bad regex: %v", i, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.New("config problems: " + strings.Join(errs, "; "))
	}
	return nil
}

// Save writes the config atomically, preserving unknown fields from the original document.
func (c *Config) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var fresh map[string]any
	_ = json.Unmarshal(b, &fresh)
	out := map[string]any{}
	for k, v := range c.raw {
		var x any
		if json.Unmarshal(v, &x) == nil {
			out[k] = x
		}
	}
	merge(out, fresh)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(Path(), append(data, '\n'))
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok && k != "profiles" {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// WriteFileAtomic writes via temp file + rename.
func WriteFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		return os.Rename(tmp, path)
	}
	return nil
}
