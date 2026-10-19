package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/herddash/internal/config"
	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/store"
)

// zone is a fixed zone, so the tests do not depend on the machine's.
var zone = time.FixedZone("test", 2*3600)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation(timeLayout, s, zone)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fixture returns testdata/hook.log: lines trimmed from the real plugin log,
// Profiles anonymised, pane ids kept.
func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lines(b []byte) []string { return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") }

// keyOf is the import key of line for opts' Host and Server.
func keyOf(line string, occurrence int) string {
	sum := sha256.Sum256([]byte("mac\x00default\x00" + line))
	return fmt.Sprintf("plugin-log:%s#%d", hex.EncodeToString(sum[:]), occurrence)
}

// opts keeps every entry and flags nothing.
func opts() Options {
	return Options{Host: "mac", Server: "default", Location: zone}
}

func parse(t *testing.T, b []byte, o Options) (Result, []core.Transition) {
	t.Helper()
	res, ts, err := Parse(bytes.NewReader(b), o)
	if err != nil {
		t.Fatal(err)
	}
	return res, ts
}

// row describes a Transition by its program, profile and status, "-" for
// unknown.
func row(tr core.Transition) string {
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	return fmt.Sprintf("%s %s %s %s", tr.Key().PaneID, dash(string(tr.AgentProgram())), dash(tr.Profile()), tr.New())
}

func TestParseFixture(t *testing.T) {
	b := fixture(t)
	ls := lines(b)
	res, ts := parse(t, b, opts())
	if res != (Result{Parsed: 21}) || len(ts) != 21 {
		t.Fatalf("result %+v with %d rows, want 21 parsed and kept", res, len(ts))
	}
	for i, tr := range ts {
		k := tr.Key()
		_, hasPrev := tr.Previous()
		_, hasOrder := tr.HerdrOrder()
		if k.Host != "mac" || k.Server != "default" || k.Epoch != 0 || hasPrev || hasOrder ||
			tr.HerdrVersion() != "" || tr.Provenance() != core.ProvenanceBackfill || tr.PossiblySpurious() {
			t.Errorf("row %d: %+v", i, tr)
		}
		if want := keyOf(ls[i], 0); tr.ImportKey() != want {
			t.Errorf("row %d import key %q, want %q", i, tr.ImportKey(), want)
		}
		if want := at(t, ls[i][:19]).UnixMilli(); tr.ObservedAt() != want {
			t.Errorf("row %d observed_at %d, want %d", i, tr.ObservedAt(), want)
		}
	}
	for i, want := range map[int]string{
		0:  "w1:pW - - blocked",          // posted form: no program, no profile
		10: "w1:pC - - done",             // posted done
		2:  "w1:pE claude - done",        // early JSON without a profile key
		11: "w1:pE claude personal done", // a Profile
		14: "w1:p1 claude - blocked",     // "profile": null
		15: "w1:pW claude other blocked", // anonymised cldo
		16: "wA:p7 claude work blocked",  // anonymised cldw
		18: "wA:p3 agy - done",           // other Agent Programs
		19: "wA:p3 codex - blocked",
	} {
		if got := row(ts[i]); got != want {
			t.Errorf("row %d %q, want %q", i, got, want)
		}
	}
	// The observed time is the line's local time, as UTC ms.
	if got, want := ts[0].ObservedAt(), time.Date(2026, 9, 10, 15, 59, 5, 0, time.UTC).UnixMilli(); got != want {
		t.Errorf("first row observed_at %d, want %d (17:59:05 at UTC+2)", got, want)
	}
}

func TestParseDefaultsToLocalTime(t *testing.T) {
	line := "2026-09-10 17:59:05 posted blocked for w1:pW -> 204\n"
	_, ts := parse(t, []byte(line), Options{Host: "mac", Server: "default"})
	want := time.Date(2026, 9, 10, 17, 59, 5, 0, time.Local).UnixMilli()
	if len(ts) != 1 || ts[0].ObservedAt() != want {
		t.Fatalf("rows %+v, want one at %d", ts, want)
	}
}

func TestParseSpurious(t *testing.T) {
	o := opts()
	o.Since = at(t, "2026-09-14 00:00:00")
	res, ts := parse(t, fixture(t), o)
	var flagged []int
	for i, tr := range ts {
		if tr.PossiblySpurious() {
			flagged = append(flagged, i)
			if tr.New() != core.StatusDone {
				t.Errorf("row %d flagged with status %s", i, tr.New())
			}
		}
	}
	// Every done before 2026-09-14 local midnight; the done entries on the
	// 14th, and every blocked entry, are not flagged.
	if got, want := fmt.Sprint(flagged), "[2 3 4 5 6 7 8 10 11 12]"; got != want {
		t.Errorf("flagged rows %s, want %s", got, want)
	}
	if res.Spurious != 10 {
		t.Errorf("result %+v, want 10 spurious", res)
	}
}

func TestParseOld(t *testing.T) {
	o := opts()
	o.Cutoff = at(t, "2026-09-11 00:00:00").UnixMilli()
	o.Since = at(t, "2026-10-19 00:00:00")
	res, ts := parse(t, fixture(t), o)
	// The 14 entries of 2026-09-10 are skipped and counted; the 7 later ones
	// are kept, and only their done entries count as spurious.
	if res != (Result{Parsed: 21, Old: 14, Spurious: 2}) || len(ts) != 7 {
		t.Errorf("result %+v with %d rows, want 21 parsed, 14 old, 2 spurious, 7 rows", res, len(ts))
	}
	if len(ts) > 0 && row(ts[0]) != "w1:p1 claude - blocked" {
		t.Errorf("first kept row %q", row(ts[0]))
	}
}

func TestParseGarbage(t *testing.T) {
	good := "2026-09-10 18:28:00 posted done for w1:pC -> 204"
	log := strings.Join([]string{
		"",
		"hello",
		"2026-09-10 18:28:00",
		"2026-13-01 18:28:00 posted done for w1:pC -> 204",
		"2026-09-10T18:28:00 posted done for w1:pC -> 204",
		"2026-09-10 18:28:00 posted sleeping for w1:pC -> 204",
		"2026-09-10 18:28:00 posted done for w1:pC",
		"2026-09-10 18:28:00 posted done for  -> 204",
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "pane_id": "w1:pC"}`,
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "status": "done"}`,
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "status": "done", "pane_id": ""}`,
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "status": "done", "pane_id": "w1:pC"} trailing`,
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "status": "done", "pane_id": "w1:pC"`,
		`2026-09-10 18:28:00 no url configured; would have sent {"agent": "claude", "status": "UNKNOWN", "pane_id": "w1:pC"}`,
		`2026-09-10 18:28:00 sending {"agent": "claude", "status": "done", "pane_id": "w1:pC"}`,
		good,
	}, "\n")
	res, ts := parse(t, []byte(log), opts())
	if res != (Result{Parsed: 1, Unparsed: 15}) || len(ts) != 1 || row(ts[0]) != "w1:pC - - done" {
		t.Errorf("result %+v, rows %d, want 1 parsed and 15 unparsed", res, len(ts))
	}
}

func TestParseIdenticalLines(t *testing.T) {
	line := `2026-09-14 06:45:39 no url configured; would have sent {"agent": "claude", "profile": "work", "status": "blocked", "pane_id": "wA:p7", "workspace_id": "wA", "title": null}`
	other := "2026-09-10 18:28:00 posted done for w1:pC -> 204"
	res, ts := parse(t, []byte(line+"\n"+other+"\n"+line+"\n"), opts())
	if res.Parsed != 3 || len(ts) != 3 {
		t.Fatalf("result %+v, want three rows", res)
	}
	if ts[0].ImportKey() != keyOf(line, 0) || ts[2].ImportKey() != keyOf(line, 1) || ts[1].ImportKey() != keyOf(other, 0) {
		t.Errorf("import keys %q, %q, %q", ts[0].ImportKey(), ts[1].ImportKey(), ts[2].ImportKey())
	}
}

func TestParseCRLF(t *testing.T) {
	line := "2026-09-10 18:28:00 posted done for w1:pC -> 204"
	_, lf := parse(t, []byte(line+"\n"), opts())
	_, crlf := parse(t, []byte(line+"\r\n"), opts())
	_, bare := parse(t, []byte(line), opts())
	if len(lf) != 1 || len(crlf) != 1 || len(bare) != 1 {
		t.Fatalf("rows %d, %d, %d; want one each", len(lf), len(crlf), len(bare))
	}
	for name, tr := range map[string]core.Transition{"\\r\\n": crlf[0], "no newline": bare[0]} {
		if tr.ImportKey() != keyOf(line, 0) || row(tr) != row(lf[0]) || tr.ObservedAt() != lf[0].ObservedAt() {
			t.Errorf("%s: %q %s, want %q %s", name, tr.ImportKey(), row(tr), keyOf(line, 0), row(lf[0]))
		}
	}
}

func TestParseLongLine(t *testing.T) {
	good := "2026-09-10 18:28:00 posted done for w1:pC -> 204"
	long := "2026-09-10 18:28:00 posted done for w1:p" + strings.Repeat("x", maxLine) + " -> 204"
	res, ts := parse(t, []byte(good+"\n"+long+"\n"+good+"\n"+long), opts())
	if res != (Result{Parsed: 2, Unparsed: 2}) || len(ts) != 2 {
		t.Fatalf("result %+v with %d rows, want 2 parsed and 2 unparsed", res, len(ts))
	}
	if ts[0].ImportKey() != keyOf(good, 0) || ts[1].ImportKey() != keyOf(good, 1) {
		t.Errorf("import keys %q, %q", ts[0].ImportKey(), ts[1].ImportKey())
	}
}

func TestParseKeyHasHostAndServer(t *testing.T) {
	line := []byte("2026-09-10 18:28:00 posted done for w1:pC -> 204\n")
	keys := map[string]bool{}
	for _, hs := range [][2]string{{"mac", "default"}, {"other", "default"}, {"mac", "work"}, {"ma", "cdefault"}} {
		_, ts := parse(t, line, Options{Host: hs[0], Server: hs[1], Location: zone})
		if len(ts) != 1 {
			t.Fatalf("%v: %d rows", hs, len(ts))
		}
		keys[ts[0].ImportKey()] = true
	}
	if len(keys) != 4 {
		t.Errorf("%d distinct keys for four Host and Server pairs, want 4", len(keys))
	}
}

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2026-10-19", zone)
	if err != nil || !d.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, zone)) {
		t.Errorf("ParseDate = %v, %v", d, err)
	}
	d, err = ParseDate("2026-10-19", nil)
	if err != nil || !d.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)) {
		t.Errorf("ParseDate with no zone = %v, %v; want local midnight", d, err)
	}
	for _, bad := range []string{"", "2026-10-32", "19-10-2026", "2026-10-19T00:00:00", "today"} {
		if _, err := ParseDate(bad, nil); err == nil {
			t.Errorf("ParseDate(%q) succeeded", bad)
		}
	}
}

// loadConfig is what serve would resolve with --host mac and the given data
// directory, in a hermetic environment.
func loadConfig(t *testing.T, dataDir string) config.Config {
	t.Helper()
	host, data := "mac", dataDir
	cfg, err := config.Load(config.Flags{Host: &host, DataDir: &data},
		config.Env{Home: "/nonexistent/home", Hostname: "other.example", Vars: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func openStore(t *testing.T, cfg config.Config) *store.Store {
	t.Helper()
	st, err := store.Open(cfg.Database())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestImportRerunAndGrownLog(t *testing.T) {
	cfg := loadConfig(t, filepath.Join(t.TempDir(), "data"))
	st := openStore(t, cfg)
	ctx := context.Background()
	b := fixture(t)
	ls := lines(b)
	since := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	head := []byte(strings.Join(ls[:15], "\n") + "\n")

	s, err := Import(ctx, st, bytes.NewReader(head), cfg, since, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Summary{Result: Result{Parsed: 15, Spurious: 10}, Added: 15}); s != want {
		t.Errorf("first import %+v, want %+v", s, want)
	}
	if got, want := s.String(), "imported 15 of 15 entries (0 already present, 0 older than retention, 10 flagged possibly spurious, 0 unparsed)"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}

	s, err = Import(ctx, st, bytes.NewReader(head), cfg, since, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Added != 0 || s.Duplicates != 15 || s.Spurious != 0 {
		t.Errorf("re-run %+v, want 0 added, 15 duplicates and none flagged", s)
	}

	s, err = Import(ctx, st, bytes.NewReader(b), cfg, since, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Added != 6 || s.Duplicates != 15 || s.Parsed != 21 || s.Spurious != 2 {
		t.Errorf("grown log %+v, want 6 added (2 flagged) and 15 duplicates of 21", s)
	}

	// The rows carry the Host and Server config.Load resolved, at epoch 0.
	if cfg.Host() != "mac" {
		t.Fatalf("config host %q", cfg.Host())
	}
	h, err := st.History(ctx, core.AgentKey{Host: cfg.Host(), Server: cfg.Server(), Epoch: 0, PaneID: "w1:pE"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range h {
		got = append(got, fmt.Sprintf("%s %s %v", tr.New(), tr.Profile(), tr.PossiblySpurious()))
	}
	if g, want := strings.Join(got, "; "), "done  true; done  true; blocked  false; done personal true; blocked personal false"; g != want {
		t.Errorf("w1:pE history %q, want %q", g, want)
	}
}

func TestImportRetention(t *testing.T) {
	cfg := loadConfig(t, filepath.Join(t.TempDir(), "data"))
	st := openStore(t, cfg)
	// 90 days (the default) after the fixture's first day, its 2026-09-10
	// entries are older than Retention.
	now := time.Date(2026, 12, 10, 6, 0, 0, 0, time.Local)
	s, err := Import(context.Background(), st, bytes.NewReader(fixture(t)), cfg, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Old != 14 || s.Added != 7 || s.Parsed != 21 {
		t.Errorf("summary %+v, want 14 old and 7 added of 21", s)
	}
}
