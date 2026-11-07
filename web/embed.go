// Package web embeds the static UI.
package web

import "embed"

// FS holds index.html, app.js and style.css.
//
//go:embed index.html app.js style.css
var FS embed.FS
