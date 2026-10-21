package herdr

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vilosource/herddash/internal/core"
	"github.com/vilosource/herddash/internal/herdr/gen"
)

// This file is the package edge (ADR-0006): wire types from gen/ in, core types
// out. Status strings pass through verbatim.

// mapSnapshot maps a session snapshot to its core form. A pane's agent comes from
// the snapshot's agent records, matched by pane id, since only those carry
// state_change_seq. A pane that names an agent but has no agent record falls back
// to the pane record's agent and Status, with no state_change_seq. An agent
// record whose pane is missing from the pane list is kept as a pane of its own
// rather than dropped.
//
// Each pane's context comes from its pane record, falling back to its agent
// record field by field, plus the labels of its workspace and tab, the
// workspace's position in the snapshot's workspace list, which is herdr's order,
// and the pane's position in its tab, from the tab's layout rectangles
// (core.PanePositions).
func mapSnapshot(s gen.SessionSnapshot) core.Snapshot {
	type workspace struct {
		label    string
		position int
	}
	workspaces := make(map[string]workspace, len(s.Workspaces))
	for i, w := range s.Workspaces {
		workspaces[w.WorkspaceID] = workspace{label: w.Label, position: i + 1}
	}
	tabs := make(map[string]string, len(s.Tabs))
	for _, t := range s.Tabs {
		tabs[t.TabID] = t.Label
	}
	positions := make(map[string]map[string]string, len(s.Layouts))
	for _, l := range s.Layouts {
		rects := make([]core.PaneRect, 0, len(l.Panes))
		for _, p := range l.Panes {
			rects = append(rects, core.PaneRect{PaneID: p.PaneID, X: int(p.Rect.X), Y: int(p.Rect.Y),
				Width: int(p.Rect.Width), Height: int(p.Rect.Height)})
		}
		positions[l.TabID] = core.PanePositions(rects)
	}
	paneContext := func(paneID, workspaceID, tabID, projectDir, paneTitle string) core.PaneContext {
		w := workspaces[workspaceID]
		return core.PaneContext{
			ProjectDir:        projectDir,
			WorkspaceLabel:    w.label,
			TabLabel:          tabs[tabID],
			WorkspacePosition: w.position,
			PaneTitle:         paneTitle,
			PanePosition:      positions[tabID][paneID],
		}
	}
	agentRecords := make(map[string]gen.AgentInfo, len(s.Agents))
	agents := make(map[string]*core.AgentObservation, len(s.Agents))
	for _, a := range s.Agents {
		agentRecords[a.PaneID] = a
		agents[a.PaneID] = &core.AgentObservation{
			AgentProgram:   program(a.Agent),
			Status:         core.Status(a.AgentStatus),
			StateChangeSeq: copyUint64(a.StateChangeSeq),
		}
	}
	out := core.Snapshot{HerdrVersion: s.Version, Panes: make([]core.PaneObservation, 0, len(s.Panes))}
	seen := make(map[string]bool, len(s.Panes))
	for _, p := range s.Panes {
		seen[p.PaneID] = true
		agent := agents[p.PaneID]
		if agent == nil && p.Agent != nil {
			agent = &core.AgentObservation{AgentProgram: program(p.Agent), Status: core.Status(p.AgentStatus)}
		}
		a := agentRecords[p.PaneID] // the zero value when there is none
		out.Panes = append(out.Panes, core.PaneObservation{
			PaneID:      p.PaneID,
			TerminalID:  p.TerminalID,
			WorkspaceID: p.WorkspaceID,
			TabID:       p.TabID,
			Context: paneContext(p.PaneID, p.WorkspaceID, p.TabID,
				first(p.ForegroundCWD, a.ForegroundCWD, p.CWD, a.CWD),
				first(p.TerminalTitleStripped, a.TerminalTitleStripped, p.TerminalTitle, a.TerminalTitle)),
			Agent: agent,
		})
	}
	for _, a := range s.Agents {
		if seen[a.PaneID] {
			continue
		}
		seen[a.PaneID] = true
		out.Panes = append(out.Panes, core.PaneObservation{
			PaneID:      a.PaneID,
			TerminalID:  a.TerminalID,
			WorkspaceID: a.WorkspaceID,
			TabID:       a.TabID,
			Context: paneContext(a.PaneID, a.WorkspaceID, a.TabID,
				first(a.ForegroundCWD, a.CWD), first(a.TerminalTitleStripped, a.TerminalTitle)),
			Agent: agents[a.PaneID],
		})
	}
	return out
}

// mapForegroundProcess picks the foreground process: the one whose pid is the
// foreground process group id, else the first listed.
func mapForegroundProcess(pi gen.PaneProcessInfo) (core.ForegroundProcess, error) {
	if len(pi.ForegroundProcesses) == 0 {
		return core.ForegroundProcess{}, ErrNoForegroundProcess
	}
	chosen := pi.ForegroundProcesses[0]
	if pi.ForegroundProcessGroupID != nil {
		for _, p := range pi.ForegroundProcesses {
			if p.PID == *pi.ForegroundProcessGroupID {
				chosen = p
				break
			}
		}
	}
	fp := core.ForegroundProcess{PID: int(chosen.PID), Name: chosen.Name}
	if chosen.Argv0 != nil {
		fp.Argv0 = *chosen.Argv0
	}
	return fp, nil
}

// errUnexpectedEvent is wrapped by the event decoders for a line that decodes as
// JSON but is not an event this subscription can deliver.
var errUnexpectedEvent = errors.New("unexpected event")

// decodeLifecycle decodes one line of the lifecycle subscription, whose envelope
// is {event, data} with underscored event names.
func decodeLifecycle(line []byte) (core.LifecycleEvent, error) {
	var env gen.EventEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return core.LifecycleEvent{}, fmt.Errorf("decode lifecycle envelope: %w", err)
	}
	if len(env.Data) == 0 {
		return core.LifecycleEvent{}, fmt.Errorf("%w: lifecycle line has no data", errUnexpectedEvent)
	}
	switch env.Event {
	case gen.EventKindPaneCreated:
		var d gen.EventDataPaneCreated
		if err := decodeData(env, &d); err != nil {
			return core.LifecycleEvent{}, err
		}
		return lifecycle(core.LifecycleEvent{Kind: core.LifecycleCreated, PaneID: d.Pane.PaneID,
			TerminalID: d.Pane.TerminalID, WorkspaceID: d.Pane.WorkspaceID, TabID: d.Pane.TabID})
	case gen.EventKindPaneClosed:
		var d gen.EventDataPaneClosed
		if err := decodeData(env, &d); err != nil {
			return core.LifecycleEvent{}, err
		}
		return lifecycle(core.LifecycleEvent{Kind: core.LifecycleClosed, PaneID: d.PaneID})
	case gen.EventKindPaneMoved:
		var d gen.EventDataPaneMoved
		if err := decodeData(env, &d); err != nil {
			return core.LifecycleEvent{}, err
		}
		return lifecycle(core.LifecycleEvent{Kind: core.LifecycleMoved, PaneID: d.Pane.PaneID, PreviousPaneID: d.PreviousPaneID,
			TerminalID: d.Pane.TerminalID, WorkspaceID: d.Pane.WorkspaceID, TabID: d.Pane.TabID})
	case gen.EventKindPaneExited:
		var d gen.EventDataPaneExited
		if err := decodeData(env, &d); err != nil {
			return core.LifecycleEvent{}, err
		}
		return lifecycle(core.LifecycleEvent{Kind: core.LifecycleExited, PaneID: d.PaneID})
	case gen.EventKindPaneAgentDetected:
		var d gen.EventDataPaneAgentDetected
		if err := decodeData(env, &d); err != nil {
			return core.LifecycleEvent{}, err
		}
		e := core.LifecycleEvent{Kind: core.LifecycleAgentDetected, PaneID: d.PaneID, AgentProgram: program(d.Agent)}
		if d.Released != nil && *d.Released {
			e.Kind = core.LifecycleAgentReleased
		}
		if d.FinalStatus != nil {
			e.FinalStatus = core.Status(*d.FinalStatus)
		}
		return lifecycle(e)
	case gen.EventKindWorkspaceRenamed, gen.EventKindWorkspaceMoved, gen.EventKindWorkspaceReordered,
		gen.EventKindTabRenamed, gen.EventKindTabMoved, gen.EventKindPaneUpdated, gen.EventKindLayoutUpdated:
		// Only a signal to read again: the snapshot is the truth for context, so
		// the payload is not decoded.
		return core.LifecycleEvent{Kind: core.LifecycleContextChanged}, nil
	}
	return core.LifecycleEvent{}, fmt.Errorf("%w: lifecycle event %q", errUnexpectedEvent, env.Event)
}

func decodeData(env gen.EventEnvelope, v any) error {
	if err := json.Unmarshal(env.Data, v); err != nil {
		return fmt.Errorf("decode %s: %w", env.Event, err)
	}
	return nil
}

// lifecycle rejects an event without a pane id, which nothing can act on.
func lifecycle(e core.LifecycleEvent) (core.LifecycleEvent, error) {
	if e.PaneID == "" {
		return core.LifecycleEvent{}, fmt.Errorf("%w: %s event has no pane id", errUnexpectedEvent, e.Kind)
	}
	return e, nil
}

// decodeStatus decodes one line of a pane's status subscription, whose envelope
// keeps the dotted event name.
func decodeStatus(line []byte) (core.StatusEvent, error) {
	var env gen.SubscriptionEventEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return core.StatusEvent{}, fmt.Errorf("decode status envelope: %w", err)
	}
	if env.Event != gen.SubscriptionEventKindPaneAgentStatusChanged || len(env.Data) == 0 {
		return core.StatusEvent{}, fmt.Errorf("%w: status event %q", errUnexpectedEvent, env.Event)
	}
	var d gen.PaneAgentStatusChangedEvent
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return core.StatusEvent{}, fmt.Errorf("decode %s: %w", env.Event, err)
	}
	if d.PaneID == "" {
		return core.StatusEvent{}, fmt.Errorf("%w: status event has no pane id", errUnexpectedEvent)
	}
	return core.StatusEvent{PaneID: d.PaneID, Status: core.Status(d.AgentStatus), AgentProgram: program(d.Agent)}, nil
}

// first returns the first non-empty string among ps, or "".
func first(ps ...*string) string {
	for _, p := range ps {
		if p != nil && *p != "" {
			return *p
		}
	}
	return ""
}

func program(p *string) core.AgentProgram {
	if p == nil {
		return ""
	}
	return core.AgentProgram(*p)
}

func copyUint64(p *uint64) *uint64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
