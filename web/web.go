package web

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/FarmRadioHangar/devices/db"
	"github.com/FarmRadioHangar/devices/events"
	"github.com/gernest/alien"
	"github.com/gorilla/websocket"
)

const evtCtxKey = "_stream"

var upgrader = websocket.Upgrader{} // use default options

func GetDongles(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}
	ql, ok := r.Context().Value(db.CtxKey).(*sql.DB)
	if !ok {
		// Log something and return?
		return
	}
	dongles, err := db.GetAllDongles(ql)
	if err != nil {
		// log something?
		return
	}
	_ = ws.WriteJSON(dongles)
	stream, ok := r.Context().Value(evtCtxKey).(*events.Stream)
	if !ok {
		// Log something and return?
		return
	}
	evts := stream.Channel()
	go func() {
		for {
			select {
			case ev := <-evts:
				_ = ws.WriteJSON(ev)
			}
		}
	}()
	reader(ws)
}

func reader(ws *websocket.Conn) {
	defer ws.Close()
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
		}
	}
}

func PrepCtx(ql *sql.DB, s *events.Stream) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), db.CtxKey, ql)
			ctx = context.WithValue(ctx, evtCtxKey, s)
		})
	}
}

func New(ql *sql.DB, s *events.Stream) *alien.Mux {
	m := alien.New()
	m.Use(PrepCtx(ql, s))
	m.Get("/", GetDongles)
	return m
}