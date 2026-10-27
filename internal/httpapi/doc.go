// Package httpapi is the HTTP surface (AD-6, AD-7), read-only but for
// acknowledgement (ADR-0014): GET routes, the SSE stream and the embedded Board
// assets, plus the POST write routes that mark a finish seen or undo it, each
// behind a same-origin guard. It serves only the engine's published view and
// store reads, changes nothing but through engine.Engine.Acknowledge, and never
// calls herdr.
//
// Permitted internal imports: core, engine, store.
package httpapi
