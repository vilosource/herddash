//go:build golden

package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGoldenUsage is the golden replay (ADR-0017, Story 8.2), run by hand:
//
//	go test -tags golden -run TestGoldenUsage ./internal/core -v
//
// It replays every *.jsonl under $HOME/.claude*/projects, or under each
// directory of HERDDASH_GOLDEN_DIRS (a path list), through the decoder and one
// UsageLedger per file, keeping the first Response per key across files, and
// compares that with an independent count over the same lines: per key
// across files, the usage record with the largest output_tokens (the later
// on a tie), with no ledger. Output never decreases within a Response, so
// that is its final record, even when a forked subagent's file starts with
// an early copy of its parent's Response (seen once on 2026-11-11: same ids
// and timestamp, output 16 against 1,088, no stop reason). The two must
// differ by exactly the Responses still pending at the end of a file and
// completed in none. It prints counts and totals only, never a path or any
// text.
func TestGoldenUsage(t *testing.T) {
	dirs := goldenDirs(t)
	if len(dirs) == 0 {
		t.Skip("no transcript directories")
	}
	var files []string
	// unreadable counts walk errors and files that failed to open;
	// partReads counts reads that failed part-way, whose earlier lines were
	// already counted.
	var unreadable, partReads int
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				unreadable++
				return nil
			}
			if !e.IsDir() && strings.HasSuffix(p, ".jsonl") {
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)

	ledger := map[responseKey]Response{}
	independent := map[responseKey]Usage{}
	pendingAtEOF := map[responseKey]bool{}
	// keyFiles is how many files hold a record of each key, lastFile the
	// last file seen with one.
	keyFiles, lastFile := map[responseKey]int{}, map[responseKey]int{}
	var nFiles, lines, markers, noKey, unknown, stopped, superseded int
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			unreadable++
			continue
		}
		nFiles++
		var g UsageLedger
		r := bufio.NewReaderSize(f, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				lines++
				l, _ := DecodeTranscriptLine(line, false, true)
				switch l.UsageIssue {
				case UsageNoKey:
					noKey++
				case UsageUnknownShape:
					unknown++
				}
				if l.Usage != (UsageMarker{}) {
					markers++
					for _, resp := range g.Add(l.Usage) {
						k := responseKey{resp.MessageID, resp.RequestID}
						if _, seen := ledger[k]; !seen {
							ledger[k] = resp
						}
					}
				}
				if k, u, ok := independentUsage(line); ok {
					if old, seen := independent[k]; !seen || u.Output >= old.Output {
						independent[k] = u
					}
					if n, seen := lastFile[k]; !seen || n != nFiles {
						keyFiles[k]++
						lastFile[k] = nFiles
					}
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					partReads++
				}
				break
			}
		}
		_ = f.Close()
		if g.Pending() {
			pendingAtEOF[keyOf(g.pending)] = true
		}
	}

	var ledgerTotal, independentTotal, pendingTotal goldenTotals
	for _, r := range ledger {
		ledgerTotal.add(r.Usage)
		if r.Stopped {
			stopped++
		} else {
			superseded++
		}
	}
	missing, mismatched, mismatchedSpread, unexplained, spread := 0, 0, 0, 0, 0
	for _, n := range keyFiles {
		if n > 1 {
			spread++
		}
	}
	for k, r := range ledger {
		u, ok := independent[k]
		switch {
		case !ok:
			missing++
		case u.Input != r.Input || u.Output != r.Output || u.CacheRead != r.CacheRead ||
			u.CacheWrite5m != r.CacheWrite5m+r.CacheWrite1h:
			mismatched++
			if keyFiles[k] > 1 {
				mismatchedSpread++
			}
		}
	}
	pendingOnly := 0
	for k, u := range independent {
		independentTotal.add(u)
		if _, ok := ledger[k]; ok {
			continue
		}
		if pendingAtEOF[k] {
			pendingOnly++
			pendingTotal.add(u)
		} else {
			unexplained++
		}
	}

	t.Logf("files %d (unreadable %d, read failed part-way %d), lines %d, usage markers %d, no key %d, unknown shape %d",
		nFiles, unreadable, partReads, lines, markers, noKey, unknown)
	t.Logf("ledger: %d Responses (%d stopped, %d superseded); independent: %d keys; pending at end of file: %d (%d completed in no file)",
		len(ledger), stopped, superseded, len(independent), len(pendingAtEOF), pendingOnly)
	t.Logf("ledger totals:      %s", ledgerTotal)
	t.Logf("independent totals: %s", independentTotal)
	t.Logf("pending-only:       %s", pendingTotal)
	t.Logf("keys with records in several files %d", spread)
	t.Logf("ledger keys missing from the independent count %d, values differing %d (%d of them with records in several files), independent keys unexplained %d",
		missing, mismatched, mismatchedSpread, unexplained)

	if len(ledger) == 0 {
		t.Fatal("no Responses: nothing was replayed")
	}
	if unreadable != 0 || partReads != 0 {
		t.Errorf("%d unreadable, %d read failed part-way: the replay is incomplete", unreadable, partReads)
	}
	if missing != 0 || mismatched != 0 || unexplained != 0 {
		t.Error("the ledger and the independent count disagree beyond the Responses pending at end of file")
	}
	want := independentTotal
	want.sub(pendingTotal)
	if len(independent)-pendingOnly != len(ledger) || want != ledgerTotal.withoutSplit() {
		t.Errorf("independent minus pending-only %s (%d keys), ledger %s (%d)",
			want, len(independent)-pendingOnly, ledgerTotal.withoutSplit(), len(ledger))
	}
}

// goldenDirs is HERDDASH_GOLDEN_DIRS, or every $HOME/.claude*/projects.
func goldenDirs(t *testing.T) []string {
	if v := os.Getenv("HERDDASH_GOLDEN_DIRS"); v != "" {
		return filepath.SplitList(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := filepath.Glob(filepath.Join(home, ".claude*", "projects"))
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// independentUsage is the independent count's reading of one line, written
// apart from the decoder: an assistant record, not <synthetic>, with both
// ids and a usage object whose four counts decode as integers. Its cache
// writes are cache_creation_input_tokens, in CacheWrite5m.
func independentUsage(line []byte) (responseKey, Usage, bool) {
	var rec struct {
		Type      string `json:"type"`
		RequestID string `json:"requestId"`
		Message   struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage *struct {
				Input     int64 `json:"input_tokens"`
				Output    int64 `json:"output_tokens"`
				Creation  int64 `json:"cache_creation_input_tokens"`
				CacheRead int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.Message.Usage == nil ||
		rec.Message.Model == "<synthetic>" || rec.Message.ID == "" || rec.RequestID == "" {
		return responseKey{}, Usage{}, false
	}
	u := rec.Message.Usage
	return responseKey{rec.Message.ID, rec.RequestID},
		Usage{Input: u.Input, Output: u.Output, CacheWrite5m: u.Creation, CacheRead: u.CacheRead}, true
}

type goldenTotals struct {
	Responses                                       int64
	Input, Output, Write, Write5m, Write1h, ReadTok int64
}

func (g *goldenTotals) add(u Usage) {
	g.Responses++
	g.Input += u.Input
	g.Output += u.Output
	g.Write += u.CacheWrite5m + u.CacheWrite1h
	g.Write5m += u.CacheWrite5m
	g.Write1h += u.CacheWrite1h
	g.ReadTok += u.CacheRead
}

func (g *goldenTotals) sub(o goldenTotals) {
	g.Responses -= o.Responses
	g.Input -= o.Input
	g.Output -= o.Output
	g.Write -= o.Write
	g.ReadTok -= o.ReadTok
	g.Write5m, g.Write1h = 0, 0
}

// withoutSplit drops the 5m/1h split, which the independent count does not
// read, so the two compare on what both read.
func (g goldenTotals) withoutSplit() goldenTotals {
	g.Write5m, g.Write1h = 0, 0
	return g
}

func (g goldenTotals) String() string {
	b, _ := json.Marshal(g)
	return string(b)
}
