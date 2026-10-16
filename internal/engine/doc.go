// Package engine is the single-writer actor (AD-2, AD-11). It alone owns the pane
// set and current state, decides when to resync, runs the reconcile and Retention
// timers, applies the version gate, detects server epochs, and publishes immutable
// views. Everything else reaches its state by message.
//
// Permitted internal imports: core, herdr, procenv, store.
package engine
