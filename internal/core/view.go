package core

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// contextLineRules is the Context Line rule table (FR-2a), keyed by Agent
// Program. A rule turns the Pane Title the program set, already cleaned to one
// line, into the Context Line. A program with no entry gets the global
// default, "".
//
// Adding a rule for a new program is one entry here, with a comment saying how
// that program titles its pane. Nothing outside core changes: the API and the
// Board show whatever ContextLine returns.
var contextLineRules = map[AgentProgram]func(title string) string{
	// Claude Code titles its pane with the session's name once it has one, and
	// with the placeholder "Claude Code" until then. The name is the Context
	// Line; the placeholder, like an empty title, says nothing.
	//
	// herdr's stripped pane title is what the client maps (internal/herdr/map.go);
	// on 2026-10-19 all 12 Claude Code agents on the live server had a session
	// name there. The raw terminal title starts with Claude Code's status glyph
	// and a space ("✳ " idle, "◐ " working), so as a fallback for when only the
	// raw title is available, exactly those glyphs are dropped as a prefix, and
	// a title that is only a glyph says nothing. Any other leading rune, such as
	// a session name's "-", "#" or emoji, is kept.
	"claude": func(title string) string {
		for _, glyph := range claudeGlyphs {
			if title == glyph {
				return ""
			}
			if rest, ok := strings.CutPrefix(title, glyph+" "); ok {
				title = strings.TrimSpace(rest)
				break
			}
		}
		if title == "Claude Code" {
			return ""
		}
		return title
	},
}

// claudeGlyphs are the status glyphs Claude Code puts before its raw terminal
// title: "✳" idle, "◐" working.
var claudeGlyphs = []string{"✳", "◐"}

// ContextLine is the one line a Box shows about what the Agent is doing,
// derived from its Pane Title by its Agent Program's rule. The title is first
// made one line: control characters other than whitespace, and Unicode
// Bidi_Control characters (U+202E and the rest, which would let a pane's
// process reorder the Box's text), are dropped, and every whitespace run
// becomes one space, trimmed at both ends. Other format characters, such as
// the zero-width joiner in emoji sequences, are kept. It is "" for a program
// with no rule.
func ContextLine(program AgentProgram, paneTitle string) string {
	rule, ok := contextLineRules[program]
	if !ok {
		return ""
	}
	return rule(oneLine(paneTitle))
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// ShortenDir replaces the home prefix of dir with "~" when dir is home or lies
// under it. Any other dir, and any dir when home is "", is returned unchanged.
func ShortenDir(dir, home string) string {
	if home == "" {
		return dir
	}
	home = strings.TrimSuffix(home, "/")
	if home == "" {
		// home was "/": every absolute path is under it, which says nothing.
		return dir
	}
	if dir == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(dir, home+"/"); ok {
		return "~/" + rest
	}
	return dir
}

// AgentViewInput is everything NewAgentView needs to describe one Agent.
type AgentViewInput struct {
	Key          AgentKey
	AgentProgram AgentProgram
	Profile      string
	// Status is the Agent's current Status as herdr reports it.
	Status Status
	// Fold is the Agent's folded record: its class, when that class began and
	// with which Provenance.
	Fold    FoldResult
	Context PaneContext
	// Home is the Daemon user's home directory, for shortening the project
	// directory; "" shortens nothing.
	Home string
	// Background is set when the Agent's process has a child its program's
	// BackgroundRule names a background task (HasBackground). The view shows
	// it only while ChecksBackground holds for the Status and the class is
	// finished or working.
	Background bool
	// Transcript is the Agent's background work and latest session recap as
	// its Claude Code transcript shows them (ADR-0009, ADR-0010); the zero
	// value when it is not read.
	Transcript BackgroundTasks
	// Seen is set when the operator acknowledged the Agent's current finish,
	// as Seen decides from the record (ADR-0014). The view shows it only
	// while the class is finished.
	Seen bool
}

// AgentView is how every surface (the API, the event stream and the Board)
// describes one Agent (AD-1). It is built only by NewAgentView, so the
// description is computed once, in core.
type AgentView struct {
	Key          AgentKey     `json:"key"`
	AgentProgram AgentProgram `json:"agent_program"`
	Profile      string       `json:"profile"`
	Heading      string       `json:"heading"`
	Directory    string       `json:"directory"`
	Workspace    string       `json:"workspace"`
	Tab          string       `json:"tab"`
	// PanePosition is where the pane sits in its tab ("left", "top-right");
	// "" when the tab has one pane. It is part of the Box's location, with
	// the workspace and tab.
	PanePosition string         `json:"pane_position"`
	Status       Status         `json:"status"`
	Class        AttentionClass `json:"attention_class"`
	// WaitingSince is when the current class began (UTC Unix ms); 0 when not
	// known, and then omitted from JSON.
	WaitingSince int64 `json:"waiting_since,omitempty"`
	// WaitingExact is set when WaitingSince is exact rather than a lower bound.
	WaitingExact bool   `json:"waiting_exact"`
	PaneTitle    string `json:"pane_title"`
	// ContextLine is never omitted: an empty one is sent as "".
	ContextLine string `json:"context_line"`
	// Recap is Claude Code's latest session recap (ADR-0010), read only when
	// the operator turned recaps on, and cleared when the Agent's next turn
	// starts. It is set only on a Finished, Idle or Background Box; a
	// Working, Needs you or Unknown one gets "", since herdr reports a new
	// turn before the next transcript read (operator decision 2026-10-22).
	// The Board shows it in place of the Context Line. Never omitted: ""
	// when there is none. It changes no other value.
	Recap string `json:"recap"`
	// Background is set when the Agent is waiting on background work: its
	// Status is done or idle, or working with its main turn over (ADR-0009),
	// and it has background tasks open. The class, the Waiting Time and the
	// record are unchanged by it; only the Board's kind and the sort key's
	// class rank are.
	Background bool `json:"background"`
	// BackgroundTasks is how many background tasks the Agent has open
	// (ADR-0009): its transcript's open tasks, plus one for a live tool
	// shell the process check finds that no open command accounts for. It
	// is shown on a Working Box ("WORKING · 2 bg") and a Background one,
	// and is 0 for Needs you and Unknown.
	BackgroundTasks int `json:"background_tasks"`
	// Seen is set when the operator acknowledged this finish (ADR-0014): only
	// with class finished, which it leaves unchanged. It moves the Box down
	// among the Idle ones (the sort key's class rank 2) unless the Box is
	// Background, which still takes precedence. Never omitted.
	Seen    bool    `json:"seen"`
	SortKey SortKey `json:"sort_key"`
}

// NewAgentView builds the view of one Agent. The Attention Class comes from
// the fold when it is known, otherwise from ClassOf on the current Status.
func NewAgentView(in AgentViewInput) AgentView {
	class := in.Fold.Class
	if !in.Fold.Known {
		class = ClassOf(in.Status, in.Fold.EverWorking)
	}
	tasks, background := backgroundOf(in, class)
	seen := in.Seen && class == ClassFinished
	var since int64
	var exact bool
	if in.Fold.Known && in.Fold.Since > 0 {
		since = in.Fold.Since
		_, exact = WaitingTime(in.Fold, in.Fold.Since)
	}
	v := AgentView{
		Key:          in.Key,
		AgentProgram: in.AgentProgram,
		Profile:      in.Profile,
		Heading:      Heading(in.Profile, in.AgentProgram),
		Directory:    ShortenDir(in.Context.ProjectDir, in.Home),
		Workspace:    in.Context.WorkspaceLabel,
		Tab:          in.Context.TabLabel,
		PanePosition: in.Context.PanePosition,
		Status:       in.Status,
		Class:        class,
		WaitingSince: since,
		WaitingExact: exact,
		PaneTitle:    in.Context.PaneTitle,
		ContextLine:  ContextLine(in.AgentProgram, in.Context.PaneTitle),
		Recap:        shownRecap(in, class, background),
		Background:   background,

		BackgroundTasks: tasks,
		Seen:            seen,
	}
	v.SortKey = newSortKey(class, in.Status, background, seen, in.Context.WorkspacePosition, in.Context.WorkspaceID, since, in.Key)
	return v
}

// backgroundOf is an Agent's open background tasks and whether its Box is
// Background (ADR-0009). The count is the transcript's open tasks, plus one
// when the process check (shown only while ChecksBackground holds) finds a
// tool shell and no open command accounts for it. With any open:
//   - Status done or idle, class finished or working: Background;
//   - Status working and the main turn ended: Background, as an idle one;
//   - Status working otherwise: Working, with the count shown.
//
// Needs you and Unknown never change: no count, never Background.
func backgroundOf(in AgentViewInput, class AttentionClass) (int, bool) {
	if class != ClassFinished && class != ClassWorking {
		return 0, false
	}
	switch in.Status {
	case StatusDone, StatusIdle, StatusWorking:
	default:
		return 0, false
	}
	n := in.Transcript.Count()
	if in.Background && ChecksBackground(in.AgentProgram, in.Status) && !in.Transcript.OpenCommand() {
		n++
	}
	switch {
	case n == 0:
		return 0, false
	case in.Status == StatusWorking:
		return n, in.Transcript.TurnEnded()
	}
	return n, true
}

// shownRecap is the Agent's recap when its Box is Finished, Idle or Background
// (ADR-0010), "" otherwise.
func shownRecap(in AgentViewInput, class AttentionClass, background bool) string {
	if background || class == ClassFinished || (class == ClassWorking && in.Status != StatusWorking) {
		return in.Transcript.Recap()
	}
	return ""
}

// SortKey is where an Agent sorts on the Board (FR-3):
// [zone, group, class_rank, start, agent_key], compared element by element.
//
//   - Zone 0 holds only needs_you, flat across workspaces; zone 1 holds every
//     other class, grouped by workspace (operator decision 2026-10-21).
//   - Group is 0 in zone 0. In zone 1 it is the workspace's position in
//     herdr's order. A workspace herdr did not position (position 0) gets its
//     own group, UnpositionedGroup of its workspace id, above every real
//     position and below SortUnknown, so different unpositioned workspaces
//     practically never share a header (only a hash collision, negligible,
//     would merge two); a pane with no workspace id at all sorts last as
//     SortUnknown.
//   - ClassRank is needs_you 0; then, inside a workspace group, finished 1,
//     Idle (class working with a Status other than working) 2, and a seen
//     finished Agent with it (ADR-0014), Background
//     (finished, Idle, or Working with its main turn over, waiting on
//     background work; ADR-0009) 3, Working (Status
//     working) 4, unknown 5: what to deal with first comes first (operator
//     decision 2026-10-21).
//   - Start orders a class by WaitingSince. In zone 0 it is WaitingSince, so
//     the longest wait comes first. In zone 1 it is its negation, so the most
//     recent comes first. An unknown one sorts after every known one, as
//     SortUnknown.
//   - Key breaks ties: host, server, epoch (numerically), then pane id.
//
// It serialises as a JSON array of five values, the last being the key
// string "host/server/epoch/pane_id".
type SortKey struct {
	Zone      int
	Group     int64
	ClassRank int
	Start     int64
	Key       AgentKey
}

// SortUnknown is the sort key's value for an unknown group or start: 2^53-1,
// JavaScript's Number.MAX_SAFE_INTEGER, so the Board compares it exactly.
const SortUnknown int64 = 1<<53 - 1

// unpositionedBase is the first group number of a workspace herdr did not
// position: 2^52, far above any real position.
const unpositionedBase int64 = 1 << 52

// UnpositionedGroup is the zone-1 group of a workspace herdr did not
// position: 2^52 plus the workspace id's 64-bit FNV-1a hash modulo 2^52-1, so
// it lies in [2^52, 2^53-2], above every real position and below SortUnknown,
// and stays exact as a JavaScript number. It is hashed, not ranked, so one
// Agent's group never depends on other workspaces; unpositioned groups order
// among themselves by hash, stable and arbitrary.
func UnpositionedGroup(workspaceID string) int64 {
	h := fnv.New64a()
	h.Write([]byte(workspaceID))
	return unpositionedBase + int64(h.Sum64()%uint64(unpositionedBase-1))
}

func newSortKey(class AttentionClass, status Status, background, seen bool, workspacePosition int, workspaceID string, since int64, key AgentKey) SortKey {
	k := SortKey{Key: key, Start: since}
	switch {
	case class == ClassNeedsYou:
		k.ClassRank = 0
	case background:
		k.ClassRank = 3
	case seen:
		k.ClassRank = 2 // with Idle, newest first (ADR-0014)
	case class == ClassFinished:
		k.ClassRank = 1
	case class == ClassWorking && status == StatusWorking:
		k.ClassRank = 4
	case class == ClassWorking:
		k.ClassRank = 2
	default:
		k.ClassRank = 5
	}
	if k.ClassRank > 0 {
		k.Zone = 1
		switch {
		case workspacePosition > 0:
			k.Group = int64(workspacePosition)
		case workspaceID != "":
			k.Group = UnpositionedGroup(workspaceID)
		default:
			k.Group = SortUnknown
		}
		k.Start = -since // the most recent first
	}
	if since == 0 {
		k.Start = SortUnknown
	}
	return k
}

// Less reports whether k sorts before o.
func (k SortKey) Less(o SortKey) bool {
	return k.compare(o) < 0
}

func (k SortKey) compare(o SortKey) int {
	if c := cmpInt(k.Zone, o.Zone); c != 0 {
		return c
	}
	if c := cmpInt(k.Group, o.Group); c != 0 {
		return c
	}
	if c := cmpInt(k.ClassRank, o.ClassRank); c != 0 {
		return c
	}
	if c := cmpInt(k.Start, o.Start); c != 0 {
		return c
	}
	if c := strings.Compare(k.Key.Host, o.Key.Host); c != 0 {
		return c
	}
	if c := strings.Compare(k.Key.Server, o.Key.Server); c != 0 {
		return c
	}
	if c := cmpInt(k.Key.Epoch, o.Key.Epoch); c != 0 {
		return c
	}
	return strings.Compare(k.Key.PaneID, o.Key.PaneID)
}

func cmpInt[T int | int64 | uint64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// KeyString is an AgentKey as one string, "host/server/epoch/pane_id".
func KeyString(k AgentKey) string {
	return k.Host + "/" + k.Server + "/" + strconv.FormatUint(k.Epoch, 10) + "/" + k.PaneID
}

// MarshalJSON writes the key as [zone, group, class_rank, start, "key"].
func (k SortKey) MarshalJSON() ([]byte, error) {
	return json.Marshal([5]any{k.Zone, k.Group, k.ClassRank, k.Start, KeyString(k.Key)})
}

// SortViews sorts views into Board order, in place.
func SortViews(views []AgentView) {
	slices.SortFunc(views, func(a, b AgentView) int { return a.SortKey.compare(b.SortKey) })
}

// WorkspaceHeaders returns the indexes of sorted views at which a zone-1
// workspace group starts, the first zone-1 view included. The Board draws a
// workspace header before each.
func WorkspaceHeaders(views []AgentView) []int {
	headers := []int{}
	for i, v := range views {
		if v.SortKey.Zone != 1 {
			continue
		}
		if i == 0 || views[i-1].SortKey.Zone != 1 || views[i-1].SortKey.Group != v.SortKey.Group {
			headers = append(headers, i)
		}
	}
	return headers
}

// ConnectionTruth is what every surface says about the Daemon's connection to
// herdr: whether it is observing, when it last read herdr's state, and the
// version gate's outcome (FR-16).
type ConnectionTruth struct {
	// Connected is set from a connection's first applied snapshot until the
	// connection is lost.
	Connected bool `json:"connected"`
	// LastSnapshotAt is when the latest snapshot was applied (UTC Unix ms);
	// 0 before any, and then omitted.
	LastSnapshotAt int64 `json:"last_snapshot_at,omitempty"`
	// HerdrVersion is the version herdr's ping returned at the latest gate
	// decision; omitted when empty.
	HerdrVersion string `json:"herdr_version,omitempty"`
	// HerdrMinimum is the oldest herdr herddash supports, HerdrMinimum.
	HerdrMinimum string `json:"herdr_minimum"`
	// Gate is the latest gate decision; omitted before the first.
	Gate Gate `json:"gate,omitempty"`
	// ResyncGuaranteed is DecideGate's verdict on the resync guarantee.
	ResyncGuaranteed bool `json:"resync_guaranteed"`
}
