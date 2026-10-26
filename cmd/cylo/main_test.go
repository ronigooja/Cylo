package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseCommandSupportsNestedOperations(t *testing.T) {
	name, args, err := parseCommand([]string{"provider", "test", "--json"})
	if err != nil || name != "provider test" || len(args) != 1 || args[0] != "--json" {
		t.Fatalf("parseCommand() = %q, %#v, %v", name, args, err)
	}
}

func TestMenuDispatchesSelectedOperationAndExits(t *testing.T) {
	old := operations["status"]
	t.Cleanup(func() { operations["status"] = old })
	var called bool
	operations["status"] = func(args []string, _ io.Reader, out, _ io.Writer) error {
		called = true
		if len(args) != 0 {
			t.Errorf("menu passed unexpected args: %#v", args)
		}
		_, _ = io.WriteString(out, "ok")
		return nil
	}
	var out, errOut bytes.Buffer
	if err := menu(strings.NewReader("6\n0\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(out.String(), "ok") || errOut.Len() != 0 {
		t.Fatalf("menu output/call mismatch: called=%v out=%q err=%q", called, out.String(), errOut.String())
	}
}
