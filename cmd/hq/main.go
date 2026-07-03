package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"hq/internal/hq"
)

func main() {
	var (
		schemaPath = flag.String("schema", "", "schema JSONL path; defaults to built-in hq schema")
		complete   = flag.String("complete", "", "non-interactive: print suggestions for this buffer as JSON")
		context    = flag.String("context", "", "non-interactive: print cursor context for this buffer as JSON")
		draft      = flag.String("draft", "", "non-interactive: print compileDraft for this buffer as JSON")
		cursor     = flag.Int("cursor", -1, "cursor byte offset for --complete/--context; default=len(buffer)")
	)
	flag.Parse()

	world, err := loadWorld(*schemaPath)
	if err != nil {
		fatal(err)
	}

	if flagWasSet("complete") {
		c := chooseCursor(*complete, *cursor)
		writeJSON(os.Stdout, hq.Complete(*complete, c, world))
		return
	}
	if flagWasSet("context") {
		c := chooseCursor(*context, *cursor)
		writeJSON(os.Stdout, hq.Analyze(*context, c, world))
		return
	}
	if flagWasSet("draft") {
		writeJSON(os.Stdout, hq.CompileLine(*draft, world))
		return
	}

	fmt.Fprintln(os.Stderr, "hq interactive runtime is still carried by cmd/hq-reflective during transition")
	os.Exit(2)
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

func chooseCursor(buffer string, c int) int {
	if c < 0 || c > len(buffer) {
		return len(buffer)
	}
	return c
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func flagWasSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
