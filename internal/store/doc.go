// Package store is the SQLite adapter (AD-4, AD-9, AD-10): open with an exclusive
// lock, numbered migrations, append-only Transition writes, history reads and fold
// input, and the Retention sweep.
//
// Permitted internal imports: core.
package store
