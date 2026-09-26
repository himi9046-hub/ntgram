package main

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestKeysMatchBetweenPeerAndInputPeer(t *testing.T) {
	cases := []struct {
		peer  tg.PeerClass
		input tg.InputPeerClass
		key   string
	}{
		{&tg.PeerUser{UserID: 42}, &tg.InputPeerUser{UserID: 42, AccessHash: 7}, "u42"},
		{&tg.PeerChat{ChatID: 5}, &tg.InputPeerChat{ChatID: 5}, "c5"},
		{&tg.PeerChannel{ChannelID: 1001}, &tg.InputPeerChannel{ChannelID: 1001, AccessHash: 9}, "ch1001"},
	}
	for _, c := range cases {
		if got := peerKey(c.peer); got != c.key {
			t.Errorf("peerKey = %q, want %q", got, c.key)
		}
		if got := inputKey(c.input); got != c.key {
			t.Errorf("inputKey = %q, want %q", got, c.key)
		}
	}
}

func TestSplitKey(t *testing.T) {
	for key, want := range map[string]struct {
		kind string
		id   int64
	}{
		"u42":    {"u", 42},
		"ch1001": {"ch", 1001},
		"42":     {"", 0},
		"x":      {"", 0},
		"u":      {"", 0},
	} {
		kind, id := splitKey(key)
		if kind != want.kind || id != want.id {
			t.Errorf("splitKey(%q) = %q %d", key, kind, id)
		}
	}
}

func TestBookResolvesKnownPeers(t *testing.T) {
	ada := &tg.User{ID: 42, FirstName: "Ada", LastName: "Lovelace"}
	ada.SetAccessHash(7)
	news := &tg.Channel{ID: 1001, Title: "News"}
	news.SetAccessHash(9)
	b := newBook()
	b.add(map[int64]*tg.User{42: ada}, map[int64]*tg.Chat{5: {ID: 5, Title: "Family"}}, map[int64]*tg.Channel{1001: news})

	in, ok := b.input("u42")
	if !ok || in.(*tg.InputPeerUser).AccessHash != 7 {
		t.Fatalf("user input = %#v %v", in, ok)
	}
	if in, _ := b.input("ch1001"); in.(*tg.InputPeerChannel).AccessHash != 9 {
		t.Fatalf("channel input = %#v", in)
	}
	if _, ok := b.input("u43"); ok {
		t.Fatal("unknown user resolved")
	}
	if got := b.title("u42"); got != "Ada Lovelace" {
		t.Fatalf("title = %q", got)
	}
	if got := b.title("c5"); got != "Family" {
		t.Fatalf("title = %q", got)
	}
	if got := b.title("ch1001"); got != "News" {
		t.Fatalf("title = %q", got)
	}
}

func TestUserNameFallbacks(t *testing.T) {
	if got := userName(&tg.User{Self: true, FirstName: "Me"}); got != "Saved Messages" {
		t.Fatalf("self = %q", got)
	}
	if got := userName(&tg.User{Username: "ada"}); got != "ada" {
		t.Fatalf("username = %q", got)
	}
	if got := userName(&tg.User{Phone: "79990001122"}); got != "+79990001122" {
		t.Fatalf("phone = %q", got)
	}
}

func TestMediaGetsALabel(t *testing.T) {
	photo := &tg.Message{Message: "look"}
	photo.SetMedia(&tg.MessageMediaPhoto{})
	if got := messageText(photo); got != "[photo] look" {
		t.Fatalf("photo = %q", got)
	}
	doc := &tg.Message{}
	doc.SetMedia(&tg.MessageMediaDocument{})
	if got := messageText(doc); got != "[file]" {
		t.Fatalf("doc = %q", got)
	}
	if got := messageText(&tg.Message{Message: "hi"}); got != "hi" {
		t.Fatalf("text = %q", got)
	}
}
