package web

import (
	"context"
	"database/sql"
	"fmt"
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
	ctx := r.Context()
	ql, ok := ctx.Value(db.CtxKey).(*sql.DB)
	if !ok {
		// Log something and return?
		return
	}
	dongles, err := db.GetDistinct(ql)
	if err != nil {
		// log something?
		fmt.Printf("ERROR: %v\n", err)
	}
	if dongles == nil {
		dongles = []*db.Dongle{}
	}
	_ = ws.WriteJSON(dongles)
	stream, ok := ctx.Value(evtCtxKey).(*events.Stream)
	if !ok {
		// Log something and return?
		return
	}
	ch, evts := stream.Subscribe()
	defer stream.Unsubscribe(ch)
	go func() {
		for {
			select {
			case ev := <-evts:
				_ = ws.WriteJSON(ev)
			}
		}
	}()
	reader(ctx, ws)
}

func reader(ctx context.Context, ws *websocket.Conn) {
	defer ws.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, _, err := ws.ReadMessage()
			if err != nil {
				log.Println(err)
			}
		}
	}
}

func PrepCtx(ql *sql.DB, s *events.Stream) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), db.CtxKey, ql)
			ctx = context.WithValue(ctx, evtCtxKey, s)
			r = r.WithContext(ctx)
			h.ServeHTTP(w, r)
		})
	}
}

func New(ql *sql.DB, s *events.Stream) *alien.Mux {
	m := alien.New()
	m.Use(PrepCtx(ql, s))
	m.Get("/", GetDongles)
	return m
}
