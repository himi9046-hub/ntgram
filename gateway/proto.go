package main

import "strings"

var (
	escaper   = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", "")
	unescaper = strings.NewReplacer(`\\`, `\`, `\t`, "\t", `\n`, "\n")
)

func line(fields ...string) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = escaper.Replace(f)
	}
	return strings.Join(out, "\t") + "\n"
}

func parse(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	fields := strings.Split(s, "\t")
	for i, f := range fields {
		fields[i] = unescaper.Replace(f)
	}
	return fields
}
