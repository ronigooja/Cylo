// Command zoe is the operator entry point for local Zoe maintenance.
// With no arguments it opens a small menu so operators do not need to
// remember sub-command names. Every menu action also has a scriptable form.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type operation func(args []string, in io.Reader, out, errOut io.Writer) error

// Operations are registered by the command implementations. Keeping the
// dispatch layer independent lets setup, provider and key flows share one
// menu while remaining usable from scripts.
var operations = map[string]operation{
	"setup":              unavailable("setup"),
	"provider configure": unavailable("provider configure"),
	"provider test":      unavailable("provider test"),
	"memory configure":   unavailable("memory configure"),
	"memory test":        unavailable("memory test"),
	"key init":           unavailable("key init"),
	"key rotate":         unavailable("key rotate"),
	"status":             unavailable("status"),
	"voice configure":    unavailable("voice configure"),
	"voice test":         unavailable("voice test"),
	"voice status":       unavailable("voice status"),
}

func unavailable(name string) operation {
	return func(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
		return fmt.Errorf("zoe %s is unavailable in this build", name)
	}
}

// RegisterOperation replaces one operation. It is intentionally small so
// command implementations can be assembled in separate files without
// duplicating argument or menu handling.
func RegisterOperation(name string, fn func([]string, io.Reader, io.Writer, io.Writer) error) error {
	name = normalizeCommand(name)
	if _, ok := operations[name]; !ok {
		return fmt.Errorf("unknown zoe operation %q", name)
	}
	if fn == nil {
		return errors.New("zoe operation cannot be nil")
	}
	operations[name] = fn
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "zoe:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	in = sharedInput(in)
	if len(args) == 0 {
		return menu(in, out, errOut)
	}
	name, rest, err := parseCommand(args)
	if err != nil {
		return err
	}
	fn, ok := operations[name]
	if !ok {
		return usageError()
	}
	return fn(rest, in, out, errOut)
}

func parseCommand(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("missing command")
	}
	if len(args) >= 2 {
		name := normalizeCommand(args[0] + " " + args[1])
		if _, ok := operations[name]; ok {
			return name, args[2:], nil
		}
	}
	name := normalizeCommand(args[0])
	if _, ok := operations[name]; ok {
		return name, args[1:], nil
	}
	return "", nil, usageError()
}

func normalizeCommand(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func usageError() error {
	return errors.New("usage: zoe [setup|provider configure|provider test|memory configure|memory test|key init|key rotate|status|voice configure|voice test|voice status]")
}

func menu(in io.Reader, out, errOut io.Writer) error {
	in = sharedInput(in)
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Zoe maintenance")
		fmt.Fprintln(out, "Overview:")
		fmt.Fprintln(out, "  1) Show status")
		fmt.Fprintln(out, "  2) First-time setup")
		fmt.Fprintln(out, "Models:")
		fmt.Fprintln(out, "  3) Configure task model")
		fmt.Fprintln(out, "  4) Test task model")
		fmt.Fprintln(out, "  5) Configure memory model")
		fmt.Fprintln(out, "  6) Test memory model")
		fmt.Fprintln(out, "Voice:")
		fmt.Fprintln(out, "  7) Configure voice gateway")
		fmt.Fprintln(out, "  8) Test voice gateway")
		fmt.Fprintln(out, "  9) Show voice gateway status")
		fmt.Fprintln(out, "Access:")
		fmt.Fprintln(out, "  10) Create instance key")
		fmt.Fprintln(out, "  11) Rotate instance key")
		fmt.Fprintln(out, "  0) Exit")
		fmt.Fprint(out, "Select an option: ")
		line, err := inputLine(in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		choice := strings.TrimSpace(line)
		if choice == "0" || strings.EqualFold(choice, "q") || strings.EqualFold(choice, "quit") || strings.EqualFold(choice, "exit") {
			return nil
		}
		name, ok := map[string]string{"1": "status", "2": "setup", "3": "provider configure", "4": "provider test", "5": "memory configure", "6": "memory test", "7": "voice configure", "8": "voice test", "9": "voice status", "10": "key init", "11": "key rotate"}[choice]
		if !ok {
			fmt.Fprintln(errOut, "Please choose a number from 0 to 11.")
			continue
		}
		if err := operations[name](nil, in, out, errOut); err != nil {
			fmt.Fprintln(errOut, "Error:", err)
		}
	}
}
