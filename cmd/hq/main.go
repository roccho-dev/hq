package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"hq/internal/hq"
)

func main() {
	schemaPath := flag.String("schema", "", "schema JSONL path")
	queuePath := flag.String("queue", "", "append accepted compileDraft JSONL here")
	complete := flag.String("complete", "", "print suggestions for this buffer")
	context := flag.String("context", "", "print cursor context for this buffer")
	draft := flag.String("draft", "", "print compileDraft for this buffer")
	cursor := flag.Int("cursor", -1, "cursor byte offset")
	flag.Parse()

	world, err := loadWorld(*schemaPath)
	if err != nil {
		die(err)
	}
	if flagWasSet("complete") {
		writeJSON(hq.Complete(*complete, chooseCursor(*complete, *cursor), world))
		return
	}
	if flagWasSet("context") {
		writeJSON(hq.Analyze(*context, chooseCursor(*context, *cursor), world))
		return
	}
	if flagWasSet("draft") {
		writeJSON(hq.CompileLine(*draft, world))
		return
	}
	if err := runLines(os.Stdin, *queuePath, world); err != nil {
		die(err)
	}
}

func loadWorld(path string) (*hq.JsonlWorld, error) {
	if path == "" {
		return hq.DefaultWorld(), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return hq.LoadSchemaJSONL(f)
}

func runLines(r io.Reader, queuePath string, world *hq.JsonlWorld) error {
	var q *os.File
	if queuePath != "" {
		f, err := os.OpenFile(queuePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		q = f
	}
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if t := strings.TrimSpace(line); t == "exit" || t == "quit" {
			return nil
		}
		d := hq.CompileLine(line, world)
		b, _ := json.Marshal(d)
		fmt.Printf("[ACCEPT] %s\n", b)
		if q != nil {
			if err := (hq.QueueWriter{W: q}).Append(d); err != nil {
				return err
			}
		}
	}
	return s.Err()
}

func chooseCursor(buffer string, c int) int {
	if c < 0 || c > len(buffer) {
		return len(buffer)
	}
	return c
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func flagWasSet(name string) bool {
	ok := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			ok = true
		}
	})
	return ok
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
