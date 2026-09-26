package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type gateway struct {
	tg       *telegram.Client
	gaps     *updates.Manager
	password string

	mu       sync.Mutex
	peers    *book
	clients  map[*conn]struct{}
	phone    string
	codeHash string
	started  sync.Once
}

type conn struct {
	out    chan string
	authed bool
}

func newGateway(password string) *gateway {
	return &gateway{password: password, peers: newBook(), clients: map[*conn]struct{}{}}
}

func (g *gateway) onUpdates(d tg.UpdateDispatcher) {
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		g.push(e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		g.push(e, u.Message)
		return nil
	})
}

func (g *gateway) push(e tg.Entities, m tg.MessageClass) {
	msg, ok := m.(*tg.Message)
	if !ok {
		return
	}
	g.mu.Lock()
	g.peers.add(e.Users, e.Chats, e.Channels)
	l := g.format(peerKey(msg.PeerID), msg)
	g.mu.Unlock()
	g.broadcast(l)
}

func (g *gateway) format(key string, m *tg.Message) string {
	from := g.peers.title(key)
	if m.Out {
		from = "me"
	} else if p, ok := m.GetFromID(); ok {
		from = g.peers.title(peerKey(p))
	}
	return line("MSG", key, strconv.Itoa(m.ID), strconv.Itoa(m.Date), from, messageText(m))
}

func (g *gateway) broadcast(l string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for c := range g.clients {
		if !c.authed {
			continue
		}
		select {
		case c.out <- l:
		default:
		}
	}
}

func (g *gateway) serve(ctx context.Context, l net.Listener) {
	for {
		nc, err := l.Accept()
		if err != nil {
			return
		}
		go g.handle(ctx, nc)
	}
}

func (g *gateway) handle(ctx context.Context, nc net.Conn) {
	defer nc.Close()
	c := &conn{out: make(chan string, 256), authed: g.password == ""}
	g.mu.Lock()
	g.clients[c] = struct{}{}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.clients, c)
		g.mu.Unlock()
	}()

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case l := <-c.out:
				if _, err := nc.Write([]byte(l)); err != nil {
					nc.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()

	if c.authed {
		g.sendState(ctx, c)
	} else {
		c.out <- line("AUTH", "need_gateway_password")
	}

	r := bufio.NewReader(nc)
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			return
		}
		f := parse(s)
		if len(f) == 0 {
			continue
		}
		if err := g.command(ctx, c, f); err != nil {
			c.out <- line("ERR", errText(err))
		}
	}
}

func errText(err error) string {
	rpc, ok := tgerr.As(err)
	switch {
	case !ok:
		return err.Error()
	case rpc.Argument > 0:
		return fmt.Sprintf("%s %d", rpc.Type, rpc.Argument)
	}
	return rpc.Type
}

func (g *gateway) sendState(ctx context.Context, c *conn) {
	st, err := g.tg.Auth().Status(ctx)
	switch {
	case err != nil:
		c.out <- line("ERR", err.Error())
	case st.Authorized:
		g.ready(ctx, c)
	default:
		c.out <- line("AUTH", "need_phone")
	}
}

func (g *gateway) command(ctx context.Context, c *conn, f []string) error {
	cmd, args := f[0], f[1:]
	if !c.authed {
		if cmd != "PASS" || len(args) != 1 || args[0] != g.password {
			return errors.New("wrong gateway password")
		}
		g.mu.Lock()
		c.authed = true
		g.mu.Unlock()
		g.sendState(ctx, c)
		return nil
	}

	switch cmd {
	case "PHONE":
		if len(args) != 1 {
			return errors.New("usage: PHONE <number>")
		}
		phone := strings.Map(func(r rune) rune {
			if r < '0' || r > '9' {
				return -1
			}
			return r
		}, args[0])
		sent, err := g.tg.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
		if err != nil {
			return err
		}
		code, ok := sent.(*tg.AuthSentCode)
		if !ok {
			return fmt.Errorf("unexpected reply %T", sent)
		}
		g.mu.Lock()
		g.phone, g.codeHash = phone, code.PhoneCodeHash
		g.mu.Unlock()
		c.out <- line("AUTH", "need_code")

	case "CODE":
		if len(args) != 1 {
			return errors.New("usage: CODE <code>")
		}
		g.mu.Lock()
		phone, hash := g.phone, g.codeHash
		g.mu.Unlock()
		_, err := g.tg.Auth().SignIn(ctx, phone, args[0], hash)
		var signUp *auth.SignUpRequired
		switch {
		case errors.Is(err, auth.ErrPasswordAuthNeeded):
			c.out <- line("AUTH", "need_password")
		case errors.As(err, &signUp):
			c.out <- line("AUTH", "need_name")
		case err != nil:
			return err
		default:
			g.ready(ctx, c)
		}

	case "PASSWORD":
		if len(args) != 1 {
			return errors.New("usage: PASSWORD <password>")
		}
		if _, err := g.tg.Auth().Password(ctx, args[0]); err != nil {
			return err
		}
		g.ready(ctx, c)

	case "NAME":
		if len(args) < 1 {
			return errors.New("usage: NAME <first> [last]")
		}
		last := ""
		if len(args) > 1 {
			last = args[1]
		}
		g.mu.Lock()
		phone, hash := g.phone, g.codeHash
		g.mu.Unlock()
		if _, err := g.tg.Auth().SignUp(ctx, auth.SignUp{
			PhoneNumber: phone, PhoneCodeHash: hash, FirstName: args[0], LastName: last,
		}); err != nil {
			return err
		}
		g.ready(ctx, c)

	case "CHATS":
		return g.chats(ctx, c)

	case "HISTORY":
		if len(args) < 1 {
			return errors.New("usage: HISTORY <chat> [limit]")
		}
		limit := 50
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		return g.history(ctx, c, args[0], limit)

	case "SEND":
		if len(args) != 2 {
			return errors.New("usage: SEND <chat> <text>")
		}
		g.mu.Lock()
		p, ok := g.peers.input(args[0])
		g.mu.Unlock()
		if !ok {
			return fmt.Errorf("unknown chat %s, ask for CHATS first", args[0])
		}
		if _, err := message.NewSender(g.tg.API()).To(p).Text(ctx, args[1]); err != nil {
			return err
		}

	case "PING":
		c.out <- line("PONG")

	default:
		return fmt.Errorf("unknown command %s", cmd)
	}
	return nil
}

func (g *gateway) ready(ctx context.Context, c *conn) {
	g.started.Do(func() {
		self, err := g.tg.Self(ctx)
		if err != nil {
			c.out <- line("ERR", "no live updates: "+err.Error())
			return
		}
		go func() {
			err := g.gaps.Run(ctx, g.tg.API(), self.ID, updates.AuthOptions{Forget: true})
			if err != nil && ctx.Err() == nil {
				log.Printf("updates stopped: %v", err)
			}
		}()
	})
	c.out <- line("AUTH", "ok")
}

func (g *gateway) chats(ctx context.Context, c *conn) error {
	it := dialogs.NewQueryBuilder(g.tg.API()).GetDialogs().BatchSize(100).Iter()
	count := 0
	for count < 200 && it.Next(ctx) {
		d := it.Value()
		key := inputKey(d.Peer)
		if key == "" {
			continue
		}
		g.mu.Lock()
		g.peers.add(d.Entities.Users(), d.Entities.Chats(), d.Entities.Channels())
		title := g.peers.title(key)
		g.mu.Unlock()
		unread := 0
		if dl, ok := d.Dialog.(*tg.Dialog); ok {
			unread = dl.UnreadCount
		}
		c.out <- line("CHAT", key, title, strconv.Itoa(unread))
		count++
	}
	if err := it.Err(); err != nil {
		return err
	}
	c.out <- line("END", "CHATS")
	return nil
}

func (g *gateway) history(ctx context.Context, c *conn, key string, limit int) error {
	g.mu.Lock()
	p, ok := g.peers.input(key)
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown chat %s, ask for CHATS first", key)
	}

	var got []*tg.Message
	it := messages.NewQueryBuilder(g.tg.API()).GetHistory(p).BatchSize(limit).Iter()
	for len(got) < limit && it.Next(ctx) {
		e := it.Value()
		g.mu.Lock()
		g.peers.add(e.Entities.Users(), e.Entities.Chats(), e.Entities.Channels())
		g.mu.Unlock()
		if m, ok := e.Msg.(*tg.Message); ok {
			got = append(got, m)
		}
	}
	if err := it.Err(); err != nil {
		return err
	}

	g.mu.Lock()
	for i := len(got) - 1; i >= 0; i-- {
		c.out <- g.format(key, got[i])
	}
	g.mu.Unlock()
	c.out <- line("END", "HISTORY", key)
	return nil
}
