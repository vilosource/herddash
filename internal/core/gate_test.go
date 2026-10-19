package core

import "testing"

func TestDecideGate(t *testing.T) {
	cases := []struct {
		version string
		allow   bool
		gate    Gate
		resync  bool
	}{
		{"0.9.5", false, GateOK, true},
		{"0.10.0", false, GateOK, true},
		{"1.0.0", false, GateOK, true},
		{"v0.9.6", false, GateOK, true},
		{"0.9.5-rc1", false, GateOK, true},
		{"0.9.5+build.7", false, GateOK, true},
		{"0.9.5", true, GateOK, true},
		{"0.9.4", false, GateRefused, true},
		{"0.9.4-rc1", false, GateRefused, true},
		{"0.8.2", false, GateRefused, false},
		{"0.8.2", true, GateAdvisory, false},
		{"0.9.2", true, GateAdvisory, true},
		{"0.9.0", true, GateAdvisory, true},
		{"0.8.99", true, GateAdvisory, false},
		{"", false, GateRefused, false},
		{"", true, GateAdvisory, false},
		{"dev", false, GateRefused, false},
		{"dev", true, GateAdvisory, false},
		{"0.9", false, GateRefused, false},
		{"0.9.5.1", false, GateRefused, false},
		{"0.9.x", false, GateRefused, false},
		{"0..5", false, GateRefused, false},
		{"-0.9.5", false, GateRefused, false},
		{"v", false, GateRefused, false},
		{"vv0.9.5", false, GateRefused, false},
		{"0.9.+5", false, GateRefused, false},
		{" 0.9.5", false, GateRefused, false},
		{"99999999999999999999.0.0", false, GateRefused, false},
	}
	for _, c := range cases {
		gate, resync := DecideGate(c.version, c.allow)
		if gate != c.gate || resync != c.resync {
			t.Errorf("DecideGate(%q, %v) = %s, %v; want %s, %v", c.version, c.allow, gate, resync, c.gate, c.resync)
		}
	}
}

func TestHerdrMinimumParses(t *testing.T) {
	for _, v := range []string{HerdrMinimum, HerdrResyncMinimum} {
		if _, ok := parseVersion(v); !ok {
			t.Errorf("%q does not parse", v)
		}
	}
}
