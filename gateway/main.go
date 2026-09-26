package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

func main() {
	listen := flag.String("listen", ":7100", "address old clients connect to")
	sessionFile := flag.String("session", "ntgram.session", "where the Telegram login is kept")
	test := flag.Bool("test", false, "use Telegram test servers")
	flag.Parse()
	log.SetFlags(0)

	appID, appHash := telegram.TestAppID, telegram.TestAppHash
	if !*test {
		id, err := strconv.Atoi(os.Getenv("TG_APP_ID"))
		appHash = os.Getenv("TG_APP_HASH")
		if err != nil || appHash == "" {
			log.Fatal("set TG_APP_ID and TG_APP_HASH from https://my.telegram.org/apps")
		}
		appID = id
	}

	g := newGateway(os.Getenv("NTGRAM_PASSWORD"))
	d := tg.NewUpdateDispatcher()
	g.onUpdates(d)
	g.gaps = updates.New(updates.Config{Handler: d})

	opts := telegram.Options{
		SessionStorage: &session.FileStorage{Path: *sessionFile},
		UpdateHandler:  g.gaps,
	}
	if *test {
		opts.DC = 2
		opts.DCList = dcs.Test()
	}
	g.tg = telegram.NewClient(appID, appHash, opts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := g.tg.Run(ctx, func(ctx context.Context) error {
		l, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		defer l.Close()
		log.Printf("listening on %s", l.Addr())
		go g.serve(ctx, l)
		<-ctx.Done()
		return nil
	})
	if err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
