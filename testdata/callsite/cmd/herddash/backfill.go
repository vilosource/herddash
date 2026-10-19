package main

import "github.com/vilosource/herddash/internal/store"

func backfill(st *store.Store) {
	st.AppendBackfill(nil, nil)
}
