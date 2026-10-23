package engine

// engine -> herdr is a permitted edge, so only the gen confinement rule fires,
// for gen itself and for a package under it.
import (
	_ "github.com/vilosource/herddash/internal/herdr/gen"
	_ "github.com/vilosource/herddash/internal/herdr/gen/genwire"
)
