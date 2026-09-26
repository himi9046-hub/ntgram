package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/gotd/td/tgerr"
)

func TestGatewayPasswordIsRequired(t *testing.T) {
	g := newGateway("secret")
	srv, cli := net.Pipe()
	defer cli.Close()
	go g.handle(context.Background(), srv)

	r := bufio.NewReader(cli)
	expect := func(want ...string) {
		t.Helper()
		s, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if got := parse(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	expect("AUTH", "need_gateway_password")
	for _, l := range []string{"CHATS\r\n", "PASS\twrong\r\n", "PASS\r\n"} {
		if _, err := cli.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
		expect("ERR", "wrong gateway password")
	}
}

func TestErrTextKeepsTelegramErrorShort(t *testing.T) {
	wrap := func(e error) error { return fmt.Errorf("sign in: rpcDoRequest: %w", e) }
	for err, want := range map[error]string{
		wrap(tgerr.New(400, "PHONE_CODE_INVALID")): "PHONE_CODE_INVALID",
		wrap(tgerr.New(420, "FLOOD_WAIT_30")):      "FLOOD_WAIT 30",
		errors.New("unknown chat u1"):              "unknown chat u1",
	} {
		if got := errText(err); got != want {
			t.Errorf("errText(%v) = %q, want %q", err, got, want)
		}
	}
}
