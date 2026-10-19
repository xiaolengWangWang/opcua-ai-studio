package device

import (
	"slices"
	"time"
)

type Direction string
type Status string

const (
	DirTX                 Direction = "TX"
	DirRX                 Direction = "RX"
	StatusSent            Status    = "SENT"
	StatusSuccess         Status    = "SUCCESS"
	StatusTimeout         Status    = "TIMEOUT"
	StatusException       Status    = "EXCEPTION"
	StatusCRCError        Status    = "CRC_ERROR"
	StatusConnectionError Status    = "CONNECTION_ERROR"
	StatusParseError      Status    = "PARSE_ERROR"
	StatusCancelled       Status    = "CANCELLED"
	StatusLate            Status    = "LATE_RESPONSE"
	StatusUnexpected      Status    = "UNEXPECTED"
)

type LegacyFields struct {
	Slave    int64 `json:"slave"`
	TxID     int64 `json:"tx_id"`
	Function int64 `json:"function"`
	Address  int64 `json:"address"`
	Count    int64 `json:"count"`
}
type EventResult struct {
	Read  *ReadResult  `json:"read,omitempty"`
	Write *WriteResult `json:"write,omitempty"`
}
type Event struct {
	Time         time.Time     `json:"time"`
	RequestID    uint64        `json:"request_id"`
	ConnectionID string        `json:"connection_id,omitempty"`
	Protocol     Protocol      `json:"protocol"`
	Family       string        `json:"family,omitempty"`
	Dir          Direction     `json:"direction"`
	Operation    string        `json:"operation,omitempty"`
	Targets      []Address     `json:"targets,omitempty"`
	Results      []EventResult `json:"results,omitempty"`
	Status       Status        `json:"status"`
	RTT          time.Duration `json:"rtt"`
	Raw          []byte        `json:"raw,omitempty"`
	Error        string        `json:"error,omitempty"`
	Legacy       *LegacyFields `json:"legacy,omitempty"`
	// Total is the request's target count when Targets keeps only the first.
	Total int `json:"total,omitempty"`
}
type Observer interface{ OnEvent(Event) }
type ObserverFunc func(Event)

func (f ObserverFunc) OnEvent(e Event) { f(e) }

// Clone prevents buffers and nested results being mutated after observation.
func (e Event) Clone() Event {
	e.Raw = append([]byte(nil), e.Raw...)
	e.Targets = append([]Address(nil), e.Targets...)
	e.Results = append([]EventResult(nil), e.Results...)
	for i, r := range e.Results {
		if r.Read != nil {
			v := *r.Read
			v.Value = cloneValue(v.Value)
			e.Results[i].Read = &v
		}
		if r.Write != nil {
			v := *r.Write
			v.Segments = append([]WriteSegment(nil), v.Segments...)
			e.Results[i].Write = &v
		}
	}
	if e.Legacy != nil {
		v := *e.Legacy
		e.Legacy = &v
	}
	return e
}
func cloneValue(v Value) Value {
	v.Raw = slices.Clone(v.Raw)
	v.Elements = slices.Clone(v.Elements)
	for i := range v.Elements {
		v.Elements[i] = cloneValue(v.Elements[i])
	}
	return v
}
