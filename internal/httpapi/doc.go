// Package httpapi is the read-only HTTP surface (AD-6, AD-7): GET routes, the SSE
// stream and the embedded Board assets. It serves only the engine's published view
// and store reads, and never calls herdr.
//
// Permitted internal imports: core, engine, store.
package httpapi
