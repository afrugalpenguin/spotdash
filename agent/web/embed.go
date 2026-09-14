// Package web holds the panel UI and embeds it into the binary.
//
// The embed directive has to live beside the files it embeds, which is why
// this package exists rather than the server package embedding a parent
// directory.
package web

import "embed"

// Files is the panel UI: one page, one stylesheet, and the face modules.
//
//go:embed index.html dev.html style.css app.js faces/*.js
var Files embed.FS
