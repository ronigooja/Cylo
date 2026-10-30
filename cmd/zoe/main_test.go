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
	if err := menu(strings.NewReader("1\n0\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(out.String(), "ok") || errOut.Len() != 0 {
		t.Fatalf("menu output/call mismatch: called=%v out=%q err=%q", called, out.String(), errOut.String())
	}
}

func TestMenuSharesInputAcrossPromptsAndActions(t *testing.T) {
	old := operations["provider configure"]
	t.Cleanup(func() { operations["provider configure"] = old })
	calls := 0
	operations["provider configure"] = func(_ []string, in io.Reader, out, _ io.Writer) error {
		calls++
		url, err := inputLine(in)
		if err != nil {
			return err
		}
		model, err := inputLine(in)
		if err != nil {
			return err
		}
		token, err := readToken(in, out)
		if err != nil {
			return err
		}
		if url != "https://provider/v1" || model != "model" || string(token) != "private" {
			t.Errorf("prompt input lost or reordered")
		}
		return nil
	}
	var out, errOut bytes.Buffer
	if err := menu(strings.NewReader("3\nhttps://provider/v1\nmodel\nprivate\n3\nhttps://provider/v1\nmodel\nprivate\n0\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || errOut.Len() != 0 {
		t.Fatalf("calls=%d diagnostics=%s", calls, &errOut)
	}
	if strings.Contains(out.String(), "private") || strings.Contains(out.String(), "hidden") {
		t.Fatal("prompt reveals token or claims hidden input")
	}
	if !strings.Contains(out.String(), "API token: ") {
		t.Fatalf("token prompt missing: %q", out.String())
	}
}

func TestMenuUsesGroupedTwoDigitLabels(t *testing.T) {
	old := operations["key rotate"]
	t.Cleanup(func() { operations["key rotate"] = old })
	called := false
	operations["key rotate"] = func(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
		called = true
		return nil
	}
	var out, errOut bytes.Buffer
	if err := menu(strings.NewReader("11\n0\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Overview:", "1) Show status", "First-time setup", "Models:", "5) Configure memory model", "Voice:", "Access:", "10) Create instance key", "11) Rotate instance key"} {
		if !strings.Contains(out.String(), label) {
			t.Fatalf("menu missing %q: %s", label, out.String())
		}
	}
	if !called || errOut.Len() != 0 {
		t.Fatalf("two-digit dispatch failed: called=%v err=%q", called, errOut.String())
	}
}
