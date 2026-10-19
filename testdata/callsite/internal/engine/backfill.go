package engine

// The engine records live history, but never imports it.
func (e *Engine) backfill() {
	e.store.AppendBackfill(nil, nil)
}
