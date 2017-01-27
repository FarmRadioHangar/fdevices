package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/FarmRadioHangar/fdevices/db"
	"github.com/FarmRadioHangar/fdevices/events"
	"github.com/FarmRadioHangar/fdevices/udev"
	"github.com/FarmRadioHangar/fdevices/web"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Version = "0.1.4"
	app.Usage = "Streams realtime events about devices (Dongles)"
	app.Commands = []cli.Command{
		{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Starts a server that listens to udev events",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "port",
					Usage: "ports to bind the server",
					Value: 8090,
				},
			},
			Action: Server,
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Server(cxt *cli.Context) error {
	s := events.NewStream(1000)
	ql, err := db.DB()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	m := udev.New(ql, s)
	m.Startup(ctx)
	go m.Run(ctx)

	defer cancel()
	w := web.New(ql, s)
	port := cxt.Int("port")
	fmt.Println("listening on port :", port)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), w)
}
