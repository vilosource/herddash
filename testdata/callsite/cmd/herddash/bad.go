package main

import (
	"reflect"

	"github.com/vilosource/herddash/internal/store"
)

func record(st *store.Store, rec interface{ Append(any) error }) {
	_ = reflect.Append(reflect.ValueOf([]int{}), reflect.ValueOf(1))
	st.Append(nil)
	st.AppendResyncMarker(store.ResyncMarker{})
	st.SaveServerState("h", "s", store.ServerState{Epoch: 1})
	st.Sweep(nil, 0, nil)
	st.AppendAcknowledgements(nil, nil)
	keep(rec)
}

var (
	appendLater = (*store.Store).Append
	sweepLater  = (*store.Store).Sweep
)

func keep(any) {}
