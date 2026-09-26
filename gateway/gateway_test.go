package main

import (
	"bufio"
	"context"
	"net"
	"reflect"
	"testing"
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
