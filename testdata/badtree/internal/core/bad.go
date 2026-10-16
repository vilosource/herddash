package core

import (
	"time"

	_ "example.com/notstd"
	_ "github.com/vilosource/herddash/internal/store"
)

func elapsed(t time.Time) time.Duration {
	return time.Since(t)
}
