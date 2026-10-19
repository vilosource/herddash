// Package importer parses the existing herdr plugin's log into core Transitions
// for backfill (FR-9a, AD-13), and writes them through the store. Imported rows
// are written under epoch 0 with backfill provenance and a unique import key,
// so they are never a live Agent, and importing again adds nothing.
//
// # The log's format
//
// The agent-webhook plugin's hook.log, as observed on 2026-10-19, has one entry
// per line:
//
//	YYYY-MM-DD HH:MM:SS <message>
//
// The time is local time with no zone, so it is read in this machine's local
// time zone. There are two message forms:
//
//	posted <status> for <pane_id> -> <http code>
//	no url configured; would have sent {json}
//
// The first carries no Agent Program and no Profile. The second's JSON object
// carries "agent", "status" and "pane_id", and optionally "profile" (a string,
// or null), "workspace_id" and "title"; for example
//
//	2026-09-11 09:02:34 no url configured; would have sent {"agent": "claude", "profile": null, "status": "blocked", "pane_id": "w1:p1", "workspace_id": "w1", "title": null}
//
// The earliest entries have no "profile" key at all. Any other line, an entry
// whose status herdr does not report, and a line longer than 1 MiB, is counted
// as unparsed and skipped.
//
// # The rows
//
// Each entry becomes a Transition at epoch 0 for its pane id, with the Agent
// Program and Profile from the entry (unknown when absent or null), no
// previous Status, no herdr ordering and no herdr version, provenance
// backfill, and observed_at the entry's time read in local time. Its import
// key is "plugin-log:" + the hex SHA-256 of Host + "\x00" + Server + "\x00" +
// the raw line, + "#" + the line's occurrence index among identical lines in
// the file, so it is stable across re-runs and across a log that has grown,
// and an import under the wrong Host or Server does not block a corrected one.
// Host and Server come from the same configuration serve uses, so the import
// must be given serve's configuration flags. A done entry before local
// midnight of the date the Host reached herdr 0.9.2 is flagged possibly
// spurious, so a Host that never ran herdr 0.9.2 or newer passes a date after
// the last entry, such as tomorrow. An entry older than Retention is skipped.
//
// Permitted internal imports: core, store, config.
package importer
