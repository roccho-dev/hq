package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"hq/internal/hq"

	readline "github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
)

func main() {
	var (
		schemaPath = flag.String("schema", "", "schema JSONL path; defaults to built-in hq schema")
		queuePath  = flag.String("queue", "", "append accepted compileDraft JSONL to this file")
		complete   = flag.String("complete", "", "non-interactive: print suggestions for this buffer as JSON")
		context    = flag.String("context", "", "non-interactive: print cursor context for this buffer as JSON")
		draft      = flag.String("draft", "", "non-interactive: print compileDraft for this buffer as JSON")
		cursor     = flag.Int("cursor", -1, "cursor byte offset for --complete/--context; default=len(buffer)")
		noBanner   = flag.Bool("no-banner", false, "hide interactive banner")
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

	if err := runInteractive(world, *queuePath, !*noBanner); err != nil {
		fatal(err)
	}
}

func runInteractive(world *hq.JsonlWorld, queuePath string, banner bool) error {
	rl := readline.NewShell(inputrc.WithName("hq"))
	_ = rl.Config.Set("editing-mode", "vi")
	_ = rl.Config.Set("show-mode-in-prompt", true)
	_ = rl.Config.Set("autocomplete", true)
	_ = rl.Config.Set("history-autosuggest", true)
	_ = rl.Config.Set("cursor-position-probe", false)
	_ = rl.Config.Set("completion-ignore-case", true)
	_ = rl.Config.Set("completion-selection-style", "\x1b[7m")
	_ = rl.Config.Set("usage-hint-always", true)
	rl.Keymap.SetMain("vi-insert")

	hist := readline.NewInMemoryHistory()
	_, _ = hist.Write(`{"op":"queue.create","target":"ctx","priority":"high","payload":{"path":"demo.jsonl"},"reason":"seed history"}`)
	_, _ = hist.Write(`{"op":"queue.preview","target":"local","payload":{"path":"manual.jsonl"}}`)
	rl.History.Add("hq-demo-history", hist)

	rl.Prompt.Primary(func() string { return "hq> " })
	rl.Prompt.Right(func() string { return "JSONL-aware autocomplete compiler" })

	preview := func() {
		line := string(*rl.Line())
		cursor := rl.Cursor().Pos()
		sugs := hq.Complete(line, cursor, world)
		var draft any
		if len(sugs) > 0 {
			draft = sugs[0].Draft
		} else {
			draft = hq.CompileLine(line, world)
		}
		b, _ := json.Marshal(draft)
		rl.Printf("compileDraft %s", string(b))
	}
	rl.Keymap.Register(map[string]func(){"hq-preview-draft": preview})
	ctrlT := string([]byte{0x14})
	_ = rl.Config.Bind("vi-insert", ctrlT, "hq-preview-draft", false)
	_ = rl.Config.Bind("vi-command", ctrlT, "hq-preview-draft", false)
	_ = rl.Config.Bind("emacs", ctrlT, "hq-preview-draft", false)

	// Tab opens the visible candidate list. This makes the expected Cursor-like UX
	// inspectable: candidate label + detail + meaning/compile target.
	_ = rl.Config.Bind("vi-insert", "\t", "possible-completions", false)
	_ = rl.Config.Bind("emacs", "\t", "possible-completions", false)

	rl.Completer = func(line []rune, cursor int) readline.Completions {
		buffer := string(line)
		sugs := hq.Complete(buffer, cursor, world)
		vals := make([]readline.Completion, 0, len(sugs))
		prefix := ""
		if len(sugs) > 0 {
			ed := sugs[0].Edit
			if ed.Start >= 0 && ed.End >= ed.Start && ed.End <= len(buffer) {
				prefix = buffer[ed.Start:ed.End]
			}
		}
		for _, s := range sugs {
			desc := s.Detail
			if s.Description != "" {
				desc += " | " + s.Description
			}
			vals = append(vals, readline.Completion{
				Value:       s.InsertText,
				Display:     s.Label,
				Description: desc,
				Tag:         s.Tag,
			})
		}
		comps := readline.CompleteRaw(vals).
			DisplayList().
			JustifyDescriptions().
			ListSeparator("  →  ").
			NoSort().
			Usage("TAB: candidates / ENTER: accept line / Ctrl-T: compileDraft / ESC: vi-command")
		comps.PREFIX = prefix
		return comps
	}

	var queueFile *os.File
	if queuePath != "" {
		f, err := os.OpenFile(queuePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		queueFile = f
	}

	if banner {
		fmt.Println("hq: vi=ON Tab=candidates autocomplete=ON Ctrl-T=compileDraft Enter=queue")
		fmt.Println("try: {\"  then TAB, or {\"op\":q then TAB")
	}

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "interrupt") {
				fmt.Println()
				return nil
			}
			return err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "exit" || trimmed == "quit" {
			return nil
		}
		d := hq.CompileLine(line, world)
		b, _ := json.Marshal(d)
		fmt.Printf("[ACCEPT] %s\n", string(b))
		if queueFile != nil {
			if err := (hq.QueueWriter{W: queueFile}).Append(d); err != nil {
				return err
			}
		}
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
