// Package agent holds the example config a first run is created from. It sits
// at the module root so it can be embedded, and a first run writes it with a
// generated token.
package agent

import _ "embed"

// ExampleConfig is config.example.json.
//
//go:embed config.example.json
var ExampleConfig []byte
