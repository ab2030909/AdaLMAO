// Package templates embeds the mode context fragments.
package templates

import "embed"

//go:embed modes/*.md
var Modes embed.FS
