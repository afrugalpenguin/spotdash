// Package agent holds the one file that has to sit at the module root so it can
// be embedded: the example config a first run is created from.
//
// Embedding config.example.json rather than repeating its contents in Go keeps
// one source of truth for the defaults. The example someone reads in the repo
// is exactly what a first run writes, apart from the token.
package agent

import _ "embed"

// ExampleConfig is config.example.json.
//
//go:embed config.example.json
var ExampleConfig []byte
