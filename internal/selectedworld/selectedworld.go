// Package selectedworld owns read-only loading of one explicitly selected,
// immutable runtime world. It adds path validation around the canonical strict
// adapter without introducing discovery or another identity implementation.
package selectedworld

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"hq/internal/adapter/current"
	"hq/internal/core"
)

// Selection is the exact strict world snapshot and identity produced by one
// successful load. Ref is derived from World and is never independently
// computed.
type Selection struct {
	World    *core.JsonlWorld
	Ref      core.WorldRef
	Selected bool
}

// Load opens an explicit absolute regular file and invokes the existing strict
// selected-world loader. It performs no search, precedence, or path fallback.
func Load(path string) (Selection, error) {
	return load(path, current.LoadSelectedWorldJSONL)
}

// LoadRuntime preserves the existing identity-free runtime compatibility path
// while reporting Selected only when the loaded snapshot has a strict world
// identity. It performs the same explicit path checks as strict inspection.
func LoadRuntime(path string) (Selection, error) {
	return load(path, current.LoadRuntimeWorldJSONL)
}

func load(path string, loader func(io.Reader) (*core.JsonlWorld, error)) (Selection, error) {
	if !filepath.IsAbs(path) {
		return Selection{}, errors.New("selected world path must be absolute")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return Selection{}, fmt.Errorf("lstat selected world: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return Selection{}, errors.New("selected world path must name a regular non-symlink file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Selection{}, fmt.Errorf("open selected world: %w", err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return Selection{}, fmt.Errorf("stat opened selected world: %w", statErr)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return Selection{}, errors.New("selected world path must name a regular file")
	}
	if !os.SameFile(pathInfo, info) {
		_ = file.Close()
		return Selection{}, errors.New("selected world path changed while opening")
	}
	world, loadErr := loader(file)
	closeErr := file.Close()
	if loadErr != nil {
		return Selection{}, loadErr
	}
	if closeErr != nil {
		return Selection{}, closeErr
	}
	ref, selected := world.SelectedRef()
	return Selection{World: world, Ref: ref, Selected: selected}, nil
}
