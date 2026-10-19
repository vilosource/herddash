package importer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/vilosource/herddash/internal/config"
	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/store"
)

// KeyPrefix begins every import key this package writes.
const KeyPrefix = "plugin-log:"

// The layouts of an entry's time and of the --herdr-0.9.2-since date.
const (
	timeLayout = "2006-01-02 15:04:05"
	DateLayout = "2006-01-02"
)

// maxLine bounds one log line; the longest real entry is under 200 bytes. A
// longer line is counted unparsed.
const maxLine = 1 << 20

// Options says how to read the log.
type Options struct {
	// Host and Server key every row, as serve would key this Host's Agents.
	Host, Server string
	// Since is the instant the Host reached herdr 0.9.2: a done entry before
	// it is flagged possibly spurious.
	Since time.Time
	// Cutoff is the Retention cutoff, in UTC Unix ms: an entry observed
	// before it is skipped.
	Cutoff int64
	// Location is the zone the log's times are read in; nil means time.Local.
	Location *time.Location
}

// Result counts what Parse read. Parsed counts every entry it understood,
// Old those of them skipped as older than Retention, Spurious those of the
// rest flagged possibly spurious, and Unparsed every line it did not
// understand.
type Result struct {
	Parsed, Old, Spurious, Unparsed int
}

var postedForm = regexp.MustCompile(`^posted (\S+) for (\S+) -> \d+$`)

const sentPrefix = "no url configured; would have sent "

// sent is the JSON object of a "would have sent" entry. Profile is a pointer
// because it may be absent or null, both unknown.
type sent struct {
	Agent   string  `json:"agent"`
	Profile *string `json:"profile"`
	Status  *string `json:"status"`
	PaneID  *string `json:"pane_id"`
}

// ParseDate reads a --herdr-0.9.2-since date, YYYY-MM-DD, as local midnight.
func ParseDate(s string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	t, err := time.ParseInLocation(DateLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date in the form YYYY-MM-DD", s)
	}
	return t, nil
}

// Parse reads the plugin log from r and returns what it counted and the
// backfill Transitions it kept, in the log's order. Its error is only a
// failure to read r.
func Parse(r io.Reader, opts Options) (Result, []core.Transition, error) {
	loc := opts.Location
	if loc == nil {
		loc = time.Local
	}
	var res Result
	var out []core.Transition
	seen := map[string]int{}
	br := bufio.NewReader(r)
	for {
		raw, tooLong, err := readLine(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, nil, fmt.Errorf("importer: read log: %w", err)
		}
		if tooLong {
			res.Unparsed++
			continue
		}
		h := sha256.New()
		h.Write([]byte(opts.Host + "\x00" + opts.Server + "\x00"))
		h.Write(raw)
		digest := hex.EncodeToString(h.Sum(nil))
		occurrence := seen[digest]
		seen[digest]++

		in, ok := parseLine(string(raw), loc)
		if !ok {
			res.Unparsed++
			continue
		}
		in.Key.Host, in.Key.Server = opts.Host, opts.Server
		in.ImportKey = fmt.Sprintf("%s%s#%d", KeyPrefix, digest, occurrence)
		at := in.ObservedAt
		in.PossiblySpurious = in.New == core.StatusDone && at < opts.Since.UnixMilli()
		t, err := core.NewTransition(in)
		if err != nil {
			res.Unparsed++
			continue
		}
		res.Parsed++
		if at < opts.Cutoff {
			res.Old++
			continue
		}
		if t.PossiblySpurious() {
			res.Spurious++
		}
		out = append(out, t)
	}
	return res, out, nil
}

// readLine returns the next line without its "\n" or "\r\n", or io.EOF when
// there is none. A line longer than maxLine is read to its end and discarded,
// reported as tooLong.
func readLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	for {
		part, isPrefix, err := br.ReadLine()
		if err != nil {
			if err == io.EOF && (line != nil || tooLong) {
				return line, tooLong, nil
			}
			return nil, false, err
		}
		if !tooLong {
			line = append(line, part...)
			if len(line) > maxLine {
				line, tooLong = nil, true
			}
		}
		if !isPrefix {
			if line == nil && !tooLong {
				line = []byte{}
			}
			return line, tooLong, nil
		}
	}
}

// parseLine reads one entry into everything but the Host, Server, import key
// and flag, or reports false when the line is neither form.
func parseLine(line string, loc *time.Location) (core.TransitionInput, bool) {
	if len(line) < len(timeLayout)+2 || line[len(timeLayout)] != ' ' {
		return core.TransitionInput{}, false
	}
	at, err := time.ParseInLocation(timeLayout, line[:len(timeLayout)], loc)
	if err != nil {
		return core.TransitionInput{}, false
	}
	in := core.TransitionInput{
		Key:        core.AgentKey{Epoch: core.BackfillEpoch},
		ObservedAt: at.UnixMilli(),
		Provenance: core.ProvenanceBackfill,
	}
	msg := line[len(timeLayout)+1:]
	if m := postedForm.FindStringSubmatch(msg); m != nil {
		in.New, in.Key.PaneID = core.Status(m[1]), m[2]
		return in, true
	}
	body, ok := strings.CutPrefix(msg, sentPrefix)
	if !ok {
		return core.TransitionInput{}, false
	}
	var s sent
	if err := json.Unmarshal([]byte(body), &s); err != nil || s.Status == nil || s.PaneID == nil {
		return core.TransitionInput{}, false
	}
	in.New, in.Key.PaneID, in.AgentProgram = core.Status(*s.Status), *s.PaneID, core.AgentProgram(s.Agent)
	if s.Profile != nil {
		in.Profile = *s.Profile
	}
	return in, true
}

// Summary is one import's outcome: what Parse counted, how many rows were
// added, and how many of the kept entries were already in the record. Its
// Spurious counts only the flagged rows among those added.
type Summary struct {
	Result
	Added, Duplicates int
}

// String is the command's one summary line.
func (s Summary) String() string {
	return fmt.Sprintf("imported %d of %d entries (%d already present, %d older than retention, %d flagged possibly spurious, %d unparsed)",
		s.Added, s.Parsed, s.Duplicates, s.Old, s.Spurious, s.Unparsed)
}

// Import parses the log from r and writes its entries to st in one
// transaction. Host, Server and Retention come from cfg; since is when the
// Host reached herdr 0.9.2 and now is the clock, for the Retention cutoff.
// Nothing is written when reading or writing fails.
func Import(ctx context.Context, st *store.Store, r io.Reader, cfg config.Config, since, now time.Time) (Summary, error) {
	// The same cutoff as the engine's Retention sweep.
	cutoff := now.UnixMilli() - int64(cfg.RetentionDays())*int64(24*time.Hour/time.Millisecond)
	res, ts, err := Parse(r, Options{Host: cfg.Host(), Server: cfg.Server(), Since: since, Cutoff: cutoff})
	if err != nil {
		return Summary{}, err
	}
	added, flagged, err := st.AppendBackfill(ctx, ts)
	if err != nil {
		return Summary{}, err
	}
	// The summary's spurious count is of the rows this run added.
	res.Spurious = flagged
	return Summary{Result: res, Added: added, Duplicates: len(ts) - added}, nil
}
