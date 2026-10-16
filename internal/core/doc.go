// Package core is the pure centre of herddash (AD-1, AD-3, AD-13). It holds the
// domain vocabulary every other package shares, named after the PRD glossary, and
// every domain rule. It performs no I/O, imports only the standard library, and
// never reads the clock: time-dependent functions take the time as a parameter.
//
// Permitted internal imports: none.
package core
