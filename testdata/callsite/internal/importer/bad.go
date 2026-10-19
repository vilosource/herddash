package importer

// The importer appends, but never sweeps.
func run(st interface {
	Append(any) error
	Sweep(any, int64, any) (any, error)
}) {
	st.Sweep(nil, 0, nil)
	st.Append(nil)
}
