package httpapi

type writer interface {
	Append(any) error
	AppendAcknowledgements(any, any) error
	AppendUsage(any, any) (int64, error)
}

// A Recorder handed to httpapi is still the record.
type handler struct{ w writer }

func (h handler) serve() {
	h.w.Append(nil)
	h.w.AppendAcknowledgements(nil, nil)
	h.w.AppendUsage(nil, nil)
}
