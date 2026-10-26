// Command cylo is the operator entry point for local Cylo maintenance.
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
	"key init":           unavailable("key init"),
	"key rotate":         unavailable("key rotate"),
	"status":             unavailable("status"),
}

func unavailable(name string) operation {
	return func(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
		return fmt.Errorf("cylo %s is unavailable in this build", name)
	}
}

// RegisterOperation replaces one operation. It is intentionally small so
// command implementations can be assembled in separate files without
// duplicating argument or menu handling.
func RegisterOperation(name string, fn func([]string, io.Reader, io.Writer, io.Writer) error) error {
	name = normalizeCommand(name)
	if _, ok := operations[name]; !ok {
		return fmt.Errorf("unknown cylo operation %q", name)
	}
	if fn == nil {
		return errors.New("cylo operation cannot be nil")
	}
	operations[name] = fn
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "cylo:", err)
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
	return errors.New("usage: cylo [setup|provider configure|provider test|key init|key rotate|status]")
}

func menu(in io.Reader, out, errOut io.Writer) error {
	in = sharedInput(in)
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Cylo maintenance")
		fmt.Fprintln(out, "  1) Setup")
		fmt.Fprintln(out, "  2) Configure provider")
		fmt.Fprintln(out, "  3) Test provider")
		fmt.Fprintln(out, "  4) Initialize instance key")
		fmt.Fprintln(out, "  5) Rotate instance key")
		fmt.Fprintln(out, "  6) Show status")
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
		name, ok := map[string]string{"1": "setup", "2": "provider configure", "3": "provider test", "4": "key init", "5": "key rotate", "6": "status"}[choice]
		if !ok {
			fmt.Fprintln(errOut, "Please choose a number from 0 to 6.")
			continue
		}
		if err := operations[name](nil, in, out, errOut); err != nil {
			fmt.Fprintln(errOut, "Error:", err)
		}
	}
}
