// Package web embeds the paywall page so the binary has no files to ship.
package web

import "embed"

//go:embed paywall.html paywall.js paywall.css
var FS embed.FS
