package importer

// The importer appends and imports, but never sweeps.
func run(st interface {
	Append(any) error
	Sweep(any, int64, any) (any, error)
	AppendBackfill(any, any) (int, error)
}) {
	st.Sweep(nil, 0, nil)
	st.Append(nil)
	st.AppendBackfill(nil, nil)
}
