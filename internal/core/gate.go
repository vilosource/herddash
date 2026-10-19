package core

import (
	"strconv"
	"strings"
)

// Gate is the version gate's outcome for one herdr server (FR-16, ADR-0006).
type Gate string

// The three Gate outcomes.
const (
	// GateOK is herdr HerdrMinimum or newer: observed normally.
	GateOK Gate = "ok"
	// GateAdvisory is herdr older than HerdrMinimum with the dogfooding
	// override set: observed normally, with a warning.
	GateAdvisory Gate = "advisory"
	// GateRefused is herdr older than HerdrMinimum without the override:
	// observed by nothing.
	GateRefused Gate = "refused"
)

// HerdrMinimum is the oldest herdr herddash supports. Below it herdr
// under-reports blocked.
const HerdrMinimum = "0.9.5"

// HerdrResyncMinimum is the oldest herdr for which the resync guarantee holds.
// Below it herdr replays stale events and gives no lost-events signal.
const HerdrResyncMinimum = "0.9.0"

// DecideGate decides the gate for the version string herdr's ping returned,
// never its protocol number. version is MAJOR.MINOR.PATCH, with an optional
// leading "v" and any "-" or "+" suffix ignored. An empty or unparsable
// version cannot be shown to be supported, so it is treated as below the
// minimum. resyncGuaranteed is false below HerdrResyncMinimum or when the
// version is unparsable.
func DecideGate(version string, allowUnsupported bool) (gate Gate, resyncGuaranteed bool) {
	v, ok := parseVersion(version)
	minimum, _ := parseVersion(HerdrMinimum)
	resyncMin, _ := parseVersion(HerdrResyncMinimum)
	resyncGuaranteed = ok && !v.less(resyncMin)
	switch {
	case ok && !v.less(minimum):
		return GateOK, resyncGuaranteed
	case allowUnsupported:
		return GateAdvisory, resyncGuaranteed
	default:
		return GateRefused, resyncGuaranteed
	}
}

type semver [3]uint64

func (a semver) less(b semver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// parseVersion parses MAJOR.MINOR.PATCH, tolerating a leading "v" and
// ignoring anything from the first "-" or "+".
func parseVersion(s string) (semver, bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return semver{}, false
		}
		v[i] = n
	}
	return v, true
}
