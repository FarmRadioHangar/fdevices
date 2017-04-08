package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/FarmRadioHangar/fdevices/db"
	"github.com/FarmRadioHangar/fdevices/events"
	"github.com/FarmRadioHangar/fdevices/log"
	"github.com/FarmRadioHangar/fdevices/udev"
	"github.com/FarmRadioHangar/fdevices/web"
	"github.com/okzk/sdnotify"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Version = "0.1.10"
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
		log.Error(err.Error())
		os.Exit(1)
	}
}

// Server starts a service that manages the Dongles
func Server(cxt *cli.Context) error {
	s := events.NewStream(1000)
	ql, err := db.DB()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	log.Info("removing all symlinks managed by this application")
	err = udev.ClearSymlinks()
	if err != nil {
		return err
	}
	log.Info("OK")

	m := udev.New(ql, s)
	m.Startup(ctx)
	go m.Run(ctx)

	w := web.New(ql, s)
	port := cxt.Int("port")
	log.Info("listening on port :%d", port)
	log.Info("sending systeemd notify ready signal")
	err = sdnotify.SdNotifyReady()
	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info("OK")
	}
	return http.ListenAndServe(fmt.Sprintf(":%d", port), w)
}
