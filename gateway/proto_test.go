package main

import (
	"reflect"
	"testing"
)

func TestLineRoundTrip(t *testing.T) {
	fields := []string{"MSG", "u42", "multi\nline", "tab\there", `back\slash`, `\n literal`}
	got := parse(line(fields...))
	if !reflect.DeepEqual(got, fields) {
		t.Fatalf("got %q", got)
	}
}

func TestLineIsOneLine(t *testing.T) {
	l := line("SEND", "u1", "a\nb\r\nc")
	if l != "SEND\tu1\ta\\nb\\nc\n" {
		t.Fatalf("line = %q", l)
	}
}

func TestParseAcceptsWindowsLineEndings(t *testing.T) {
	if got := parse("CHATS\r\n"); !reflect.DeepEqual(got, []string{"CHATS"}) {
		t.Fatalf("got %q", got)
	}
	if parse("\r\n") != nil {
		t.Fatal("empty line should parse to nil")
	}
}
