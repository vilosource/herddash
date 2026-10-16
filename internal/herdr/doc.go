// Package herdr is the adapter that speaks to the herdr socket (AD-5, AD-11). It is
// the only package that opens the socket, and it only does: dial, ping, snapshot,
// subscribe, unsubscribe and stream-health signals. It decides nothing. It has no
// focus method and no pane-output method. Wire types live in gen/ and are mapped to
// core types at this package's edge.
//
// Permitted internal imports: core.
package herdr
