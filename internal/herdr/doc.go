// Package herdr is the adapter that speaks to the herdr socket (AD-5, AD-11). It is
// the only package that opens the socket, and it only does: dial, ping, snapshot,
// subscribe, unsubscribe and stream-health signals. It decides nothing. It has no
// focus method and no pane-output method. Wire types live in gen/ and are mapped to
// core types at this package's edge.
//
// A Client holds no connection. Ping, Snapshot and ForegroundProcess each dial,
// write one request line, read one response line and close, because herdr answers
// one request per connection. SubscribeLifecycle and SubscribeStatus each own one
// long-lived connection; closing the Subscription is unsubscribing.
//
// When a subscription stream can no longer be trusted (events_lost, any other
// herdr error code, a closed or failed connection, an undecodable line) the
// Subscription delivers one Health signal last and closes its channel. A line on
// the lifecycle subscription that decodes as JSON but is not an event it can
// deliver is not one of those: it arrives as a non-final delivery flagged
// Unexpected, and the stream goes on. Retrying,
// reconnecting, resyncing, gating on version and tracking the pane set are the
// engine's.
//
// Permitted internal imports: core, and its own gen.
package herdr
