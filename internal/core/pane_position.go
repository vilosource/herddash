package core

import (
	"cmp"
	"slices"
	"strconv"
)

// PaneRect is one pane's rectangle in its tab's layout, in terminal cells, as
// herdr reports it.
type PaneRect struct {
	PaneID string
	X, Y   int
	Width  int
	Height int
}

// PanePositions names where each pane of one tab sits, from the tab's layout
// rectangles, so the operator can tell two Agents in one tab apart. It returns
// a map from pane id to position; a tab of one pane (or none) gets no entry,
// which reads as "".
//
// A position is a vertical part and a horizontal part, joined with "-" when
// both are set ("top-left"), else whichever is set:
//
//   - Horizontal, by column: "left" when the pane starts at the tab's left
//     edge but does not reach its right edge, "right" for the reverse,
//     "middle" when it touches neither, and nothing when it spans the width.
//   - Vertical, only when another pane's horizontal extent overlaps the
//     pane's: "top" when it starts at the top edge of its column but does not
//     reach its bottom, "bottom" for the reverse, "middle" when it touches
//     neither. Its column is the overlapping panes no wider than it, or every
//     overlapping pane when none is.
//
// Names that would repeat within the tab, such as the two inner columns of
// four, are numbered in reading order, top to bottom then left to right:
// "middle 1", "middle 2".
func PanePositions(rects []PaneRect) map[string]string {
	if len(rects) < 2 {
		return nil
	}
	minX, maxR := rects[0].X, rects[0].X+rects[0].Width
	for _, r := range rects[1:] {
		minX = min(minX, r.X)
		maxR = max(maxR, r.X+r.Width)
	}
	out := make(map[string]string, len(rects))
	for i, r := range rects {
		var horizontal string
		atLeft, atRight := r.X == minX, r.X+r.Width == maxR
		switch {
		case atLeft && atRight:
		case atLeft:
			horizontal = "left"
		case atRight:
			horizontal = "right"
		default:
			horizontal = "middle"
		}

		// r's column: the panes overlapping it horizontally that are no wider
		// than it, so a pane spanning several columns does not make the top
		// of one column read as its middle. With none, every overlapping pane.
		top, bottom := r.Y, r.Y+r.Height
		ntop, nbottom := top, bottom
		shared, narrower := false, false
		for j, o := range rects {
			if j == i || o.X >= r.X+r.Width || r.X >= o.X+o.Width {
				continue
			}
			shared = true
			top = min(top, o.Y)
			bottom = max(bottom, o.Y+o.Height)
			if o.Width <= r.Width {
				narrower = true
				ntop = min(ntop, o.Y)
				nbottom = max(nbottom, o.Y+o.Height)
			}
		}
		if narrower {
			top, bottom = ntop, nbottom
		}
		var vertical string
		if shared {
			atTop, atBottom := r.Y == top, r.Y+r.Height == bottom
			switch {
			case atTop && atBottom:
			case atTop:
				vertical = "top"
			case atBottom:
				vertical = "bottom"
			default:
				vertical = "middle"
			}
		}

		switch {
		case vertical != "" && horizontal != "":
			out[r.PaneID] = vertical + "-" + horizontal
		case vertical != "":
			out[r.PaneID] = vertical
		default:
			out[r.PaneID] = horizontal
		}
	}
	disambiguate(rects, out)
	return out
}

// disambiguate numbers the positions that more than one pane shares, in
// reading order, so every pane of a tab has its own name.
func disambiguate(rects []PaneRect, out map[string]string) {
	count := make(map[string]int, len(out))
	for _, name := range out {
		count[name]++
	}
	order := make([]PaneRect, len(rects))
	copy(order, rects)
	slices.SortStableFunc(order, func(a, b PaneRect) int {
		if a.Y != b.Y {
			return cmp.Compare(a.Y, b.Y)
		}
		return cmp.Compare(a.X, b.X)
	})
	seen := make(map[string]int, len(out))
	for _, r := range order {
		name := out[r.PaneID]
		if count[name] < 2 {
			continue
		}
		seen[name]++
		out[r.PaneID] = name + " " + strconv.Itoa(seen[name])
	}
}
