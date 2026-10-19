package engine

func (e *Engine) record(t any) {
	e.store.Append(t)
	e.store.AppendResyncMarker(t)
	e.store.SaveServerState(t)
}
