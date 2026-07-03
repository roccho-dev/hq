package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"hq/internal/hq"

	readline "github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
)

func main() {
	schemaPath := flag.String("schema", "", "schema JSONL path")
	queuePath := flag.String("queue", "", "append accepted compileDraft JSONL here")
	complete := flag.String("complete", "", "print suggestions for this buffer")
	context := flag.String("context", "", "print cursor context for this buffer")
	draft := flag.String("draft", "", "print compileDraft for this buffer")
	cursor := flag.Int("cursor", -1, "cursor byte offset")
	noBanner := flag.Bool("no-banner", false, "hide banner")
	flag.Parse()

	world, err := loadWorld(*schemaPath)
	if err != nil { die(err) }
	if flagWasSet("complete") { writeJSON(hq.Complete(*complete, chooseCursor(*complete, *cursor), world)); return }
	if flagWasSet("context") { writeJSON(hq.Analyze(*context, chooseCursor(*context, *cursor), world)); return }
	if flagWasSet("draft") { writeJSON(hq.CompileLine(*draft, world)); return }
	if err := interactive(world, *queuePath, !*noBanner); err != nil { die(err) }
}

func loadWorld(path string) (*hq.JsonlWorld, error) {
	if path == "" { return hq.DefaultWorld(), nil }
	f, err := os.Open(path); if err != nil { return nil, err }
	defer f.Close()
	return hq.LoadSchemaJSONL(f)
}

func interactive(world *hq.JsonlWorld, queuePath string, banner bool) error {
	rl := readline.NewShell(inputrc.WithName("hq"))
	_ = rl.Config.Set("editing-mode", "vi")
	_ = rl.Config.Set("autocomplete", true)
	_ = rl.Config.Set("history-autosuggest", true)
	_ = rl.Config.Set("completion-ignore-case", true)
	_ = rl.Config.Set("usage-hint-always", true)
	rl.Keymap.SetMain("vi-insert")
	_ = rl.Config.Bind("vi-insert", "\t", "possible-completions", false)
	_ = rl.Config.Bind("emacs", "\t", "possible-completions", false)
	rl.Prompt.Primary(func() string { return "hq> " })
	rl.Prompt.Right(func() string { return "JSONL-aware autocomplete compiler" })
	rl.Completer = func(line []rune, cursor int) readline.Completions {
		buffer := string(line)
		sugs := hq.Complete(buffer, cursor, world)
		vals := make([]readline.Completion, 0, len(sugs))
		prefix := ""
		if len(sugs) > 0 { ed := sugs[0].Edit; if ed.Start >= 0 && ed.End <= len(buffer) && ed.End >= ed.Start { prefix = buffer[ed.Start:ed.End] } }
		for _, s := range sugs { vals = append(vals, readline.Completion{Value: s.InsertText, Display: s.Label, Description: s.Detail, Tag: s.Tag}) }
		c := readline.CompleteRaw(vals).DisplayList().JustifyDescriptions().NoSort().Usage("TAB: candidates / ENTER: accept")
		c.PREFIX = prefix
		return c
	}
	var q *os.File
	if queuePath != "" { f, err := os.OpenFile(queuePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); if err != nil { return err }; defer f.Close(); q = f }
	if banner { fmt.Println("hq: vi=ON Tab=candidates autocomplete=ON Enter=queue") }
	for {
		line, err := rl.Readline(); if err != nil { return nil }
		if t := strings.TrimSpace(line); t == "exit" || t == "quit" { return nil }
		d := hq.CompileLine(line, world)
		b, _ := json.Marshal(d); fmt.Printf("[ACCEPT] %s\n", b)
		if q != nil { if err := (hq.QueueWriter{W: q}).Append(d); err != nil { return err } }
	}
}

func chooseCursor(buffer string, c int) int { if c < 0 || c > len(buffer) { return len(buffer) }; return c }
func writeJSON(v any) { enc := json.NewEncoder(os.Stdout); enc.SetEscapeHTML(false); enc.SetIndent("", "  "); _ = enc.Encode(v) }
func flagWasSet(name string) bool { ok := false; flag.Visit(func(f *flag.Flag) { if f.Name == name { ok = true } }); return ok }
func die(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
