package events

type Event struct {
	Name string      `json:"name"`
	Data interface{} `json:"data"`
}

type Stream struct {
	evts chan *Event
}

func NewStream(size int) *Stream {
	return &Stream{evts: make(chan *Event, size)}
}

func (s *Stream) Channel() <-chan *Event {
	return s.evts
}

func (s *Stream) Send(evt *Event) {
	go s.send(evt)
}

func (s *Stream) send(evt *Event) {
	s.evts <- evt
}
