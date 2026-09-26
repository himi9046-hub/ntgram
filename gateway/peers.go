package main

import (
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

func peerKey(p tg.PeerClass) string {
	switch p := p.(type) {
	case *tg.PeerUser:
		return "u" + strconv.FormatInt(p.UserID, 10)
	case *tg.PeerChat:
		return "c" + strconv.FormatInt(p.ChatID, 10)
	case *tg.PeerChannel:
		return "ch" + strconv.FormatInt(p.ChannelID, 10)
	}
	return ""
}

func inputKey(p tg.InputPeerClass) string {
	switch p := p.(type) {
	case *tg.InputPeerUser:
		return "u" + strconv.FormatInt(p.UserID, 10)
	case *tg.InputPeerChat:
		return "c" + strconv.FormatInt(p.ChatID, 10)
	case *tg.InputPeerChannel:
		return "ch" + strconv.FormatInt(p.ChannelID, 10)
	}
	return ""
}

type book struct {
	users    map[int64]*tg.User
	chats    map[int64]*tg.Chat
	channels map[int64]*tg.Channel
}

func newBook() *book {
	return &book{users: map[int64]*tg.User{}, chats: map[int64]*tg.Chat{}, channels: map[int64]*tg.Channel{}}
}

func (b *book) add(users map[int64]*tg.User, chats map[int64]*tg.Chat, channels map[int64]*tg.Channel) {
	for id, u := range users {
		b.users[id] = u
	}
	for id, c := range chats {
		b.chats[id] = c
	}
	for id, c := range channels {
		b.channels[id] = c
	}
}

func (b *book) input(key string) (tg.InputPeerClass, bool) {
	kind, id := splitKey(key)
	switch kind {
	case "u":
		if u, ok := b.users[id]; ok {
			return u.AsInputPeer(), true
		}
	case "c":
		if _, ok := b.chats[id]; ok {
			return &tg.InputPeerChat{ChatID: id}, true
		}
	case "ch":
		if c, ok := b.channels[id]; ok {
			return c.AsInputPeer(), true
		}
	}
	return nil, false
}

func (b *book) title(key string) string {
	kind, id := splitKey(key)
	switch kind {
	case "u":
		if u, ok := b.users[id]; ok {
			return userName(u)
		}
	case "c":
		if c, ok := b.chats[id]; ok {
			return c.Title
		}
	case "ch":
		if c, ok := b.channels[id]; ok {
			return c.Title
		}
	}
	return key
}

func userName(u *tg.User) string {
	if u.Self {
		return "Saved Messages"
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = "+" + u.Phone
	}
	return name
}

func splitKey(key string) (string, int64) {
	i := strings.IndexFunc(key, func(r rune) bool { return r >= '0' && r <= '9' })
	if i <= 0 {
		return "", 0
	}
	id, err := strconv.ParseInt(key[i:], 10, 64)
	if err != nil {
		return "", 0
	}
	return key[:i], id
}

func messageText(m *tg.Message) string {
	text := m.Message
	media, ok := m.GetMedia()
	if !ok {
		return text
	}
	var label string
	switch media.(type) {
	case *tg.MessageMediaPhoto:
		label = "[photo]"
	case *tg.MessageMediaDocument:
		label = "[file]"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		label = "[location]"
	case *tg.MessageMediaContact:
		label = "[contact]"
	case *tg.MessageMediaPoll:
		label = "[poll]"
	case *tg.MessageMediaWebPage:
		return text
	default:
		label = "[media]"
	}
	if text == "" {
		return label
	}
	return label + " " + text
}
