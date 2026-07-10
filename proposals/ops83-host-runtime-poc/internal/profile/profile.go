package profile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const CommandSpecKind = "hq.commandSpec.v1"

type CommandSpec struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Label      string `json:"label"`
	Detail     string `json:"detail"`
	InsertText string `json:"insertText"`
	BufferKind string `json:"bufferKind"`
}

type Profile struct {
	Name        string
	Root        string
	CatalogPath string
	QueuePath   string
	ReceiptPath string
	Commands    []CommandSpec
}

func Load(root, name string) (*Profile, error) {
	if root == "" {
		return nil, fmt.Errorf("HQ_LOCAL_ROOT is required")
	}
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid profile name: %q", name)
	}

	p := &Profile{
		Name:        name,
		Root:        root,
		CatalogPath: filepath.Join(root, "profiles", name, "catalog.jsonl"),
		QueuePath:   filepath.Join(root, "queues", "hq.host-command.queue.jsonl"),
		ReceiptPath: filepath.Join(root, "receipts", "hq.host-command.receipt.jsonl"),
	}
	for _, path := range []string{p.CatalogPath, p.QueuePath, p.ReceiptPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("required profile JSONL is missing: %s", path)
		}
	}

	commands, err := readCatalog(p.CatalogPath)
	if err != nil {
		return nil, err
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("profile catalog has no command specs: %s", p.CatalogPath)
	}
	p.Commands = commands
	return p, nil
}

func readCatalog(path string) ([]CommandSpec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []CommandSpec
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var spec CommandSpec
		if err := json.Unmarshal(scanner.Bytes(), &spec); err != nil {
			return nil, fmt.Errorf("catalog line %d: %w", line, err)
		}
		if spec.Kind != CommandSpecKind || spec.ID == "" || spec.Label == "" || spec.InsertText == "" || spec.BufferKind == "" {
			return nil, fmt.Errorf("catalog line %d: invalid %s record", line, CommandSpecKind)
		}
		out = append(out, spec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
