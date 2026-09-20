// Package web holds the panel UI and embeds it into the binary. The embed
// directive must sit beside the files, and it cannot reach a parent directory.
package web

import "embed"

// Files is the panel UI: pages, stylesheet, and face modules.
//
//go:embed index.html dev.html settings.html style.css app.js settings.js faces/*.js
var Files embed.FS
