// Package frontend embeds the built user interface.
package frontend

import "embed"

// Dist holds the production build of the user interface (frontend/dist).
//
//go:embed all:dist
var Dist embed.FS
