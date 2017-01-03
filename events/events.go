package events

import (
	"context"
	"sync"

	uuid "github.com/satori/go.uuid"
)

type Event struct {
	Name string      `json:"name"`
	Data interface{} `json:"data"`
}

type Stream struct {
	evts chan *Event
	subs map[string]chan *Event
	mu   sync.RWMutex
}

func NewStream(size int) *Stream {
	return &Stream{
		evts: make(chan *Event, size),
		subs: make(map[string]chan *Event),
	}
}

func (s *Stream) Subscribe() (string, <-chan *Event) {
	id := uuid.NewV4()
	e := make(chan *Event, 10)
	s.mu.Lock()
	s.subs[id.String()] = e
	s.mu.Unlock()
	return id.String(), e

}

func (s *Stream) Send(evt *Event) {
	go s.send(evt)
}

func (s *Stream) send(evt *Event) {
	s.evts <- evt
}

func (s *Stream) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case ev := <-s.evts:
				s.mu.RLock()
				for _, ch := range s.subs {
					go func(c chan *Event) {
						ch <- ev
					}(ch)
				}
				s.mu.RUnlock()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Stream) Unsubscribe(id string) {
	s.mu.Lock()
	delete(s.subs, id)
	s.mu.Unlock()
}
