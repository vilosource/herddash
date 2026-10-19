package httpapi

type writer interface{ Append(any) error }

// A Recorder handed to httpapi is still the record.
type handler struct{ w writer }

func (h handler) serve() {
	h.w.Append(nil)
}
