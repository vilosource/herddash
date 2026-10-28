package importer

// The importer appends and imports, but never sweeps or acknowledges.
func run(st interface {
	Append(any) error
	Sweep(any, int64, any) (any, error)
	AppendBackfill(any, any) (int, error)
	AppendAcknowledgements(any, any) error
}) {
	st.Sweep(nil, 0, nil)
	st.Append(nil)
	st.AppendBackfill(nil, nil)
	st.AppendAcknowledgements(nil, nil)
}
