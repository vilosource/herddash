package core

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// name there. The raw terminal title starts with a status glyph and a space
	// ("✳ " idle, "◐ " working), so as a fallback for when only the raw title is
	// available, one leading rune that is neither a letter nor a digit, followed
	// by a space, is dropped.
	"claude": func(title string) string {
		if r, size := utf8.DecodeRuneInString(title); size > 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			strings.HasPrefix(title[size:], " ") {
			title = strings.TrimSpace(title[size:])
		}
		if title == "Claude Code" {
			return ""
		}
		return title
	},
}

// ContextLine is the one line a Box shows about what the Agent is doing,
// derived from its Pane Title by its Agent Program's rule. The title is first
// made one line: control characters other than whitespace are dropped and
// every whitespace run becomes one space, trimmed at both ends. It is "" for a
// program with no rule.
func ContextLine(program AgentProgram, paneTitle string) string {
	rule, ok := contextLineRules[program]
	if !ok {
		return ""
	}
	return rule(oneLine(paneTitle))
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
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
	ContextLine string  `json:"context_line"`
	SortKey     SortKey `json:"sort_key"`
}

// NewAgentView builds the view of one Agent. The Attention Class comes from
// the fold when it is known, otherwise from ClassOf on the current Status.
func NewAgentView(in AgentViewInput) AgentView {
	class := in.Fold.Class
	if !in.Fold.Known {
		class = ClassOf(in.Status, in.Fold.EverWorking)
	}
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
	}
	v.SortKey = newSortKey(class, in.Status, in.Context.WorkspacePosition, since, in.Key)
	return v
}

// SortKey is where an Agent sorts on the Board (FR-3):
// [zone, group, class_rank, start, agent_key], compared element by element.
//
//   - Zone 0 holds only needs_you, flat across workspaces; zone 1 holds every
//     other class, grouped by workspace (operator decision 2026-10-21).
//   - Group is 0 in zone 0. In zone 1 it is the workspace's position in
//     herdr's order, with an unknown position (0) sorting last as
//     SortUnknown.
//   - ClassRank is needs_you 0; then, inside a workspace group, Working (class
//     working with Status working) 1, finished 2, Idle (class working with any
//     other Status) 3, unknown 4.
//   - Start is WaitingSince, so the longest wait comes first; an unknown one
//     sorts after every known one, as SortUnknown.
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

func newSortKey(class AttentionClass, status Status, workspacePosition int, since int64, key AgentKey) SortKey {
	k := SortKey{Key: key, Start: since}
	switch class {
	case ClassNeedsYou:
		k.ClassRank = 0
	case ClassWorking:
		k.ClassRank = 1
		if status != StatusWorking {
			k.ClassRank = 3
		}
	case ClassFinished:
		k.ClassRank = 2
	default:
		k.ClassRank = 4
	}
	if k.ClassRank > 0 {
		k.Zone = 1
		k.Group = int64(workspacePosition)
		if k.Group <= 0 {
			k.Group = SortUnknown
		}
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
