// Package importer parses the existing herdr plugin's log into core Transitions
// for backfill (AD-13). Imported rows are written under epoch 0 with backfill
// provenance and a unique import key.
//
// Permitted internal imports: core, store, config.
package importer
