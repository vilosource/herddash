// Package gen holds herddash's herdr wire types, generated from the API schema of a
// herdr 0.9.5 binary (herdr-api-0.9.5.schema.json, the verbatim output of
// `herdr api schema --json`). Only internal/herdr imports it; the types never leak
// past that package's edge (AD-5).
//
// Regenerate with `go generate ./internal/herdr/gen`. The generator is genwire, in
// this directory, and imports only the standard library. It generates only what
// herddash needs, and never a focus method or a pane-output method. A test fails if
// the committed output differs from a fresh run.
//
// To move to a newer herdr: dump its schema with `herdr api schema --json` into a new
// file here, point the go:generate line at it, regenerate, and review the diff.
package gen

//go:generate go run ./genwire -schema herdr-api-0.9.5.schema.json -out wire_gen.go
