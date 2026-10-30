// Package tune reports AdaL's real model / reasoning-effort configuration. It is read-only.
package tune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adalmao/adalmao/internal/config"
)

type catalogModel struct {
	Key           string `json:"key"`
	DisplayName   string `json:"display_name"`
	ConfigOptions struct {
		Effort []string `json:"effort"`
	} `json:"config_options"`
}

type catalog struct {
	DefaultModel string         `json:"default_model"`
	Models       []catalogModel `json:"models"`
}

type projectPref struct {
	ModelPreference struct {
		DefaultModel   string            `json:"default_model"`
		PerModelEffort map[string]string `json:"per_model_effort"`
	} `json:"model_preference"`
}

type settings struct {
	Projects map[string]projectPref `json:"projects"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Report builds the tune display for the project at cwd.
func Report(cwd string) string {
	var s strings.Builder
	s.WriteString("😂 AdaLmao Tune (read-only)\n\n")
	var cat catalog
	catErr := readJSON(filepath.Join(config.AdalDir(), "model_catalog.json"), &cat)
	var st settings
	setErr := readJSON(filepath.Join(config.AdalDir(), "settings.json"), &st)

	model, source := "", ""
	var pref projectPref
	if setErr == nil {
		for k, v := range st.Projects {
			if strings.EqualFold(filepath.Clean(k), filepath.Clean(cwd)) {
				pref = v
			}
		}
	}
	if pref.ModelPreference.DefaultModel != "" {
		model, source = pref.ModelPreference.DefaultModel, "project preference in settings.json"
	} else if catErr == nil && cat.DefaultModel != "" {
		model, source = cat.DefaultModel, "AdaL catalog default (no project override)"
	}
	if model == "" {
		s.WriteString("Current model:  unavailable (could not read AdaL settings/catalog)\n")
	} else {
		name := model
		for _, m := range cat.Models {
			if m.Key == model && m.DisplayName != "" {
				name = m.DisplayName + " (" + model + ")"
			}
		}
		fmt.Fprintf(&s, "Current model:  %s — %s\n", name, source)
	}
	var options []string
	for _, m := range cat.Models {
		if m.Key == model {
			options = m.ConfigOptions.Effort
		}
	}
	if len(options) == 0 {
		s.WriteString("Effort control: not exposed for this model (or model unknown)\n")
	} else {
		cur := pref.ModelPreference.PerModelEffort[model]
		if cur == "" {
			cur = "not set (model default applies)"
		}
		fmt.Fprintf(&s, "Effort setting: %s\n", cur)
		fmt.Fprintf(&s, "Supported:      %s\n", strings.Join(options, ", "))
	}
	if len(pref.ModelPreference.PerModelEffort) > 0 {
		s.WriteString("Per-model effort saved for this project:\n")
		for k, v := range pref.ModelPreference.PerModelEffort {
			fmt.Fprintf(&s, "  %s = %s\n", k, v)
		}
	}
	s.WriteString("\nHow to change it (AdaLmao never edits your model settings):\n")
	s.WriteString("  - interactive: /model config inside AdaL\n")
	s.WriteString("  - headless:    adal -q \"...\" --thinking-effort <level>   (note: AdaL persists this to model config)\n")
	s.WriteString("\nPrompt-level alternative: `adalmao mode on focus terse` asks the model to be concise.\n")
	s.WriteString("That is only an instruction; it does not change the model's internal reasoning parameter.\n")
	return s.String()
}
